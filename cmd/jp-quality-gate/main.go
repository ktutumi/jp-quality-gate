// Command jp-quality-gate checks LLM-generated Japanese text.
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/ktutumi/jp-quality-gate/internal/cj"
	"github.com/ktutumi/jp-quality-gate/internal/embedded"
	"github.com/ktutumi/jp-quality-gate/internal/gate"
	"github.com/ktutumi/jp-quality-gate/internal/unihan"
)

const defaultUnicodeVersion = "18.0.0"

type cliOptions struct {
	unicodeVersion   string
	unihanTable      string
	unihanTableSet   bool
	cjMinCJK         int
	cjMinGap         float64
	includeCode      bool
	warningsAsErrors bool
	pretty           bool
	file             string
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdin, os.Stdout, os.Stderr))
}

func run(args []string, stdin io.Reader, stdout, stderr io.Writer) int {
	options, err := parseArgs(args, stderr)
	if errors.Is(err, flag.ErrHelp) {
		return 0
	}
	if err != nil {
		writeInternalError(stdout, err)
		return 2
	}
	if !(options.cjMinGap >= 0 && options.cjMinGap <= 1) {
		err := errors.New("--cj-min-gap must be between 0 and 1")
		fmt.Fprintf(stderr, "jp-quality-gate: error: %s\n", err)
		writeInternalError(stdout, err)
		return 2
	}
	if options.cjMinCJK < 1 {
		err := errors.New("--cj-min-cjk must be >= 1")
		fmt.Fprintf(stderr, "jp-quality-gate: error: %s\n", err)
		writeInternalError(stdout, err)
		return 2
	}

	input, err := readInput(options.file, stdin)
	if err != nil {
		writeInternalError(stdout, err)
		return 2
	}
	unihanScanner, err := loadUnihan(options)
	if err != nil {
		writeInternalError(stdout, err)
		return 2
	}
	classifier, err := cj.Load()
	if err != nil {
		writeInternalError(stdout, err)
		return 2
	}

	result := (&gate.Gate{Unihan: unihanScanner, CJ: classifier}).Check(input, gate.Options{
		IncludeCode:      options.includeCode,
		CJMinCJK:         options.cjMinCJK,
		CJMinGap:         options.cjMinGap,
		WarningsAsErrors: options.warningsAsErrors,
	})
	encoder := json.NewEncoder(stdout)
	encoder.SetEscapeHTML(false)
	if options.pretty {
		encoder.SetIndent("", "  ")
	}
	if err := encoder.Encode(result); err != nil {
		return 2
	}
	if result.Passed() {
		return 0
	}
	return 1
}

func parseArgs(args []string, stderr io.Writer) (cliOptions, error) {
	var options cliOptions
	flags := flag.NewFlagSet("jp-quality-gate", flag.ContinueOnError)
	flags.SetOutput(stderr)
	flags.StringVar(&options.unicodeVersion, "unicode-version", defaultUnicodeVersion, "Unicode version")
	flags.StringVar(&options.unihanTable, "unihan-table", "", "Unihan table path")
	flags.IntVar(&options.cjMinCJK, "cj-min-cjk", 4, "minimum CJK characters per segment")
	flags.Float64Var(&options.cjMinGap, "cj-min-gap", 0.15, "minimum CJ score gap")
	flags.BoolVar(&options.includeCode, "include-code", false, "include Markdown code")
	flags.BoolVar(&options.warningsAsErrors, "warnings-as-errors", false, "promote warnings to errors")
	flags.BoolVar(&options.pretty, "pretty", false, "pretty-print JSON")
	flags.Usage = func() {
		fmt.Fprintln(stderr, "usage: jp-quality-gate [options] [file]")
		flags.PrintDefaults()
	}

	reordered, err := reorderArgs(args)
	if err != nil {
		return options, err
	}
	if err := flags.Parse(reordered); err != nil {
		return options, err
	}
	flags.Visit(func(value *flag.Flag) {
		if value.Name == "unihan-table" {
			options.unihanTableSet = true
		}
	})
	if flags.NArg() > 1 {
		return options, fmt.Errorf("unrecognized arguments: %s", strings.Join(flags.Args()[1:], " "))
	}
	if flags.NArg() == 1 {
		options.file = flags.Arg(0)
	}
	return options, nil
}

// The standard flag package stops at the first positional argument. Python's
// argparse does not, so move one positional file after all recognized flags.
func reorderArgs(args []string) ([]string, error) {
	valueFlags := map[string]bool{
		"unicode-version": true,
		"unihan-table":    true,
		"cj-min-cjk":      true,
		"cj-min-gap":      true,
	}
	flagArgs := make([]string, 0, len(args))
	positional := make([]string, 0, 1)
	for index := 0; index < len(args); index++ {
		argument := args[index]
		if argument == "--" {
			positional = append(positional, args[index+1:]...)
			break
		}
		if argument == "-" || !strings.HasPrefix(argument, "-") {
			positional = append(positional, argument)
			continue
		}
		flagArgs = append(flagArgs, argument)
		name := strings.TrimLeft(argument, "-")
		if equal := strings.IndexByte(name, '='); equal >= 0 {
			name = name[:equal]
			continue
		}
		if valueFlags[name] {
			if index+1 >= len(args) {
				return nil, fmt.Errorf("flag needs an argument: --%s", name)
			}
			index++
			flagArgs = append(flagArgs, args[index])
		}
	}
	return append(flagArgs, positional...), nil
}

func readInput(path string, stdin io.Reader) (string, error) {
	var (
		data     []byte
		err      error
		fromFile bool
	)
	if path == "" || path == "-" {
		data, err = io.ReadAll(stdin)
	} else {
		data, err = os.ReadFile(path)
		fromFile = true
	}
	if err != nil {
		return "", err
	}
	if !utf8.Valid(data) {
		return "", errors.New("input is not valid UTF-8")
	}
	text := string(data)
	if fromFile {
		// Path.read_text uses universal newline translation; sys.stdin does not.
		text = strings.ReplaceAll(text, "\r\n", "\n")
		text = strings.ReplaceAll(text, "\r", "\n")
	}
	return text, nil
}

func loadUnihan(options cliOptions) (*unihan.Scanner, error) {
	if options.unihanTableSet {
		return unihan.Load(options.unihanTable)
	}
	if path := os.Getenv("JPQG_UNIHAN_TABLE"); path != "" {
		return unihan.Load(expandUser(path))
	}
	if options.unicodeVersion == defaultUnicodeVersion {
		reader, err := gzip.NewReader(bytes.NewReader(embedded.UnihanTableGZIP))
		if err != nil {
			return nil, err
		}
		data, readErr := io.ReadAll(reader)
		closeErr := reader.Close()
		if readErr != nil {
			return nil, readErr
		}
		if closeErr != nil {
			return nil, closeErr
		}
		return unihan.LoadBytes(data, "embedded:unihan-suspicious-18.0.0.json.gz")
	}
	return unihan.Load(filepath.Join(cacheDir(), "unihan-suspicious-"+options.unicodeVersion+".json"))
}

func cacheDir() string {
	if value := os.Getenv("XDG_CACHE_HOME"); value != "" {
		return filepath.Join(value, "jp-quality-gate")
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return filepath.Join(".cache", "jp-quality-gate")
	}
	return filepath.Join(home, ".cache", "jp-quality-gate")
}

func expandUser(path string) string {
	if path != "~" && !strings.HasPrefix(path, "~/") {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

func writeInternalError(output io.Writer, err error) {
	encoder := json.NewEncoder(output)
	encoder.SetEscapeHTML(false)
	_ = encoder.Encode(struct {
		Pass          bool   `json:"pass"`
		InternalError string `json:"internal_error"`
	}{Pass: false, InternalError: err.Error()})
}
