package checkpoint

import (
	"context"
	"testing"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/entireio/cli/cmd/entire/cli/checkpoint/id"
	"github.com/entireio/cli/cmd/entire/cli/testutil"
	"github.com/entireio/cli/redact"
)

func newRefsStore(t *testing.T) *gitRefsStore {
	t.Helper()
	dir := t.TempDir()
	testutil.InitRepo(t, dir)
	testutil.WriteFile(t, dir, "README.md", "# test")
	testutil.GitAdd(t, dir, "README.md")
	testutil.GitCommit(t, dir, "init")
	repo, err := git.PlainOpen(dir)
	require.NoError(t, err)
	return newGitRefsStore(repo)
}

func refsWrite(t *testing.T, store *gitRefsStore, cid id.CheckpointID, sessionID, transcript string) {
	t.Helper()
	require.NoError(t, store.Write(context.Background(), Session{
		CheckpointID: cid,
		SessionID:    sessionID,
		Strategy:     "manual-commit",
		Transcript:   redact.AlreadyRedacted([]byte(transcript)),
		Prompts:      []string{"do the thing"},
		FilesTouched: []string{"a.go"},
		AuthorName:   "Test Author",
		AuthorEmail:  "test@example.com",
	}))
}

func TestGitRefsStore_WriteEnqueuesForPush(t *testing.T) {
	t.Parallel()
	store := newRefsStore(t)
	cid := id.MustCheckpointID("a1b2c3d4e5f6")

	refsWrite(t, store, cid, "sess-1", "transcript")

	q, err := PushQueueForRepo(context.Background(), store.repo)
	require.NoError(t, err)
	refs, err := q.Drain()
	require.NoError(t, err)
	assert.Contains(t, refs, RefName(cid), "a session write should enqueue its checkpoint ref for push")
}

func TestGitRefsStore_OnDemandRefFetch(t *testing.T) {
	t.Parallel()
	store := newRefsStore(t)
	ctx := context.Background()
	cid := id.MustCheckpointID("a1b2c3d4e5f6")

	refsWrite(t, store, cid, "sess-1", "transcript")
	ref, err := store.repo.Reference(RefName(cid), true)
	require.NoError(t, err)
	commitHash := ref.Hash()

	// Simulate "not present locally" by dropping the ref (the commit object
	// survives, so a fetch can restore the ref).
	require.NoError(t, store.repo.Storer.RemoveReference(RefName(cid)))

	// No fetcher configured: read resolves to not-found (nil summary).
	summary, err := store.Read(ctx, cid)
	require.NoError(t, err)
	assert.Nil(t, summary, "missing ref with no fetcher reads as not-found")

	// A fetcher that restores the ref makes the read succeed, and is invoked once.
	fetched := 0
	store.SetRefFetcher(func(_ context.Context, rn plumbing.ReferenceName) error {
		fetched++
		return store.repo.Storer.SetReference(plumbing.NewHashReference(rn, commitHash))
	})
	summary, err = store.Read(ctx, cid)
	require.NoError(t, err)
	require.NotNil(t, summary, "ref should resolve after on-demand fetch")
	assert.Equal(t, cid, summary.CheckpointID)
	assert.Equal(t, 1, fetched, "fetcher invoked once for the missing ref")
}

func TestGitRefsStore_OnDemandRefFetch_FailureIsNotFound(t *testing.T) {
	t.Parallel()
	store := newRefsStore(t)
	store.SetRefFetcher(func(_ context.Context, _ plumbing.ReferenceName) error {
		return assert.AnError // fetch fails (e.g. offline / unknown checkpoint)
	})

	summary, err := store.Read(context.Background(), id.MustCheckpointID("ffffffffffff"))
	require.NoError(t, err)
	assert.Nil(t, summary, "a failed fetch reads as not-found, not an error")
}

func TestGitRefsStore_WriteAllVariantsAndRead(t *testing.T) {
	t.Parallel()
	store := newRefsStore(t)
	ctx := context.Background()
	cid := id.MustCheckpointID("a1b2c3d4e5f6")
	const sessionID = "sess-1"

	refsWrite(t, store, cid, sessionID, "initial transcript")
	require.NoError(t, store.Write(ctx, SessionTranscript{
		CheckpointID: cid, SessionID: sessionID,
		Transcript: redact.AlreadyRedacted([]byte("final transcript")),
		Prompts:    []string{"do the thing"},
	}))
	require.NoError(t, store.Write(ctx, SessionSummary{
		CheckpointID: cid, Summary: &Summary{Intent: "intent-x", Outcome: "outcome-y"},
	}))
	require.NoError(t, store.Write(ctx, CheckpointAttribution{
		CheckpointID: cid, Attribution: &Attribution{AgentLines: 7, AgentPercentage: 70},
	}))

	// The per-checkpoint ref exists at the sharded name.
	_, err := store.repo.Reference(RefName(cid), true)
	require.NoError(t, err, "checkpoint ref should exist")

	summary, err := store.Read(ctx, cid)
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, CheckpointVersionRefsV1, summary.CheckpointVersion)
	require.Len(t, summary.Sessions, 1)
	require.NotNil(t, summary.CombinedAttribution)
	assert.Equal(t, 7, summary.CombinedAttribution.AgentLines)

	content, err := store.ReadSessionContent(ctx, cid, 0)
	require.NoError(t, err)
	assert.Equal(t, []byte("final transcript"), content.Transcript)

	meta, err := store.ReadSessionMetadata(ctx, cid, 0)
	require.NoError(t, err)
	require.NotNil(t, meta.Summary)
	assert.Equal(t, "intent-x", meta.Summary.Intent)
}

func TestGitRefsStore_RefSharding(t *testing.T) {
	t.Parallel()
	store := newRefsStore(t)
	ctx := context.Background()

	// A legacy hex checkpoint stores under a first-two-char shard and round-trips.
	legacy := id.MustCheckpointID("a1b2c3d4e5f6")
	refsWrite(t, store, legacy, "s-legacy", "t")
	_, err := store.repo.Reference("refs/entire/checkpoints/a1/a1b2c3d4e5f6", true)
	require.NoError(t, err)
	summary, err := store.Read(ctx, legacy)
	require.NoError(t, err)
	require.NotNil(t, summary)
	assert.Equal(t, legacy, summary.CheckpointID)

	// ULIDs shard on the last two chars (the ref namespace is ULID-ready). Only
	// the ref-naming layer is asserted here: storing a ULID checkpoint also needs
	// id.CheckpointID JSON (un)marshaling to accept ULIDs, which lands with the
	// deferred ULID-generation switch.
	ulid := id.CheckpointID("01KVBJCWYA4YW6J5M9GP655HZN")
	assert.Equal(t, "refs/entire/checkpoints/ZN/01KVBJCWYA4YW6J5M9GP655HZN", RefName(ulid).String())
}

func TestGitRefsStore_MultipleSessions(t *testing.T) {
	t.Parallel()
	store := newRefsStore(t)
	ctx := context.Background()
	cid := id.MustCheckpointID("abcdef012345")

	refsWrite(t, store, cid, "sess-1", "first")
	refsWrite(t, store, cid, "sess-2", "second")

	summary, err := store.Read(ctx, cid)
	require.NoError(t, err)
	require.Len(t, summary.Sessions, 2, "two sessions should occupy two numbered dirs")

	infos, err := store.List(ctx)
	require.NoError(t, err)
	require.Len(t, infos, 1)
	assert.Equal(t, 2, infos[0].SessionCount)
}

func TestGitRefsStore_SeparateCheckpointsSeparateRefs(t *testing.T) {
	t.Parallel()
	store := newRefsStore(t)
	ctx := context.Background()
	cid1 := id.MustCheckpointID("a1b2c3d4e5f6")
	cid2 := id.MustCheckpointID("f6e5d4c3b2a1")

	refsWrite(t, store, cid1, "s1", "t1")
	refsWrite(t, store, cid2, "s2", "t2")

	_, err := store.repo.Reference("refs/entire/checkpoints/a1/a1b2c3d4e5f6", true)
	require.NoError(t, err)
	_, err = store.repo.Reference("refs/entire/checkpoints/f6/f6e5d4c3b2a1", true)
	require.NoError(t, err)

	infos, err := store.List(ctx)
	require.NoError(t, err)
	assert.Len(t, infos, 2)
}

func TestGitRefsStore_PerCheckpointHistory(t *testing.T) {
	t.Parallel()
	store := newRefsStore(t)
	ctx := context.Background()
	cid := id.MustCheckpointID("a1b2c3d4e5f6")

	refsWrite(t, store, cid, "sess-1", "t")

	// First write is an orphan (no parent).
	ref, err := store.repo.Reference(RefName(cid), true)
	require.NoError(t, err)
	first, err := store.repo.CommitObject(ref.Hash())
	require.NoError(t, err)
	require.Empty(t, first.ParentHashes, "first checkpoint commit should be an orphan")

	// A backfill advances the same ref, preserving history.
	require.NoError(t, store.Write(ctx, SessionSummary{
		CheckpointID: cid, Summary: &Summary{Intent: "later"},
	}))
	ref, err = store.repo.Reference(RefName(cid), true)
	require.NoError(t, err)
	second, err := store.repo.CommitObject(ref.Hash())
	require.NoError(t, err)
	require.Len(t, second.ParentHashes, 1, "backfill should parent on the prior tip")
	assert.Equal(t, first.Hash, second.ParentHashes[0])
}

func TestGitRefsStore_GetCheckpointAuthor(t *testing.T) {
	t.Parallel()
	store := newRefsStore(t)
	ctx := context.Background()
	cid := id.MustCheckpointID("a1b2c3d4e5f6")

	refsWrite(t, store, cid, "sess-1", "t")

	author, err := store.GetCheckpointAuthor(ctx, cid)
	require.NoError(t, err)
	assert.Equal(t, "Test Author", author.Name)
	assert.Equal(t, "test@example.com", author.Email)
}

func TestGitRefsStore_BackfillUnknownCheckpointNotFound(t *testing.T) {
	t.Parallel()
	store := newRefsStore(t)
	ctx := context.Background()
	cid := id.MustCheckpointID("a1b2c3d4e5f6")

	err := store.Write(ctx, SessionTranscript{
		CheckpointID: cid, SessionID: "s",
		Transcript: redact.AlreadyRedacted([]byte("x")),
	})
	require.ErrorIs(t, err, ErrCheckpointNotFound)

	err = store.Write(ctx, SessionSummary{CheckpointID: cid, Summary: &Summary{Intent: "x"}})
	require.ErrorIs(t, err, ErrCheckpointNotFound)

	err = store.Write(ctx, CheckpointAttribution{CheckpointID: cid, Attribution: &Attribution{AgentLines: 1}})
	require.ErrorIs(t, err, ErrCheckpointNotFound)

	// Read of an absent checkpoint is (nil, nil) per the contract.
	summary, err := store.Read(ctx, cid)
	require.NoError(t, err)
	assert.Nil(t, summary)
}
