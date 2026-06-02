package strategy

import (
	"testing"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/entireio/cli/cmd/entire/cli/checkpoint"
	"github.com/entireio/cli/cmd/entire/cli/checkpoint/id"
	"github.com/entireio/cli/cmd/entire/cli/paths"
	"github.com/entireio/cli/redact"
)

// getCommittedReadStore must resolve the committed read ref from the active
// checkpoints_version: the local-only v1.1 custom ref when opted in, else the
// v1 branch. This is the ref the rewind read path resolves against.
// Not parallel: setupV1CustomRefRepo uses t.Chdir().
func TestGetCommittedReadStore_ResolvesReadRef(t *testing.T) {
	v1Branch := plumbing.NewBranchReferenceName(paths.MetadataBranchName)
	custom := plumbing.ReferenceName(paths.MetadataRefName)
	tests := []struct {
		name    string
		version string // checkpoints_version value; "" omits it
		want    plumbing.ReferenceName
	}{
		{"v1 only", "", v1Branch},
		{"opted in to 1.1", `"1.1"`, custom},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := setupV1CustomRefRepo(t, tt.version)
			s := NewManualCommitStrategy()
			store := s.getCommittedReadStore(t.Context(), repo)
			assert.Equal(t, tt.want, store.CommittedReadRef())
		})
	}
}

// seedCheckpointOnlyOnCustomRef writes a committed checkpoint, points the v1.1
// custom ref at it, then drops the v1 branch so the checkpoint is reachable only
// through the custom ref. A v1-branch read then finds nothing.
func seedCheckpointOnlyOnCustomRef(t *testing.T, repo *git.Repository) id.CheckpointID {
	t.Helper()
	cpID := id.MustCheckpointID("aabbccdd1122")
	require.NoError(t, checkpoint.NewGitStore(repo).WriteCommitted(t.Context(), checkpoint.WriteCommittedOptions{
		CheckpointID: cpID,
		SessionID:    "session-v11",
		Strategy:     "manual-commit",
		Transcript:   redact.AlreadyRedacted([]byte(`{"type":"user","message":{"content":"hi"}}` + "\n")),
		AuthorName:   "Test",
		AuthorEmail:  "test@example.com",
	}))
	v1Hash := v1MetadataBranchHash(t, repo)
	require.NoError(t, repo.Storer.SetReference(
		plumbing.NewHashReference(plumbing.ReferenceName(paths.MetadataRefName), v1Hash)))
	require.NoError(t, repo.Storer.RemoveReference(plumbing.NewBranchReferenceName(paths.MetadataBranchName)))
	return cpID
}

// listCheckpoints feeds the logs-only rewind points. When opted in to 1.1 it
// must read committed checkpoints from the v1.1 custom ref, not the v1 branch.
// Not parallel: setupV1CustomRefRepo uses t.Chdir().
func TestListCheckpoints_ReadsV11CustomRefWhenOptedIn(t *testing.T) {
	repo := setupV1CustomRefRepo(t, `"1.1"`)
	cpID := seedCheckpointOnlyOnCustomRef(t, repo)

	infos, err := NewManualCommitStrategy().listCheckpoints(t.Context())
	require.NoError(t, err)
	require.Len(t, infos, 1, "checkpoint on the v1.1 custom ref should be read when opted in")
	assert.Equal(t, cpID, infos[0].CheckpointID)
}

// Without opting in to 1.1, committed reads resolve against the v1 branch, so a
// checkpoint living only on the custom ref is invisible.
// Not parallel: setupV1CustomRefRepo uses t.Chdir().
func TestListCheckpoints_IgnoresV11CustomRefWhenNotOptedIn(t *testing.T) {
	repo := setupV1CustomRefRepo(t, "") // v1 only
	seedCheckpointOnlyOnCustomRef(t, repo)

	infos, err := NewManualCommitStrategy().listCheckpoints(t.Context())
	require.NoError(t, err)
	assert.Empty(t, infos, "custom-ref checkpoints must not be read when not opted in")
}
