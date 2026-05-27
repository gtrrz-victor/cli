//go:build windows

package remote

import "os/exec"

// killProcessGroupOnCancel is a no-op on Windows. Reliable termination of a
// process tree there requires a Job Object, which is out of scope here. The
// cross-platform WaitDelay backstop still bounds how long we block on a hung
// subprocess, and exec.Cmd's default context-cancellation handler still kills
// the direct git process.
func killProcessGroupOnCancel(_ *exec.Cmd) {}
