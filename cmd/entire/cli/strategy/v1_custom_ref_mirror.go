package strategy

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"

	"github.com/entireio/cli/cmd/entire/cli/checkpoint"
	"github.com/entireio/cli/cmd/entire/cli/logging"
)

// ErrPrimaryMetadataMissing is returned by MirrorCommittedMetadataRef when the
// primary ref does not exist yet. Callers match this sentinel to distinguish
// "expected on first use" from a real read failure, and from a SetReference
// NotFound on the mirror itself.
var ErrPrimaryMetadataMissing = errors.New("primary metadata ref missing")

// AdvanceCommittedPrimary points refs.Primary at hash and best-effort-advances
// refs.Mirror. Used by autonomous flows (hooks, push rebase, condensation,
// reconcile) where mirror failure must not abort the primary operation.
// User-foreground commands (attach, explain summary) call
// MirrorCommittedMetadataRef directly when they want to surface mirror errors.
func AdvanceCommittedPrimary(ctx context.Context, repo *git.Repository, refs checkpoint.CommittedRefs, hash plumbing.Hash) error {
	if err := repo.Storer.SetReference(plumbing.NewHashReference(refs.Primary, hash)); err != nil {
		return fmt.Errorf("set primary metadata ref %s to %s: %w", refs.Primary, hash, err)
	}
	mirrorCommittedMetadataRefBestEffort(ctx, repo, refs)
	return nil
}

// MirrorCommittedMetadataRef points refs.Mirror at refs.Primary's tip. No-op
// when the topology has no mirror.
func MirrorCommittedMetadataRef(ctx context.Context, repo *git.Repository, refs checkpoint.CommittedRefs) error {
	if !refs.HasMirror() {
		return nil
	}

	primaryRef, err := repo.Reference(refs.Primary, true)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return fmt.Errorf("primary metadata ref %s missing: %w", refs.Primary, ErrPrimaryMetadataMissing)
		}
		return fmt.Errorf("read primary metadata ref %s: %w", refs.Primary, err)
	}

	if err := repo.Storer.SetReference(plumbing.NewHashReference(refs.Mirror, primaryRef.Hash())); err != nil {
		return fmt.Errorf("set mirror ref %s to %s: %w", refs.Mirror, primaryRef.Hash(), err)
	}

	logging.Debug(ctx, "committed-ref mirror updated",
		slog.String("ref", refs.Mirror.String()),
		slog.String("hash", primaryRef.Hash().String()))
	return nil
}

// mirrorCommittedMetadataRefBestEffort runs MirrorCommittedMetadataRef under a
// detached cancellation context, swallowing errors as logs. The detached
// context preserves trace/value context but prevents a near-expired parent
// deadline (e.g. the 2-minute fetch budget) from silently failing
// settings.Load and skipping the mirror with no log. The mirror itself is
// short.
func mirrorCommittedMetadataRefBestEffort(ctx context.Context, repo *git.Repository, refs checkpoint.CommittedRefs) {
	ctx = context.WithoutCancel(ctx)
	if !refs.HasMirror() {
		return
	}
	if err := MirrorCommittedMetadataRef(ctx, repo, refs); err != nil {
		if errors.Is(err, ErrPrimaryMetadataMissing) {
			logging.Debug(ctx, "committed-ref mirror skipped: primary metadata ref unavailable",
				slog.String("error", err.Error()))
			return
		}
		logging.Warn(ctx, "committed-ref mirror failed",
			slog.String("ref", refs.Mirror.String()),
			slog.String("error", err.Error()))
	}
}
