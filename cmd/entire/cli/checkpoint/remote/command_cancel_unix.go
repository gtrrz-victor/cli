//go:build unix

package remote

import (
	"fmt"
	"os/exec"
	"syscall"
)

// killProcessGroupOnCancel puts the child in its own process group and replaces
// the context-cancellation handler so the entire group is SIGKILLed. exec.Cmd's
// default Cancel only kills the direct `git` process, which leaves a spawned
// remote-helper grandchild (e.g. git-remote-entire) running and holding the
// output pipe open — defeating the caller's timeout.
func killProcessGroupOnCancel(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return nil
		}
		// Negative PID targets the whole process group (the leader's PID == pgid),
		// so the remote helper and any other descendants are killed too. ESRCH
		// means the group already exited, which is not an error for our purposes.
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != nil && err != syscall.ESRCH {
			return fmt.Errorf("kill process group: %w", err)
		}
		return nil
	}
}
