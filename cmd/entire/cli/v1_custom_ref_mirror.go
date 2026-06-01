package cli

import (
	"errors"
	"fmt"

	git "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"

	"github.com/entireio/cli/cmd/entire/cli/checkpoint"
)

// mirrorToV1CustomRef sets the committed-ref topology's mirror (today
// refs/entire/checkpoints/v1.1) to the primary's tip (today the v1 metadata
// branch), returning errors so callers can surface them. The primary is the
// source of truth; the mirror is a strict local mirror, so this force-overwrites
// rather than safely advancing. Callers gate on refs.HasMirror(). The hook-side
// equivalent (strategy.mirrorMetadataToV1CustomRef) logs errors instead of
// returning them.
func mirrorToV1CustomRef(refs checkpoint.CommittedRefs, repo *git.Repository) error {
	primaryRef, err := repo.Reference(refs.Primary, true)
	if err != nil {
		if errors.Is(err, plumbing.ErrReferenceNotFound) {
			return fmt.Errorf("primary metadata ref %s missing after committed write", refs.Primary)
		}
		return fmt.Errorf("read primary metadata ref %s: %w", refs.Primary, err)
	}
	mirror := plumbing.NewHashReference(refs.Mirror, primaryRef.Hash())
	if err := repo.Storer.SetReference(mirror); err != nil {
		return fmt.Errorf("set ref %s to %s: %w", refs.Mirror, primaryRef.Hash(), err)
	}
	return nil
}
