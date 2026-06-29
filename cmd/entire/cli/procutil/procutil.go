// Package procutil holds helpers for terminating spawned subprocesses and
// their descendants when a context is cancelled.
package procutil

import (
	"os/exec"
	"time"
)

// terminateWaitDelay backstops the wait after ctx-cancel so Wait/Run returns
// even if a wedged descendant keeps an output pipe open after the group kill.
const terminateWaitDelay = 5 * time.Second

// TerminateOnCancel makes cmd and its descendants die when cmd's context is
// cancelled, and guarantees Wait/Run returns even if a descendant keeps an
// output pipe open. Call after building cmd, before Start/Run.
//
// Agent CLIs (codex, claude, ...) spawn helper grandchildren (sandbox, MCP
// servers) that inherit the stdout pipe. exec.CommandContext's default Cancel
// only SIGKILLs the agent itself, leaving grandchildren alive with the pipe
// open — so a reader draining stdout to EOF blocks forever and Ctrl+C hangs.
func TerminateOnCancel(cmd *exec.Cmd) {
	cmd.WaitDelay = terminateWaitDelay
	killProcessGroupOnCancel(cmd)
}
