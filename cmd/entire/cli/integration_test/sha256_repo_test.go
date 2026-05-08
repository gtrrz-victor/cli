//go:build integration

package integration

import (
	"os/exec"
	"strings"
	"testing"

	"github.com/entireio/cli/cmd/entire/cli/paths"
	"github.com/entireio/cli/cmd/entire/cli/testutil"
)

func TestSHA256Repository_EnableAndFirstCheckpoint(t *testing.T) {
	t.Parallel()
	requireGitSHA256Support(t)

	env := NewTestEnv(t)
	env.ExtraEnv = append(env.ExtraEnv,
		"GIT_DEFAULT_HASH=sha256",
		"GIT_CONFIG_COUNT=2",
		"GIT_CONFIG_KEY_0=user.name",
		"GIT_CONFIG_VALUE_0=Test User",
		"GIT_CONFIG_KEY_1=user.email",
		"GIT_CONFIG_VALUE_1=test@example.com",
	)

	output := env.RunCLI(
		"enable",
		"--init-repo",
		"--no-github",
		"--agent", "claude-code",
		"--telemetry=false",
		"--initial-commit-message", "Initial SHA-256 commit",
	)
	if !strings.Contains(output, "Initialized empty git repository") {
		t.Fatalf("expected enable to initialize git repo, got output:\n%s", output)
	}

	if got := gitOutput(t, env.RepoDir, "rev-parse", "--show-object-format=storage"); got != "sha256" {
		t.Fatalf("repository object format = %q, want sha256", got)
	}

	initialHead := gitOutput(t, env.RepoDir, "rev-parse", "HEAD")
	requireHexLen(t, "initial HEAD", initialHead, 64)
	initialMetadataHead := gitOutput(t, env.RepoDir, "rev-parse", paths.MetadataBranchName)
	requireHexLen(t, "initial metadata branch HEAD", initialMetadataHead, 64)

	// Persist local identity for later git/go-git helper commits and hook subprocesses.
	gitOutput(t, env.RepoDir, "config", "user.name", "Test User")
	gitOutput(t, env.RepoDir, "config", "user.email", "test@example.com")
	gitOutput(t, env.RepoDir, "config", "commit.gpgsign", "false")

	sess := env.NewSession()
	prompt := "Create a file in the SHA-256 repo"
	if err := env.SimulateUserPromptSubmitWithPromptAndTranscriptPath(sess.ID, prompt, sess.TranscriptPath); err != nil {
		t.Fatalf("user-prompt-submit failed: %v", err)
	}

	const mainContent = "package main\n\nfunc main() {}\n"
	env.WriteFile("main.go", mainContent)
	sess.CreateTranscript(prompt, []FileChange{{Path: "main.go", Content: mainContent}})
	if err := env.SimulateStop(sess.ID, sess.TranscriptPath); err != nil {
		t.Fatalf("stop hook failed creating first checkpoint: %v", err)
	}

	state, err := env.GetSessionState(sess.ID)
	if err != nil {
		t.Fatalf("GetSessionState failed: %v", err)
	}
	if state == nil || state.StepCount != 1 {
		t.Fatalf("session StepCount after first checkpoint = %#v, want 1", state)
	}

	shadowBranch := env.GetShadowBranchNameForCommit(initialHead)
	shadowHead := gitOutput(t, env.RepoDir, "rev-parse", shadowBranch)
	requireHexLen(t, "shadow checkpoint commit", shadowHead, 64)

	env.GitCommitWithShadowHooks("Add SHA-256 main", "main.go")
	userHead := gitOutput(t, env.RepoDir, "rev-parse", "HEAD")
	requireHexLen(t, "user commit", userHead, 64)
	if userHead == initialHead {
		t.Fatal("expected user commit to advance HEAD")
	}

	metadataHead := gitOutput(t, env.RepoDir, "rev-parse", paths.MetadataBranchName)
	requireHexLen(t, "checkpoint metadata commit", metadataHead, 64)
	if metadataHead == initialMetadataHead {
		t.Fatal("expected metadata branch to advance after condensing the first checkpoint")
	}

	subject := gitOutput(t, env.RepoDir, "log", "-1", "--format=%s", paths.MetadataBranchName)
	if !strings.HasPrefix(subject, "Checkpoint: ") {
		t.Fatalf("metadata branch latest subject = %q, want Checkpoint: <id>", subject)
	}
	checkpointID := strings.TrimPrefix(subject, "Checkpoint: ")
	if len(checkpointID) != 12 {
		t.Fatalf("checkpoint ID length = %d, want 12: %q", len(checkpointID), checkpointID)
	}
	if _, found := env.ReadFileFromBranch(paths.MetadataBranchName, SessionMetadataPath(checkpointID)); !found {
		t.Fatalf("expected session metadata for checkpoint %s on %s", checkpointID, paths.MetadataBranchName)
	}
}

func requireGitSHA256Support(t *testing.T) {
	t.Helper()

	dir := t.TempDir()
	cmd := exec.Command("git", "init", "--object-format=sha256", dir) //nolint:noctx // test capability probe
	cmd.Env = testutil.GitIsolatedEnv()
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Skipf("git does not support SHA-256 repositories: %v\n%s", err, output)
	}
	if got := gitOutput(t, dir, "rev-parse", "--show-object-format=storage"); got != "sha256" {
		t.Skipf("git initialized object format %q, not sha256", got)
	}
}

func gitOutput(t *testing.T, dir string, args ...string) string {
	t.Helper()

	cmd := exec.Command("git", args...) //nolint:noctx // test helper
	cmd.Dir = dir
	cmd.Env = testutil.GitIsolatedEnv()
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s failed: %v\n%s", strings.Join(args, " "), err, output)
	}
	return strings.TrimSpace(string(output))
}

func requireHexLen(t *testing.T, label, value string, want int) {
	t.Helper()

	if len(value) != want {
		t.Fatalf("%s length = %d, want %d: %q", label, len(value), want, value)
	}
	for _, r := range value {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			t.Fatalf("%s contains non-hex character %q: %q", label, r, value)
		}
	}
}
