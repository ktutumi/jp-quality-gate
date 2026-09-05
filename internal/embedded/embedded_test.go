package embedded

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"testing"
)

func TestCJModelProvenance(t *testing.T) {
	sum := sha256.Sum256(CJModelGZIP)
	if got, want := hex.EncodeToString(sum[:]), "b0fcb1e82dac11d2e11710012b563f7b19ee3e92ce6a01e7de806bcaadfc012f"; got != want {
		t.Fatalf("CJ model SHA-256 = %s, want %s", got, want)
	}
}

func TestEmbeddedUnihanTable(t *testing.T) {
	zr, err := gzip.NewReader(bytes.NewReader(UnihanTableGZIP))
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	if err := zr.Close(); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(data)
	if got, want := hex.EncodeToString(sum[:]), "67472b566b8153c9a9eb72ab066b8db4389e24e66669f830a5e33b8e44e06c56"; got != want {
		t.Fatalf("Unihan JSON SHA-256 = %s, want %s", got, want)
	}

	var header struct {
		SchemaVersion  int                        `json:"schema_version"`
		UnicodeVersion string                     `json:"unicode_version"`
		Characters     map[string]json.RawMessage `json:"characters"`
	}
	if err := json.Unmarshal(data, &header); err != nil {
		t.Fatal(err)
	}
	if header.SchemaVersion != 1 || header.UnicodeVersion != "18.0.0" {
		t.Fatalf("unexpected table header: schema=%d unicode=%q", header.SchemaVersion, header.UnicodeVersion)
	}
	if _, ok := header.Characters["经"]; !ok {
		t.Fatal("embedded table does not contain 经")
	}
}
