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
// When v1.1 is enabled, committed reads resolve against the local-only v1.1
// custom ref and NEVER fall back to v1: the store always binds to the v1.1 ref.
// Before returning, it best-effort syncs that ref up to the v1 tip
// (sync-then-read) so v1.1 carries v1's full history. v1 stays the source of
// truth via the write-time mirror; reads stay on v1.1 so the v1.1 path is
// genuinely exercised rather than masked by a v1 fallback. When v1.1 is not
// enabled, this is the default v1 read store.
//
// Currently only the `entire explain` command reads through this factory. Other
// read-only commands stay on NewGitStore (v1) for now and will be migrated in
// follow-up work. Write and read-modify-write paths always use NewGitStore (v1)
// plus the existing write-time mirror.
func NewCommittedReadStore(ctx context.Context, repo *git.Repository) *GitStore {
	if !settings.MirrorsToV1CustomRef(ctx) {
		return NewGitStore(repo)
	}
	syncV1CustomRefForRead(ctx, repo)
	return NewGitStoreWithRef(repo, plumbing.ReferenceName(paths.MetadataRefName))
}

// syncV1CustomRefForRead best-effort advances the local-only v1.1 custom ref to
// the v1 tip before a read so v1.1 reflects v1's current history. The v1 tip is
// taken from the local v1 branch, or from origin/entire/checkpoints/v1 when the
// local branch is absent (e.g. a fresh clone), so v1.1 can be seeded without a
// local write.
//
// It applies the write-time mirror's safety rules: seed when missing, advance
// when the custom ref is an ancestor of v1, and no-op when equal. A diverged
// custom ref is left untouched. All failures are logged, never returned: reads
// proceed against the v1.1 ref regardless (there is no v1 fallback), so a sync
// problem surfaces as missing/stale v1.1 data rather than being papered over.
func syncV1CustomRefForRead(ctx context.Context, repo *git.Repository) {
	v1Hash, ok := resolveV1Tip(repo)
	if !ok {
		// No v1 branch locally or on origin — nothing to seed from.
		logging.Debug(ctx, "v1.1 read sync skipped: no v1 tip available")
		return
	}

	customRefName := plumbing.ReferenceName(paths.MetadataRefName)
	customRef, err := repo.Reference(customRefName, false)
	if err != nil {
		// Custom ref missing — seed it at the v1 tip.
		setCustomRef(ctx, repo, customRefName, v1Hash)
		return
	}

	if customRef.Hash() == v1Hash {
		return // already current
	}

	customCommit, err := repo.CommitObject(customRef.Hash())
	if err != nil {
		logging.Warn(ctx, "v1.1 read sync skipped: custom ref commit unreadable",
			slog.String("ref", paths.MetadataRefName),
			slog.String("error", err.Error()))
		return
	}
	v1Commit, err := repo.CommitObject(v1Hash)
	if err != nil {
		logging.Warn(ctx, "v1.1 read sync skipped: v1 commit unreadable",
			slog.String("error", err.Error()))
		return
	}

	isAncestor, err := customCommit.IsAncestor(v1Commit)
	if err != nil {
		logging.Warn(ctx, "v1.1 read sync skipped: ancestry check failed",
			slog.String("error", err.Error()))
		return
	}
	if !isAncestor {
		// Diverged from v1: leave the custom ref untouched rather than clobber
		// local-only history. Reads still use the v1.1 ref as-is.
		logging.Warn(ctx, "v1.1 custom ref diverged from v1; reading custom ref as-is",
			slog.String("ref", paths.MetadataRefName),
			slog.String("custom_hash", customRef.Hash().String()),
			slog.String("v1_hash", v1Hash.String()))
		return
	}

	setCustomRef(ctx, repo, customRefName, v1Hash)
}

// resolveV1Tip returns the v1 metadata tip, preferring the local v1 branch and
// falling back to the origin remote-tracking branch (so v1.1 can be seeded on a
// fresh clone that has not yet written locally).
func resolveV1Tip(repo *git.Repository) (plumbing.Hash, bool) {
	if ref, err := repo.Reference(plumbing.NewBranchReferenceName(paths.MetadataBranchName), true); err == nil {
		return ref.Hash(), true
	}
	if ref, err := repo.Reference(plumbing.NewRemoteReferenceName("origin", paths.MetadataBranchName), true); err == nil {
		return ref.Hash(), true
	}
	return plumbing.ZeroHash, false
}

// setCustomRef points refName at hash, logging success at debug and failure at
// warning. Failures are swallowed so the read can proceed against the ref as-is.
func setCustomRef(ctx context.Context, repo *git.Repository, refName plumbing.ReferenceName, hash plumbing.Hash) {
	if err := repo.Storer.SetReference(plumbing.NewHashReference(refName, hash)); err != nil {
		logging.Warn(ctx, "v1.1 read sync failed to advance custom ref",
			slog.String("ref", refName.String()),
			slog.String("error", err.Error()))
		return
	}
	logging.Debug(ctx, "v1.1 custom ref synced for read",
		slog.String("ref", refName.String()),
		slog.String("hash", hash.String()))
}
