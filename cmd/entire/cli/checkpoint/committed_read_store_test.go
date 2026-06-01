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

	syncV1CustomRefForRead(context.Background(), repo)

	got, ok := customRefHash(t, repo)
	require.True(t, ok, "custom ref should be seeded from v1")
	assert.Equal(t, h, got)
}

func TestSyncV1CustomRefForRead_SeedsFromOriginWhenLocalV1Missing(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	h := commitFile(t, repo, dir, "f.txt", "v1", "init")
	// Only the origin remote-tracking branch exists (fresh clone), no local v1.
	require.NoError(t, repo.Storer.SetReference(
		plumbing.NewHashReference(plumbing.NewRemoteReferenceName("origin", paths.MetadataBranchName), h)))

	syncV1CustomRefForRead(context.Background(), repo)

	got, ok := customRefHash(t, repo)
	require.True(t, ok, "custom ref should be seeded from origin v1 when local v1 is missing")
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

	syncV1CustomRefForRead(context.Background(), repo)

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

	syncV1CustomRefForRead(context.Background(), repo)

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

	syncV1CustomRefForRead(context.Background(), repo)

	got, _ := customRefHash(t, repo)
	assert.Equal(t, second, got, "non-ancestor custom ref must not be rewound")
}

func TestSyncV1CustomRefForRead_NoV1TipNoOp(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	commitFile(t, repo, dir, "f.txt", "v1", "init")
	// No local or origin v1 metadata branch set.

	syncV1CustomRefForRead(context.Background(), repo)

	_, ok := customRefHash(t, repo)
	assert.False(t, ok, "custom ref must not be created when no v1 tip is available")
}

func TestSyncV1CustomRefForRead_WriteFailureLeavesRefUnset(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	h := commitFile(t, repo, dir, "f.txt", "v1", "init")
	setV1Branch(t, repo, h)

	// Block creation of refs/entire/* by occupying the path with a file.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "refs", "entire"), []byte("blocked"), 0o644))

	// Must not panic; the custom ref simply stays unset.
	syncV1CustomRefForRead(context.Background(), repo)

	_, ok := customRefHash(t, repo)
	assert.False(t, ok, "custom ref must not exist when the write was blocked")
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

// TestNewCommittedReadStore_ReadsV11ForRemoteOnlyMetadata verifies that on a
// repo whose v1 metadata exists only as origin/entire/checkpoints/v1 (no local
// v1 branch), v1.1 mode seeds the custom ref from the remote-tracking tip and
// reads through the v1.1 ref — without falling back to a v1 store.
//
// Not parallel: uses t.Chdir().
func TestNewCommittedReadStore_ReadsV11ForRemoteOnlyMetadata(t *testing.T) {
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

	// Move the v1 metadata to origin-only: copy the local branch to the remote
	// tracking ref, then remove the local branch.
	v1RefName := plumbing.NewBranchReferenceName(paths.MetadataBranchName)
	v1Ref, err := repo.Reference(v1RefName, true)
	require.NoError(t, err)
	require.NoError(t, repo.Storer.SetReference(
		plumbing.NewHashReference(plumbing.NewRemoteReferenceName("origin", paths.MetadataBranchName), v1Ref.Hash())))
	require.NoError(t, repo.Storer.RemoveReference(v1RefName))

	readStore := NewCommittedReadStore(context.Background(), repo)
	require.Equal(t, plumbing.ReferenceName(paths.MetadataRefName), readStore.CommittedReadRef(),
		"v1.1 mode must read through the custom ref, not fall back to v1")

	summary, err := readStore.ReadCommitted(context.Background(), cpID)
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, cpID, summary.CheckpointID)
}

// TestNewCommittedReadStore_BindsV11WhenSyncFails verifies that when the v1.1
// custom ref cannot be written (sync failure), v1.1 mode still binds reads to
// the custom ref rather than falling back to v1.
//
// Not parallel: uses t.Chdir().
func TestNewCommittedReadStore_BindsV11WhenSyncFails(t *testing.T) {
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
	// Block creation of the v1.1 custom ref so the sync cannot seed it.
	require.NoError(t, os.WriteFile(filepath.Join(dir, ".git", "refs", "entire"), []byte("blocked"), 0o644))

	readStore := NewCommittedReadStore(context.Background(), repo)
	require.Equal(t, plumbing.ReferenceName(paths.MetadataRefName), readStore.CommittedReadRef(),
		"v1.1 mode must not fall back to v1 even when the custom ref cannot be synced")

	// With no v1.1 ref available and no v1 fallback, the read finds nothing
	// rather than silently returning the v1 checkpoint.
	summary, err := readStore.ReadCommitted(context.Background(), cpID)
	require.NoError(t, err)
	assert.Nil(t, summary, "read must not fall back to v1 when the custom ref is unavailable")
}

// TestNewCommittedReadStore_BindsV11WhenCustomRefDiverges verifies that a
// diverged custom ref is read as-is (bound to v1.1), never falling back to v1.
//
// Not parallel: uses t.Chdir().
func TestNewCommittedReadStore_BindsV11WhenCustomRefDiverges(t *testing.T) {
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
	// Point the custom ref at an unrelated commit that is not an ancestor of v1.
	setCustomRefHash(t, repo, divergedHash)

	readStore := NewCommittedReadStore(context.Background(), repo)
	require.Equal(t, plumbing.ReferenceName(paths.MetadataRefName), readStore.CommittedReadRef(),
		"v1.1 mode must read the diverged custom ref, not fall back to v1")

	// The checkpoint lives on v1, not on the diverged custom ref, so the v1.1
	// read does not find it (no v1 fallback).
	summary, err := readStore.ReadCommitted(context.Background(), cpID)
	require.NoError(t, err)
	assert.Nil(t, summary, "diverged custom ref read must not fall back to v1")
}
