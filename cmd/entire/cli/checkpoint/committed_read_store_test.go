package checkpoint

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/go-git/go-git/v6/plumbing/object"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/entireio/cli/cmd/entire/cli/checkpoint/id"
	"github.com/entireio/cli/cmd/entire/cli/paths"
	"github.com/entireio/cli/cmd/entire/cli/testutil"
	"github.com/entireio/cli/redact"
)

// commitFile adds path with content to the worktree and commits it, returning
// the new commit hash. Successive calls build a linear ancestry chain.
func commitFile(t *testing.T, repo *git.Repository, dir, path, content, msg string) plumbing.Hash {
	t.Helper()
	require.NoError(t, os.WriteFile(filepath.Join(dir, path), []byte(content), 0o644))
	wt, err := repo.Worktree()
	require.NoError(t, err)
	_, err = wt.Add(path)
	require.NoError(t, err)
	h, err := wt.Commit(msg, &git.CommitOptions{
		Author: &object.Signature{Name: "Test", Email: "test@test.com"},
	})
	require.NoError(t, err)
	return h
}

func setV1Branch(t *testing.T, repo *git.Repository, hash plumbing.Hash) {
	t.Helper()
	require.NoError(t, repo.Storer.SetReference(
		plumbing.NewHashReference(plumbing.NewBranchReferenceName(paths.MetadataBranchName), hash)))
}

func setCustomRefHash(t *testing.T, repo *git.Repository, hash plumbing.Hash) {
	t.Helper()
	require.NoError(t, repo.Storer.SetReference(
		plumbing.NewHashReference(plumbing.ReferenceName(paths.MetadataRefName), hash)))
}

func customRefHash(t *testing.T, repo *git.Repository) (plumbing.Hash, bool) {
	t.Helper()
	ref, err := repo.Reference(plumbing.ReferenceName(paths.MetadataRefName), true)
	if err != nil {
		return plumbing.ZeroHash, false
	}
	return ref.Hash(), true
}

func TestNewGitStore_DefaultsToV1Branch(t *testing.T) {
	t.Parallel()
	store := NewGitStore(nil)
	assert.Equal(t, plumbing.NewBranchReferenceName(paths.MetadataBranchName), store.CommittedReadRef())
}

func TestNewGitStoreWithRef(t *testing.T) {
	t.Parallel()
	custom := plumbing.ReferenceName(paths.MetadataRefName)
	assert.Equal(t, custom, NewGitStoreWithRef(nil, custom).CommittedReadRef())
}

func TestSyncV1CustomRefForRead_SeedsWhenMissing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	h := commitFile(t, repo, dir, "f.txt", "v1", "init")
	setV1Branch(t, repo, h)

	synced := syncV1CustomRefForRead(context.Background(), repo)

	require.True(t, synced, "custom ref sync should succeed")
	got, ok := customRefHash(t, repo)
	require.True(t, ok, "custom ref should be seeded from v1")
	assert.Equal(t, h, got)
}

func TestSyncV1CustomRefForRead_NoopWhenEqual(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	h := commitFile(t, repo, dir, "f.txt", "v1", "init")
	setV1Branch(t, repo, h)
	setCustomRefHash(t, repo, h)

	synced := syncV1CustomRefForRead(context.Background(), repo)

	require.True(t, synced, "equal refs should be usable for reads")
	got, _ := customRefHash(t, repo)
	assert.Equal(t, h, got)
}

func TestSyncV1CustomRefForRead_AdvancesWhenAncestor(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	old := commitFile(t, repo, dir, "f.txt", "v1", "init")
	setCustomRefHash(t, repo, old)
	newHash := commitFile(t, repo, dir, "f2.txt", "more", "second")
	setV1Branch(t, repo, newHash)
	require.NotEqual(t, old, newHash)

	synced := syncV1CustomRefForRead(context.Background(), repo)

	require.True(t, synced, "ancestor custom ref should advance for reads")
	got, _ := customRefHash(t, repo)
	assert.Equal(t, newHash, got, "custom ref should advance to the v1 tip")
}

func TestSyncV1CustomRefForRead_LeavesNonAncestorRef(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	first := commitFile(t, repo, dir, "f.txt", "v1", "init")
	second := commitFile(t, repo, dir, "f2.txt", "more", "second")
	// v1 points at the parent while the custom ref is ahead at the child: the
	// custom ref is not an ancestor of v1, so it must be left untouched.
	setV1Branch(t, repo, first)
	setCustomRefHash(t, repo, second)

	synced := syncV1CustomRefForRead(context.Background(), repo)

	require.False(t, synced, "non-ancestor custom ref should not be used for reads")
	got, _ := customRefHash(t, repo)
	assert.Equal(t, second, got, "non-ancestor custom ref must not be rewound")
}

func TestSyncV1CustomRefForRead_V1MissingNoOp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	commitFile(t, repo, dir, "f.txt", "v1", "init")
	// No v1 metadata branch set.

	synced := syncV1CustomRefForRead(context.Background(), repo)

	require.False(t, synced, "missing v1 branch should fall back to the v1 store")
	_, ok := customRefHash(t, repo)
	assert.False(t, ok, "custom ref must not be created when the v1 branch is absent")
}

func TestSyncV1CustomRefForRead_WriteFailureReturnsFalse(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	h := commitFile(t, repo, dir, "f.txt", "v1", "init")
	setV1Branch(t, repo, h)

	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "refs", "entire"), []byte("blocked"), 0o644))

	synced := syncV1CustomRefForRead(context.Background(), repo)
	require.False(t, synced, "blocked custom-ref write should force v1 fallback")
}

// writeSettings writes .entire/settings.json with the given checkpoints_version
// (empty string omits the option) into dir.
func writeSettings(t *testing.T, dir, version string) {
	t.Helper()
	body := `{"enabled": true}`
	if version != "" {
		body = `{"enabled": true, "strategy_options": {"checkpoints_version": ` + version + `}}`
	}
	entireDir := filepath.Join(dir, ".entire")
	require.NoError(t, os.MkdirAll(entireDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(entireDir, paths.SettingsFileName), []byte(body), 0o644))
}

// Not parallel: uses t.Chdir() so settings.Load resolves the test repo.
func TestNewCommittedReadStore_SelectsRefByVersion(t *testing.T) {
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	h := commitFile(t, repo, dir, "f.txt", "v1", "init")
	setV1Branch(t, repo, h)
	t.Chdir(dir)

	writeSettings(t, dir, "") // v1 only
	v1Store := NewCommittedReadStore(context.Background(), repo)
	assert.Equal(t, plumbing.NewBranchReferenceName(paths.MetadataBranchName), v1Store.CommittedReadRef())

	writeSettings(t, dir, `"1.1"`)
	v11Store := NewCommittedReadStore(context.Background(), repo)
	assert.Equal(t, plumbing.ReferenceName(paths.MetadataRefName), v11Store.CommittedReadRef())
}

// Not parallel: uses t.Chdir().
func TestNewCommittedReadStore_ReadsMirroredData(t *testing.T) {
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	commitFile(t, repo, dir, "README.md", "# Test", "init")
	t.Chdir(dir)
	writeSettings(t, dir, `"1.1"`)

	// Write a committed checkpoint via the default v1 store.
	cpID := id.MustCheckpointID("a1b2c3d4e5f6")
	require.NoError(t, NewGitStore(repo).WriteCommitted(context.Background(), WriteCommittedOptions{
		CheckpointID: cpID,
		SessionID:    "session-001",
		Strategy:     "manual-commit",
		Transcript:   redact.AlreadyRedacted([]byte("transcript line 1\n")),
		Prompts:      []string{"initial prompt"},
		AuthorName:   "Test",
		AuthorEmail:  "test@test.com",
	}))

	// The v1.1 read store sync-then-reads the same checkpoint without any
	// separate mirror step, and the custom ref ends at the v1 tip.
	readStore := NewCommittedReadStore(context.Background(), repo)
	require.Equal(t, plumbing.ReferenceName(paths.MetadataRefName), readStore.CommittedReadRef())

	// The successful read-equivalence below already proves the factory
	// sync-then-read populated the v1.1 ref from v1; the ref-position itself is
	// covered by the TestSyncV1CustomRefForRead_* cases.
	v1Summary, err := NewGitStore(repo).ReadCommitted(context.Background(), cpID)
	require.NoError(t, err)
	v11Summary, err := readStore.ReadCommitted(context.Background(), cpID)
	require.NoError(t, err)
	assert.Equal(t, v1Summary, v11Summary)
}

// Not parallel: uses t.Chdir().
func TestNewCommittedReadStore_FallsBackToV1ForRemoteOnlyMetadata(t *testing.T) {
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	commitFile(t, repo, dir, "README.md", "# Test", "init")
	t.Chdir(dir)
	writeSettings(t, dir, `"1.1"`)

	cpID := id.MustCheckpointID("b1b2c3d4e5f6")
	require.NoError(t, NewGitStore(repo).WriteCommitted(context.Background(), WriteCommittedOptions{
		CheckpointID: cpID,
		SessionID:    "session-remote",
		Strategy:     "manual-commit",
		Transcript:   redact.AlreadyRedacted([]byte("remote transcript\n")),
		Prompts:      []string{"remote prompt"},
		AuthorName:   "Test",
		AuthorEmail:  "test@test.com",
	}))

	v1RefName := plumbing.NewBranchReferenceName(paths.MetadataBranchName)
	v1Ref, err := repo.Reference(v1RefName, true)
	require.NoError(t, err)
	require.NoError(t, repo.Storer.SetReference(
		plumbing.NewHashReference(plumbing.NewRemoteReferenceName("origin", paths.MetadataBranchName), v1Ref.Hash())))
	require.NoError(t, repo.Storer.RemoveReference(v1RefName))

	readStore := NewCommittedReadStore(context.Background(), repo)
	require.Equal(t, plumbing.NewBranchReferenceName(paths.MetadataBranchName), readStore.CommittedReadRef())

	summary, err := readStore.ReadCommitted(context.Background(), cpID)
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, cpID, summary.CheckpointID)
}

// Not parallel: uses t.Chdir().
func TestNewCommittedReadStore_FallsBackToV1WhenCustomRefWriteFails(t *testing.T) {
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	commitFile(t, repo, dir, "README.md", "# Test", "init")
	t.Chdir(dir)
	writeSettings(t, dir, `"1.1"`)

	cpID := id.MustCheckpointID("c1c2c3d4e5f6")
	require.NoError(t, NewGitStore(repo).WriteCommitted(context.Background(), WriteCommittedOptions{
		CheckpointID: cpID,
		SessionID:    "session-write-fails",
		Strategy:     "manual-commit",
		Transcript:   redact.AlreadyRedacted([]byte("transcript\n")),
		Prompts:      []string{"prompt"},
		AuthorName:   "Test",
		AuthorEmail:  "test@test.com",
	}))
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "refs", "entire"), []byte("blocked"), 0o644))

	readStore := NewCommittedReadStore(context.Background(), repo)
	require.Equal(t, plumbing.NewBranchReferenceName(paths.MetadataBranchName), readStore.CommittedReadRef())

	summary, err := readStore.ReadCommitted(context.Background(), cpID)
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, cpID, summary.CheckpointID)
}

// Not parallel: uses t.Chdir().
func TestNewCommittedReadStore_FallsBackToV1WhenCustomRefDiverges(t *testing.T) {
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	divergedHash := commitFile(t, repo, dir, "README.md", "# Diverged", "worktree commit")
	t.Chdir(dir)
	writeSettings(t, dir, `"1.1"`)

	cpID := id.MustCheckpointID("d1d2c3d4e5f6")
	require.NoError(t, NewGitStore(repo).WriteCommitted(context.Background(), WriteCommittedOptions{
		CheckpointID: cpID,
		SessionID:    "session-diverged",
		Strategy:     "manual-commit",
		Transcript:   redact.AlreadyRedacted([]byte("transcript\n")),
		Prompts:      []string{"prompt"},
		AuthorName:   "Test",
		AuthorEmail:  "test@test.com",
	}))
	setCustomRefHash(t, repo, divergedHash)

	readStore := NewCommittedReadStore(context.Background(), repo)
	require.Equal(t, plumbing.NewBranchReferenceName(paths.MetadataBranchName), readStore.CommittedReadRef())

	summary, err := readStore.ReadCommitted(context.Background(), cpID)
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, cpID, summary.CheckpointID)
}
