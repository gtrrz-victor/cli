// Package remotehelper holds constants shared between the entire CLI's
// argv[0] dispatch (cmd/entire) and the installer that places the
// git-remote-entire launcher (cmd/entire/cli). The remote-helper protocol
// implementation lives in the subpackages (githelper, transport, …).
package remotehelper

// BinaryName is the git remote-helper executable name. Git resolves
// `entire://` URLs by exec'ing a binary called this on PATH; the entire
// CLI is installed under this name (a symlink to, or copy of, the entire
// binary) and dispatches on argv[0].
const BinaryName = "git-remote-entire"
