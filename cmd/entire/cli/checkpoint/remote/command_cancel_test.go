package remote

import (
	"context"
	"os/exec"
	"testing"
)

// newCommand must make every git subprocess it builds terminate on cancellation
// so a hung transport can't block past the caller's context deadline.
func TestNewCommand_TerminatesOnCancel(t *testing.T) {
	t.Parallel()

	cmd := newCommand(context.Background(), "push", "origin", "main")

	if cmd.WaitDelay != killWaitDelay {
		t.Errorf("WaitDelay = %v; want %v", cmd.WaitDelay, killWaitDelay)
	}
	if cmd.Cancel == nil {
		t.Error("Cancel = nil; want a cancellation handler that terminates the process")
	}
}

// terminateOnCancel sets the WaitDelay backstop on all platforms.
func TestTerminateOnCancel_SetsWaitDelay(t *testing.T) {
	t.Parallel()

	cmd := exec.CommandContext(context.Background(), "git", "status")
	terminateOnCancel(cmd)

	if cmd.WaitDelay != killWaitDelay {
		t.Errorf("WaitDelay = %v; want %v", cmd.WaitDelay, killWaitDelay)
	}
}
