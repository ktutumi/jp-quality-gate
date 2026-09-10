// jpqg-pack-cjmodel regenerates the Worker-only CJ artifact from canonical gzip.
package main

import (
	"bufio"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/ktutumi/jp-quality-gate/internal/cj"
)

type manifest struct {
	SchemaVersion int `json:"schema_version"`
	cj.PackedMetadata
	UpstreamVersion     string `json:"upstream_version"`
	SourceFile          string `json:"source_file"`
	SourceSHA           string `json:"source_sha256"`
	SourceBytes         int    `json:"source_bytes"`
	PackedFile          string `json:"packed_file"`
	FileSHA             string `json:"packed_file_sha256"`
	ContentSHA          string `json:"content_sha256"`
	DecodedBytes        uint64 `json:"decoded_array_bytes"`
	EmbeddedPlusDecoded uint64 `json:"embedded_plus_decoded_array_bytes"`
	ParserRevision      int    `json:"parser_format_revision"`
	GoVersion           string `json:"go_version"`
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
func run(args []string) error {
	flags := flag.NewFlagSet("jpqg-pack-cjmodel", flag.ContinueOnError)
	input := flags.String("input", "internal/embedded/data/cjlogprobs.gz", "canonical gzip")
	output := flags.String("output", "internal/embedded/data/cjmodel-v1.bin", "packed artifact")
	manifestPath := flags.String("manifest", "internal/embedded/data/cjmodel-v1.manifest.json", "manifest")
	check := flags.Bool("check", false, "compare complete regeneration without writing")
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments")
	}
	if strings.Contains(os.Getenv("GOFLAGS"), "jpqg_packed_cjmodel") {
		return fmt.Errorf("packer requires native legacy build without packed GOFLAGS")
	}
	cwd, err := os.Getwd()
	if err != nil {
		return err
	}
	paths := []string{*input, *output, *manifestPath}
	labels := make([]string, len(paths))
	for i := range paths {
		if paths[i] == "" {
			return fmt.Errorf("paths must not be empty")
		}
		a, err := filepath.Abs(paths[i])
		if err != nil {
			return err
		}
		paths[i] = a
		relative, err := filepath.Rel(cwd, a)
		if err != nil {
			return err
		}
		if relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
			return fmt.Errorf("paths must be inside the working directory; run from the repository root")
		}
		labels[i] = filepath.ToSlash(relative)
		for j := 0; j < i; j++ {
			same := paths[i] == paths[j]
			a, e1 := os.Stat(paths[i])
			b, e2 := os.Stat(paths[j])
			if e1 == nil && e2 == nil {
				same = same || os.SameFile(a, b)
			}
			if same {
				return fmt.Errorf("input, output and manifest must be distinct")
			}
		}
	}
	source, err := os.ReadFile(*input)
	if err != nil {
		return err
	}
	reader, err := gzip.NewReader(bytes.NewReader(source))
	if err != nil {
		return err
	}
	buffered := bufio.NewReader(reader)
	// Validate only the format boundary here; ParseModel remains the sole
	// interpreter of model probabilities and constructs the canonical arrays.
	const languageHeader = "Languages: zh-hans,zh-hant,ja "
	header, headerErr := buffered.Peek(len(languageHeader))
	if headerErr != nil || string(header) != languageHeader {
		reader.Close()
		return fmt.Errorf("packed v1 requires exactly zh-hans,zh-hant,ja columns in that order")
	}
	c, parseErr := cj.ParseModel(buffered, "canonical gzip", 0)
	closeErr := reader.Close()
	if parseErr != nil {
		return parseErr
	}
	if closeErr != nil {
		return closeErr
	}
	// Stage both files beside their destinations; no existing artifact is touched
	// until all encoding, round-trip and manifest checks have succeeded.
	temp, err := os.CreateTemp(filepath.Dir(*output), ".cj-packed-*")
	if err != nil {
		return err
	}
	defer os.Remove(temp.Name())
	err = cj.EncodePacked(temp, c, sha256.Sum256(source))
	closeErr = temp.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	data, err := os.ReadFile(temp.Name())
	if err != nil {
		return err
	}
	decoded, meta, err := cj.DecodePacked(data)
	if err != nil {
		return err
	}
	if err = cj.EqualPackedModel(c, decoded); err != nil {
		return err
	}
	fileSHA := sha256.Sum256(data)
	m := manifest{SchemaVersion: 1, PackedMetadata: meta, UpstreamVersion: cj.Version, SourceFile: labels[0], SourceSHA: hex.EncodeToString(meta.SourceSHA256[:]), SourceBytes: len(source), PackedFile: labels[1], FileSHA: hex.EncodeToString(fileSHA[:]), ContentSHA: hex.EncodeToString(meta.ContentSHA256[:]), DecodedBytes: meta.PayloadBytes, EmbeddedPlusDecoded: meta.FileBytes + meta.PayloadBytes, ParserRevision: cj.PackedParserRevision, GoVersion: runtime.Version()}
	manifestBytes, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	manifestBytes = append(manifestBytes, '\n')
	if *check {
		for _, item := range []struct {
			path string
			want []byte
		}{{*output, data}, {*manifestPath, manifestBytes}} {
			actual, err := os.ReadFile(item.path)
			if err != nil {
				return err
			}
			if !bytes.Equal(actual, item.want) {
				return fmt.Errorf("stale CJ artifact: %s (run make pack-cj and review)", item.path)
			}
		}
		return nil
	}
	mt, err := os.CreateTemp(filepath.Dir(*manifestPath), ".cj-manifest-*")
	if err != nil {
		return err
	}
	defer os.Remove(mt.Name())
	n, err := mt.Write(manifestBytes)
	closeErr = mt.Close()
	if err != nil {
		return err
	}
	if n != len(manifestBytes) {
		return io.ErrShortWrite
	}
	if closeErr != nil {
		return closeErr
	}
	if err = os.Chmod(temp.Name(), 0644); err != nil {
		return err
	}
	if err = os.Chmod(mt.Name(), 0644); err != nil {
		return err
	}
	if err = os.Rename(temp.Name(), *output); err != nil {
		return err
	}
	// Two renames are not a transaction. Build/check rejects a mismatched pair.
	return os.Rename(mt.Name(), *manifestPath)
}
