package strategy

import (
	"context"
	"testing"
	"time"

	"github.com/entireio/cli/cmd/entire/cli/checkpoint"
	"github.com/entireio/cli/cmd/entire/cli/testutil"
	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/stretchr/testify/require"
)

// shadowCleanupEnv bundles the setup needed for testing post-push shadow
// branch cleanup: a git repo, a known base commit, and helpers to
// create shadow refs + matching session states.
type shadowCleanupEnv struct {
	t        *testing.T
	repo     *git.Repository
	dir      string
	baseHash plumbing.Hash
}

func newShadowCleanupEnv(t *testing.T) *shadowCleanupEnv {
	t.Helper()
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	t.Chdir(dir)

	emptyTree := plumbing.NewHash("4b825dc642cb6eb9a060e54bf8d69288fbee4904")
	baseHash, err := checkpoint.CreateCommit(context.Background(), repo, emptyTree, plumbing.ZeroHash, "initial commit", "test", "test@test.com")
	require.NoError(t, err)
	headRef := plumbing.NewSymbolicReference(plumbing.HEAD, plumbing.NewBranchReferenceName("main"))
	require.NoError(t, repo.Storer.SetReference(headRef))
	require.NoError(t, repo.Storer.SetReference(plumbing.NewHashReference(plumbing.NewBranchReferenceName("main"), baseHash)))
	return &shadowCleanupEnv{t: t, repo: repo, dir: dir, baseHash: baseHash}
}

// addShadowBranch creates a shadow branch for the given (base, worktreeID)
// pair and returns its derived name.
func (e *shadowCleanupEnv) addShadowBranch(baseCommit, worktreeID string) string {
	e.t.Helper()
	name := getShadowBranchNameForCommit(baseCommit, worktreeID)
	require.NoError(e.t, e.repo.Storer.SetReference(
		plumbing.NewHashReference(plumbing.NewBranchReferenceName(name), e.baseHash)))
	return name
}

// addSessionState writes a session state file. If ended is non-nil the
// session is treated as ended; pendingCheckpoints simulates the
// mid-finalize race window.
func (e *shadowCleanupEnv) addSessionState(sessionID, baseCommit, worktreeID string, ended *time.Time, pendingCheckpoints []string) {
	e.t.Helper()
	state := &SessionState{
		SessionID:         sessionID,
		BaseCommit:        baseCommit,
		WorktreeID:        worktreeID,
		StartedAt:         time.Now().Add(-time.Hour),
		EndedAt:           ended,
		TurnCheckpointIDs: pendingCheckpoints,
	}
	require.NoError(e.t, SaveSessionState(context.Background(), state))
}

func (e *shadowCleanupEnv) branchExists(name string) bool {
	e.t.Helper()
	_, err := e.repo.Reference(plumbing.NewBranchReferenceName(name), false)
	return err == nil
}

// Happy case: ended session with no pending checkpoints → shadow gone.
func TestCleanupPushedShadowBranches_FullyEndedDeleted(t *testing.T) {
	env := newShadowCleanupEnv(t)
	shadow := env.addShadowBranch(env.baseHash.String(), "")
	ended := time.Now().Add(-time.Minute)
	env.addSessionState("s1", env.baseHash.String(), "", &ended, nil)

	deleted, err := CleanupPushedShadowBranches(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, deleted)
	require.False(t, env.branchExists(shadow))
}

// Active session (EndedAt == nil) protects the shadow even if another
// session on the same branch already ended.
func TestCleanupPushedShadowBranches_ActiveSessionPreserved(t *testing.T) {
	env := newShadowCleanupEnv(t)
	shadow := env.addShadowBranch(env.baseHash.String(), "")
	ended := time.Now().Add(-time.Minute)
	env.addSessionState("s1-ended", env.baseHash.String(), "", &ended, nil)
	env.addSessionState("s2-active", env.baseHash.String(), "", nil, nil)

	deleted, err := CleanupPushedShadowBranches(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, deleted)
	require.True(t, env.branchExists(shadow))
}

// Mid-finalize race window: ended session with TurnCheckpointIDs still
// pending → preserve.
func TestCleanupPushedShadowBranches_PendingTurnCheckpointsPreserved(t *testing.T) {
	env := newShadowCleanupEnv(t)
	shadow := env.addShadowBranch(env.baseHash.String(), "")
	ended := time.Now().Add(-time.Minute)
	env.addSessionState("s1", env.baseHash.String(), "", &ended, []string{"a1b2c3d4e5f6"})

	deleted, err := CleanupPushedShadowBranches(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, deleted)
	require.True(t, env.branchExists(shadow))
}

// Orphaned shadow branch (no matching session state) gets cleaned up.
func TestCleanupPushedShadowBranches_OrphanedBranchDeleted(t *testing.T) {
	env := newShadowCleanupEnv(t)
	shadow := env.addShadowBranch(env.baseHash.String(), "")

	deleted, err := CleanupPushedShadowBranches(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, deleted)
	require.False(t, env.branchExists(shadow))
}

// Mixed: two shadow branches with different worktree IDs and different
// session statuses. The cleanup must delete only the safe one.
func TestCleanupPushedShadowBranches_MixedBranchesPartialDelete(t *testing.T) {
	env := newShadowCleanupEnv(t)
	preserved := env.addShadowBranch(env.baseHash.String(), "wt1")
	deletable := env.addShadowBranch(env.baseHash.String(), "wt2")
	ended := time.Now().Add(-time.Minute)
	env.addSessionState("s-active", env.baseHash.String(), "wt1", nil, nil)
	env.addSessionState("s-ended", env.baseHash.String(), "wt2", &ended, nil)

	deleted, err := CleanupPushedShadowBranches(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, deleted)
	require.True(t, env.branchExists(preserved))
	require.False(t, env.branchExists(deletable))
}

// No shadow branches → no-op, no error.
func TestCleanupPushedShadowBranches_NoBranches_NoOp(t *testing.T) {
	env := newShadowCleanupEnv(t)
	_ = env

	deleted, err := CleanupPushedShadowBranches(context.Background())
	require.NoError(t, err)
	require.Equal(t, 0, deleted)
}
