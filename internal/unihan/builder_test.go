package unihan

import (
	"archive/zip"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestParseZipReadsAllTextMembersAndBuildsExpectedRecords(t *testing.T) {
	zipPath := filepath.Join(t.TempDir(), "Unihan.zip")
	file, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	archive := zip.NewWriter(file)
	writeMember := func(name, content string) {
		t.Helper()
		member, memberErr := archive.Create(name)
		if memberErr != nil {
			t.Fatal(memberErr)
		}
		if _, memberErr = member.Write([]byte(content)); memberErr != nil {
			t.Fatal(memberErr)
		}
	}
	writeMember("Unihan_IRGSources.txt", "# comment\nU+7ECF\tkIRG_GSource\tG0-...\nU+7ECF\tkTraditionalVariant\tU+7D93\n")
	writeMember("extra.txt", "U+7D93\tkIRG_JSource\tJ0-...\nU+7D93\tkJapaneseNewVariant\tU+7D4C\nU+5B66\tkIRG_GSource\tG0-...\nU+5B66\tkIRG_JSource\tJ0-...\n")
	writeMember("README", "U+7ECF\tkIRG_GSource\tignored\n")
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}

	props, err := ParseZip(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	table := BuildTable(props, "test")
	if got := table.Characters["经"].Severity; got != "error" {
		t.Fatalf("经 severity = %q, want error", got)
	}
	if got := table.Characters["经"].JapaneseCandidates; len(got) != 1 || got[0] != "経" {
		t.Fatalf("经 candidates = %#v, want [経]", got)
	}
	if _, ok := table.Characters["学"]; ok {
		t.Fatal("学 has a finding despite Japanese evidence")
	}

	output := filepath.Join(t.TempDir(), "nested", "table.json")
	if err := Build(zipPath, output, "test"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(output)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Table
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.SchemaVersion != 1 || decoded.UnicodeVersion != "test" {
		t.Fatalf("metadata = %#v", decoded)
	}
}

func TestCodepointsMatchesUppercaseUnihanReferences(t *testing.T) {
	got := Codepoints([]string{"U+7D93 U+7D4C; U+ABCD", "lower U+abcd"})
	want := []int{0x7D93, 0x7D4C, 0xABCD}
	if len(got) != len(want) {
		t.Fatalf("codepoints = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("codepoints = %#v, want %#v", got, want)
		}
	}
}
