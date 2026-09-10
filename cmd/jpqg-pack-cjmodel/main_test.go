package main

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestGenerationAndReadOnlyFreshness(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	input := filepath.Join(dir, "source.gz")
	output := filepath.Join(dir, "model.bin")
	mf := filepath.Join(dir, "manifest.json")
	var b bytes.Buffer
	z := gzip.NewWriter(&b)
	z.Write([]byte("Languages: zh-hans,zh-hant,ja MinLogProb: -10\n甲 -1 -2 -3\n"))
	z.Close()
	os.WriteFile(input, b.Bytes(), 0600)
	args := []string{"--input", input, "--output", output, "--manifest", mf}
	if err := run(args); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(output)
	meta, _ := os.ReadFile(mf)
	var provenance manifest
	if err := json.Unmarshal(meta, &provenance); err != nil {
		t.Fatal(err)
	}
	if provenance.SourceFile != "source.gz" || provenance.PackedFile != "model.bin" {
		t.Fatal("incorrect custom path provenance", provenance)
	}
	if err := run(append(args, "--check")); err != nil {
		t.Fatal(err)
	}
	if err := run(args); err != nil {
		t.Fatal(err)
	}
	again, _ := os.ReadFile(output)
	if !bytes.Equal(data, again) {
		t.Fatal("nondeterministic")
	}
	for _, p := range []string{input, output, mf} {
		original, _ := os.ReadFile(p)
		changed := bytes.Clone(original)
		changed[len(changed)-1] ^= 1
		os.WriteFile(p, changed, 0600)
		if run(append(args, "--check")) == nil {
			t.Fatal("accepted stale", p)
		}
		after, _ := os.ReadFile(p)
		if !bytes.Equal(changed, after) {
			t.Fatal("check mutated", p)
		}
		os.WriteFile(p, original, 0600)
	}
	if run([]string{"--input", input, "--output", input, "--manifest", mf}) == nil {
		t.Fatal("overwrites source")
	}
	os.WriteFile(input, []byte("bad gzip"), 0600)
	if run(args) == nil {
		t.Fatal("bad input accepted")
	}
	after, _ := os.ReadFile(output)
	afterMeta, _ := os.ReadFile(mf)
	if !bytes.Equal(after, data) || !bytes.Equal(afterMeta, meta) {
		t.Fatal("damaged existing artifact")
	}
}

func TestPackedSourceLanguageHeader(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	for _, languages := range []string{"zh-hans,ja", "zh-hans,zh-hans,ja", "ja,zh-hant,zh-hans", "zh-hans,zh-hant,ja,ja"} {
		var b bytes.Buffer
		z := gzip.NewWriter(&b)
		z.Write([]byte("Languages: " + languages + " MinLogProb: -10\n"))
		z.Close()
		if err := os.WriteFile("source.gz", b.Bytes(), 0600); err != nil {
			t.Fatal(err)
		}
		if run([]string{"--input", "source.gz", "--output", "model.bin", "--manifest", "manifest.json"}) == nil {
			t.Fatal("accepted invalid column metadata", languages)
		}
		if _, err := os.Stat("model.bin"); !os.IsNotExist(err) {
			t.Fatal("invalid source created artifact")
		}
	}
	if run([]string{"--input", "../outside.gz", "--output", "model.bin", "--manifest", "manifest.json"}) == nil {
		t.Fatal("accepted path outside provenance root")
	}
}
