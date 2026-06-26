package cli

import (
	"context"

	"github.com/entireio/cli/cmd/entire/cli/checkpointpolicy"
	"github.com/go-git/go-git/v6"
)

func committedCheckpointVersion(ctx context.Context, repo *git.Repository) string {
	state, err := checkpointpolicy.ReadLocal(ctx, repo)
	if err != nil {
		return checkpointpolicy.DefaultCheckpointVersion()
	}
	return checkpointpolicy.CheckpointVersion(state.Policy)
}
