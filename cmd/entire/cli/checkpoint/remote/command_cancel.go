package remote

import (
	"os/exec"
	"time"
)

// killWaitDelay bounds how long Wait blocks for a killed git subprocess (and any
// descendants) to release the stdout/stderr pipes before Go forcibly closes them
// and returns. Without it, a `git push`/`git fetch` over a custom transport can
// hang the caller far past its context deadline: the deadline SIGKILLs `git`, but
// a spawned remote-helper grandchild (e.g. git-remote-entire) keeps the output
// pipe open, so cmd.CombinedOutput() blocks until that helper finally exits —
// observed at ~58 minutes for a stuck checkpoint push.
const killWaitDelay = 10 * time.Second

// terminateOnCancel makes a git subprocess terminate promptly when its context is
// cancelled, even if it spawned long-lived transport helpers. It always sets a
// WaitDelay backstop; on platforms that support it, killProcessGroupOnCancel
// additionally runs the child in its own process group and SIGKILLs the whole
// group on cancellation so remote-helper descendants are terminated rather than
// orphaned.
func terminateOnCancel(cmd *exec.Cmd) {
	cmd.WaitDelay = killWaitDelay
	killProcessGroupOnCancel(cmd)
}
