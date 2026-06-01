package checkpoint

import (
	"context"
	"log/slog"

	"github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"

	"github.com/entireio/cli/cmd/entire/cli/logging"
	"github.com/entireio/cli/cmd/entire/cli/paths"
	"github.com/entireio/cli/cmd/entire/cli/settings"
)

// NewCommittedReadStore returns a GitStore for reading committed checkpoints,
// honoring the checkpoints_version setting.
//
// When v1.1 is enabled it first brings the local-only v1.1 custom ref up to the
// v1 branch tip (sync-then-read). If the custom ref cannot be synced, or if it
// has diverged from v1, reads fall back to the default v1 store so v1 remains
// the source of truth and the existing remote-tracking fallback still applies.
// When v1.1 is not enabled, this is the default v1 read store.
//
// Currently only the `entire explain` command reads through this factory. Other
// read-only commands stay on NewGitStore (v1) for now and will be migrated in
// follow-up work. Write and read-modify-write paths always use NewGitStore (v1)
// plus the existing write-time mirror.
func NewCommittedReadStore(ctx context.Context, repo *git.Repository) *GitStore {
	if !settings.MirrorsToV1CustomRef(ctx) {
		return NewGitStore(repo)
	}
	if syncV1CustomRefForRead(ctx, repo) {
		return NewGitStoreWithRef(repo, plumbing.ReferenceName(paths.MetadataRefName))
	}
	return NewGitStore(repo)
}

// syncV1CustomRefForRead advances the local-only v1.1 custom ref to the v1
// branch tip before a read so v1.1 reflects v1's current history. It mirrors the
// write-time mirror's safety rules: seed when missing, advance when the custom
// ref is an ancestor of v1, and no-op when equal. A diverged custom ref is left
// untouched, but is not used for reads. All failures are logged and reported as
// false so callers can fall back to the v1 store.
func syncV1CustomRefForRead(ctx context.Context, repo *git.Repository) bool {
	v1Ref, err := repo.Reference(plumbing.NewBranchReferenceName(paths.MetadataBranchName), true)
	if err != nil {
		// No v1 branch yet — nothing to mirror. Expected on a fresh repo.
		logging.Debug(ctx, "v1.1 read sync skipped: v1 branch unavailable",
			slog.String("error", err.Error()))
		return false
	}

	customRefName := plumbing.ReferenceName(paths.MetadataRefName)
	customRef, err := repo.Reference(customRefName, false)
	if err != nil {
		// Custom ref missing — seed it at the v1 tip.
		return setCustomRef(ctx, repo, customRefName, v1Ref.Hash())
	}

	if customRef.Hash() == v1Ref.Hash() {
		return true // already current
	}

	customCommit, err := repo.CommitObject(customRef.Hash())
	if err != nil {
		logging.Warn(ctx, "v1.1 read sync skipped: custom ref commit unreadable",
			slog.String("ref", paths.MetadataRefName),
			slog.String("error", err.Error()))
		return false
	}
	v1Commit, err := repo.CommitObject(v1Ref.Hash())
	if err != nil {
		logging.Warn(ctx, "v1.1 read sync skipped: v1 commit unreadable",
			slog.String("error", err.Error()))
		return false
	}

	isAncestor, err := customCommit.IsAncestor(v1Commit)
	if err != nil {
		logging.Warn(ctx, "v1.1 read sync skipped: ancestry check failed",
			slog.String("error", err.Error()))
		return false
	}
	if !isAncestor {
		// Diverged from v1: leave the custom ref untouched rather than clobber
		// local-only history. Fall back to v1 so v1 remains the source of truth.
		logging.Warn(ctx, "v1.1 custom ref diverged from v1; falling back to v1 reads",
			slog.String("ref", paths.MetadataRefName),
			slog.String("custom_hash", customRef.Hash().String()),
			slog.String("v1_hash", v1Ref.Hash().String()))
		return false
	}

	return setCustomRef(ctx, repo, customRefName, v1Ref.Hash())
}

// setCustomRef points refName at hash, logging success at debug and failure at
// warning. The return value reports whether the ref now points at hash.
func setCustomRef(ctx context.Context, repo *git.Repository, refName plumbing.ReferenceName, hash plumbing.Hash) bool {
	if err := repo.Storer.SetReference(plumbing.NewHashReference(refName, hash)); err != nil {
		logging.Warn(ctx, "v1.1 read sync failed to advance custom ref",
			slog.String("ref", refName.String()),
			slog.String("error", err.Error()))
		return false
	}
	logging.Debug(ctx, "v1.1 custom ref synced for read",
		slog.String("ref", refName.String()),
		slog.String("hash", hash.String()))
	return true
}
