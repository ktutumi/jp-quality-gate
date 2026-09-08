//go:build unix

package prose

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestExecRunnerTimeoutKillsDescendants(t *testing.T) {
	dir := t.TempDir()
	ready, survived := filepath.Join(dir, "ready"), filepath.Join(dir, "survived")
	// The shell waits for a child that inherits stdout/stderr. Killing only
	// the shell leaves that child alive long enough to write the marker.
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	defer cancel()
	_, code, err := (ExecRunner{}).Run(ctx, "sh", []string{"-c", `
(sleep 2; printf survived > "$2") &
printf ready > "$1"
wait
`, "fixture", ready, survived}, "")
	if !errors.Is(err, context.DeadlineExceeded) || code != -1 {
		t.Fatalf("code=%d err=%v", code, err)
	}
	if _, err := os.Stat(ready); err != nil {
		t.Fatalf("fixture did not start: %v", err)
	}
	// Wait beyond the child's timer even when cancellation returns immediately.
	time.Sleep(2200 * time.Millisecond)
	if _, err := os.Stat(survived); !os.IsNotExist(err) {
		t.Fatalf("descendant survived cancellation: %v", err)
	}
}
