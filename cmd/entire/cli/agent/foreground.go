package agent

import (
	"context"
	"fmt"
	"os"
	"os/exec"
)

// NewForegroundCommand builds an exec.Cmd wired to the caller's terminal.
// Agent launchers use this for commands the user should interact with directly.
func NewForegroundCommand(ctx context.Context, binary string, args ...string) (*exec.Cmd, error) {
	bin, err := exec.LookPath(binary)
	if err != nil {
		return nil, fmt.Errorf("%s binary not on PATH: %w", binary, err)
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = os.Environ()
	return cmd, nil
}
