package cli

import (
	"context"
	"errors"
	"fmt"

	"github.com/entireio/cli/cmd/entire/cli/checkpointpolicy"
	"github.com/entireio/cli/cmd/entire/cli/gitrepo"
	"github.com/spf13/cobra"
)

type checkpointPolicyOptions struct {
	version         string
	minVersion      string
	unsetVersion    bool
	unsetMinVersion bool
	force           bool
}

func newCheckpointPolicyCmd() *cobra.Command {
	var opts checkpointPolicyOptions
	cmd := &cobra.Command{
		Use:   "policy",
		Short: "Inspect and update checkpoint policy",
		Long: `Inspect and update checkpoint policy.

checkpoint_version selects the checkpoint metadata format used for new writes.
If no policy is configured, Entire uses the CLI default. If this CLI reads a
configured checkpoint_version it cannot write, it warns and writes the default
version instead. Unset checkpoint_version to inherit the CLI default.

checkpoint_min_version is an upgrade nudge. Clients that cannot read that
version warn users to upgrade, but policy alone does not block checkpoint writes
or app usage. Unset checkpoint_min_version to inherit the CLI default.`,
		Hidden: true,
		Args:   cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCheckpointPolicy(cmd, opts)
		},
	}

	cmd.Flags().StringVar(&opts.version, "checkpoint-version", "", "Set the checkpoint version used for new writes")
	cmd.Flags().StringVar(&opts.minVersion, "checkpoint-min-version", "", "Set the checkpoint version used for upgrade warnings")
	cmd.Flags().BoolVar(&opts.unsetVersion, "unset-checkpoint-version", false, "Unset checkpoint_version so new writes use the CLI default")
	cmd.Flags().BoolVar(&opts.unsetMinVersion, "unset-checkpoint-min-version", false, "Unset checkpoint_min_version so upgrade warnings use the CLI default")
	cmd.Flags().BoolVar(&opts.force, "force", false, "Allow checkpoint policy version downgrades")
	return cmd
}

func runCheckpointPolicy(cmd *cobra.Command, opts checkpointPolicyOptions) error {
	ctx := cmd.Context()
	if err := ctx.Err(); err != nil {
		return NewSilentError(err)
	}
	repo, err := gitrepo.OpenCurrent(ctx)
	if err != nil {
		return checkpointPolicyError("open repository", err)
	}
	defer repo.Close()

	target, err := checkpointpolicy.ResolveTarget(ctx)
	if err != nil {
		return checkpointPolicyError("resolve checkpoint policy remote", err)
	}

	var state checkpointpolicy.State
	if hasCheckpointPolicyUpdate(opts) {
		state, err = checkpointpolicy.Update(ctx, repo, target, checkpointpolicy.UpdateOptions{
			CheckpointVersion:         opts.version,
			CheckpointMinVersion:      opts.minVersion,
			UnsetCheckpointVersion:    opts.unsetVersion,
			UnsetCheckpointMinVersion: opts.unsetMinVersion,
			Force:                     opts.force,
		})
		if err != nil {
			return checkpointPolicyError("update checkpoint policy", err)
		}
		if err := checkpointpolicy.Push(ctx, target); err != nil {
			return checkpointPolicyError("push checkpoint policy", err)
		}
		state.Source = checkpointpolicy.SourceRemote
	} else {
		state, err = checkpointpolicy.Sync(ctx, repo, target)
		if err != nil {
			return checkpointPolicyError("sync checkpoint policy", err)
		}
	}

	effectivePolicy := checkpointpolicy.Normalize(state.Policy)
	fmt.Fprintf(cmd.OutOrStdout(), "checkpoint_version: %s\n", formatCheckpointPolicyValue(state.Policy.CheckpointVersion, effectivePolicy.CheckpointVersion))
	fmt.Fprintf(cmd.OutOrStdout(), "checkpoint_min_version: %s\n", formatCheckpointPolicyValue(state.Policy.CheckpointMinVersion, effectivePolicy.CheckpointMinVersion))
	fmt.Fprintf(cmd.OutOrStdout(), "source: %s\n", state.Source)
	return nil
}

func hasCheckpointPolicyUpdate(opts checkpointPolicyOptions) bool {
	return opts.version != "" || opts.minVersion != "" || opts.unsetVersion || opts.unsetMinVersion
}

func formatCheckpointPolicyValue(configured, effective string) string {
	if configured == "" {
		return effective + " (default)"
	}
	return configured
}

func checkpointPolicyError(message string, err error) error {
	wrapped := fmt.Errorf("%s: %w", message, err)
	if errors.Is(wrapped, context.Canceled) {
		return NewSilentError(wrapped)
	}
	return wrapped
}
