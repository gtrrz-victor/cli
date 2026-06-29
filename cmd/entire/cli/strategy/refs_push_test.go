package strategy

import (
	"context"
	"os/exec"
	"strings"
	"testing"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/entireio/cli/cmd/entire/cli/checkpoint"
	"github.com/entireio/cli/cmd/entire/cli/checkpoint/id"
	"github.com/entireio/cli/cmd/entire/cli/testutil"
)

// setupRepoWithCheckpointRefs creates a work repo with two per-checkpoint refs
// pointing at HEAD, plus a fresh bare remote. Returns (workDir, bareDir, refs).
func setupRepoWithCheckpointRefs(t *testing.T) (string, string, []plumbing.ReferenceName) {
	t.Helper()
	ctx := context.Background()

	workDir := t.TempDir()
	testutil.InitRepo(t, workDir)
	testutil.WriteFile(t, workDir, "README.md", "# test")
	testutil.GitAdd(t, workDir, "README.md")
	testutil.GitCommit(t, workDir, "init")

	repo, err := git.PlainOpen(workDir)
	require.NoError(t, err)
	head, err := repo.Head()
	require.NoError(t, err)

	refs := []plumbing.ReferenceName{
		checkpoint.RefName(id.MustCheckpointID("a1b2c3d4e5f6")),
		checkpoint.RefName(id.MustCheckpointID("b2c3d4e5f6a1")),
	}
	for _, ref := range refs {
		require.NoError(t, repo.Storer.SetReference(plumbing.NewHashReference(ref, head.Hash())))
	}

	bareDir := t.TempDir()
	initCmd := exec.CommandContext(ctx, "git", "init", "--bare")
	initCmd.Dir = bareDir
	initCmd.Env = testutil.GitIsolatedEnv()
	out, err := initCmd.CombinedOutput()
	require.NoError(t, err, "git init --bare failed: %s", out)

	return workDir, bareDir, refs
}

func TestPartitionLocalRefs(t *testing.T) {
	t.Parallel()
	workDir, _, refs := setupRepoWithCheckpointRefs(t)
	repo, err := git.PlainOpen(workDir)
	require.NoError(t, err)

	stale := checkpoint.RefName(id.MustCheckpointID("ffffffffffff"))
	existing, missing := partitionLocalRefs(repo, append([]plumbing.ReferenceName{stale}, refs...))

	assert.ElementsMatch(t, refs, existing, "local refs are pushable")
	assert.Equal(t, []plumbing.ReferenceName{stale}, missing, "absent ref is stale")
}

func TestBatchForcePushRefs(t *testing.T) {
	workDir, bareDir, refs := setupRepoWithCheckpointRefs(t)
	t.Chdir(workDir)

	require.NoError(t, batchForcePushRefs(context.Background(), bareDir, refs))

	// All refs now exist on the bare remote.
	lsCmd := exec.CommandContext(context.Background(), "git", "ls-remote", bareDir)
	lsCmd.Env = testutil.GitIsolatedEnv()
	out, err := lsCmd.CombinedOutput()
	require.NoError(t, err, "ls-remote failed: %s", out)
	remoteRefs := string(out)
	for _, ref := range refs {
		assert.Contains(t, remoteRefs, ref.String(), "ref should be present on the remote after batch push")
	}
}

func TestBatchForcePushRefs_Empty(t *testing.T) {
	t.Parallel()
	// No refs → no git invocation, no error.
	require.NoError(t, batchForcePushRefs(context.Background(), "unused-target", nil))
}

func TestBatchForcePushRefs_IsForcePush(t *testing.T) {
	workDir, bareDir, refs := setupRepoWithCheckpointRefs(t)
	t.Chdir(workDir)
	ctx := context.Background()

	// First push establishes the refs on the remote.
	require.NoError(t, batchForcePushRefs(ctx, bareDir, refs))

	// Re-point one ref at a new (unrelated, non-fast-forward) commit and push
	// again. A non-force push would be rejected; the force refspec must succeed.
	repo, err := git.PlainOpen(workDir)
	require.NoError(t, err)
	testutil.WriteFile(t, workDir, "two.txt", "second")
	testutil.GitAdd(t, workDir, "two.txt")
	testutil.GitCommit(t, workDir, "second")
	head2, err := repo.Head()
	require.NoError(t, err)
	require.NoError(t, repo.Storer.SetReference(plumbing.NewHashReference(refs[0], head2.Hash())))

	require.NoError(t, batchForcePushRefs(ctx, bareDir, refs[:1]), "force push must overwrite the remote ref")

	lsCmd := exec.CommandContext(ctx, "git", "ls-remote", bareDir, refs[0].String())
	lsCmd.Env = testutil.GitIsolatedEnv()
	out, err := lsCmd.CombinedOutput()
	require.NoError(t, err, "ls-remote failed: %s", out)
	assert.True(t, strings.HasPrefix(strings.TrimSpace(string(out)), head2.Hash().String()),
		"remote ref should now point at the new commit")
}
