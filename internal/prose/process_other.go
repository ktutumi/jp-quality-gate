//go:build !unix

package prose

import "os/exec"

// Non-Unix platforms retain CommandContext's direct-process cancellation.
func configureCancellation(cmd *exec.Cmd) {}
