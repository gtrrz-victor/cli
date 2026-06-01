package strategy

import (
	"context"
	"log/slog"

	git "github.com/go-git/go-git/v6"

	"github.com/entireio/cli/cmd/entire/cli/checkpoint"
	"github.com/entireio/cli/cmd/entire/cli/logging"
)

// mirrorMetadataToV1CustomRef advances the committed-ref topology's mirror
// (today refs/entire/checkpoints/v1.1) to the primary's current commit (today
// the v1 metadata branch) when a mirror is configured (checkpoints_version
// "1.1").
//
// The primary stays the source of truth; the mirror is a local-only ref sharing
// the primary's exact commit — it is never pushed. Call only after a successful
// primary committed write; a mirror failure must not affect that write, so
// problems are logged, not returned.
func mirrorMetadataToV1CustomRef(ctx context.Context, repo *git.Repository) {
	refs := checkpoint.ResolveCommittedRefs(ctx)
	if !refs.HasMirror() {
		return
	}

	primaryRef, err := repo.Reference(refs.Primary, true)
	if err != nil {
		// No primary metadata ref yet — nothing to mirror. Expected on first use.
		logging.Debug(ctx, "committed-ref mirror skipped: primary metadata ref unavailable",
			slog.String("error", err.Error()))
		return
	}

	if err := SafelyAdvanceLocalRef(ctx, repo, refs.Mirror, primaryRef.Hash()); err != nil {
		logging.Warn(ctx, "committed-ref mirror failed",
			slog.String("ref", refs.Mirror.String()),
			slog.String("error", err.Error()))
		return
	}

	logging.Debug(ctx, "committed-ref mirror updated",
		slog.String("ref", refs.Mirror.String()),
		slog.String("hash", primaryRef.Hash().String()))
}
