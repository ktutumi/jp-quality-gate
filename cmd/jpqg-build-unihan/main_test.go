package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultTablePathHonorsEnvironmentOverride(t *testing.T) {
	t.Setenv("JPQG_UNIHAN_TABLE", "~/fixture/unihan.json")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if got, want := defaultTablePath("18.0.0"), filepath.Join(home, "fixture", "unihan.json"); got != want {
		t.Fatalf("defaultTablePath = %q, want %q", got, want)
	}
}
