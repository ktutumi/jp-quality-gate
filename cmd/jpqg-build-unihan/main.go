// Command jpqg-build-unihan builds the static Unihan quality-gate table.
package main

import (
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/ktutumi/jp-quality-gate/internal/unihan"
)

const defaultUnicodeVersion = "18.0.0"

func main() {
	os.Exit(run())
}

func run() int {
	flags := flag.NewFlagSet("jpqg-build-unihan", flag.ContinueOnError)
	flags.SetOutput(os.Stderr)
	version := flags.String("version", defaultUnicodeVersion, "Unicode version")
	zipPath := flags.String("unihan-zip", "", "Use an existing Unihan.zip")
	outputPath := flags.String("output", "", "Output JSON path")
	if err := flags.Parse(os.Args[1:]); err != nil {
		return 2
	}
	if len(flags.Args()) > 0 {
		fmt.Fprintf(os.Stderr, "jpqg-build-unihan: unexpected argument: %s\n", flags.Args()[0])
		return 2
	}

	output := *outputPath
	if output == "" {
		output = defaultTablePath(*version)
	}

	source := *zipPath
	if source != "" {
		if _, err := os.Stat(source); err != nil {
			fmt.Fprintf(os.Stderr, "jpqg-build-unihan: unihan zip does not exist: %s\n", source)
			return 2
		}
	} else {
		cache := filepath.Join(cacheDir(), fmt.Sprintf("Unihan-%s.zip", *version))
		if _, err := os.Stat(cache); os.IsNotExist(err) {
			fmt.Printf("Downloading %s\n", fmt.Sprintf(unihan.UnicodeURL, *version))
			if err := download(fmt.Sprintf(unihan.UnicodeURL, *version), cache); err != nil {
				fmt.Fprintf(os.Stderr, "jpqg-build-unihan: download failed: %v\n", err)
				return 2
			}
		} else if err != nil {
			fmt.Fprintf(os.Stderr, "jpqg-build-unihan: cache error: %v\n", err)
			return 2
		}
		source = cache
	}

	if err := unihan.Build(source, output, *version); err != nil {
		fmt.Fprintf(os.Stderr, "jpqg-build-unihan: %v\n", err)
		return 2
	}
	table, err := unihan.Load(output)
	if err != nil {
		fmt.Fprintf(os.Stderr, "jpqg-build-unihan: %v\n", err)
		return 2
	}
	errors, warnings := 0, 0
	for _, record := range table.Characters {
		if record.Severity == "error" {
			errors++
		} else {
			warnings++
		}
	}
	fmt.Printf("Wrote %s (%d error chars, %d warning chars)\n", output, errors, warnings)
	return 0
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

func defaultTablePath(version string) string {
	if value := os.Getenv("JPQG_UNIHAN_TABLE"); value != "" {
		return expandUser(value)
	}
	return filepath.Join(cacheDir(), fmt.Sprintf("unihan-suspicious-%s.json", version))
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

func download(url, destination string) error {
	if err := os.MkdirAll(filepath.Dir(destination), 0o755); err != nil {
		return err
	}
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	request.Header.Set("User-Agent", "jp-quality-gate/0.1")
	client := &http.Client{Timeout: 60 * time.Second}
	response, err := client.Do(request)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("HTTP %s", response.Status)
	}
	file, err := os.Create(destination)
	if err != nil {
		return err
	}
	if _, err := io.Copy(file, response.Body); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
