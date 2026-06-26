package checkpointpolicy

import (
	"context"
	"errors"
	"fmt"

	"github.com/go-git/go-git/v6"
)

type UpdateOptions struct {
	CheckpointVersion         string
	CheckpointMinVersion      string
	UnsetCheckpointVersion    bool
	UnsetCheckpointMinVersion bool
	Force                     bool
}

func Update(ctx context.Context, repo *git.Repository, target Target, opts UpdateOptions) (State, error) {
	if err := validateUpdateOptions(opts); err != nil {
		return State{}, err
	}

	baseline, err := updateBaseline(ctx, repo, target)
	if err != nil {
		return State{}, err
	}

	policy := baseline.Policy
	if opts.UnsetCheckpointVersion {
		policy.CheckpointVersion = ""
	} else if opts.CheckpointVersion != "" {
		policy.CheckpointVersion = opts.CheckpointVersion
	}
	if opts.UnsetCheckpointMinVersion {
		policy.CheckpointMinVersion = ""
	} else if opts.CheckpointMinVersion != "" {
		policy.CheckpointMinVersion = opts.CheckpointMinVersion
	}

	if err := rejectDowngrades(baseline.Policy, policy, opts); err != nil {
		return State{}, err
	}
	if err := ValidatePolicy(policy); err != nil {
		return State{}, err
	}

	hash, err := WriteLocal(ctx, repo, baseline.Hash, policy)
	if err != nil {
		return State{}, err
	}
	return State{
		Policy:     policy,
		Source:     SourceLocal,
		Hash:       hash,
		RemoteHash: baseline.RemoteHash,
	}, nil
}

func validateUpdateOptions(opts UpdateOptions) error {
	if opts.CheckpointVersion != "" && opts.UnsetCheckpointVersion {
		return errors.New("checkpoint_version cannot be both set and unset")
	}
	if opts.CheckpointMinVersion != "" && opts.UnsetCheckpointMinVersion {
		return errors.New("checkpoint_min_version cannot be both set and unset")
	}
	return nil
}

func updateBaseline(ctx context.Context, repo *git.Repository, target Target) (State, error) {
	local, err := ReadLocal(ctx, repo)
	if err != nil {
		return State{}, err
	}

	baseline, remoteFound, err := remoteBaseline(ctx, repo, target, local)
	if err != nil {
		return State{}, err
	}
	if !remoteFound || local.Hash == baseline.Hash {
		return baseline, nil
	}
	if local.Hash.IsZero() {
		return baseline, nil
	}
	localAncestor, err := isAncestorOf(ctx, repo, local.Hash, baseline.Hash)
	if err != nil {
		return State{}, err
	}
	if localAncestor {
		return baseline, nil
	}
	baselineAncestor, err := isAncestorOf(ctx, repo, baseline.Hash, local.Hash)
	if err != nil {
		return State{}, err
	}
	if baselineAncestor {
		local.RemoteHash = baseline.RemoteHash
		return local, nil
	}
	return State{}, fmt.Errorf("local checkpoint policy %s diverges from remote %s; push or reconcile the policy before updating", local.Hash, baseline.RemoteHash)
}

func rejectDowngrades(before, after Policy, opts UpdateOptions) error {
	before = Normalize(before)
	after = Normalize(after)

	if opts.Force {
		return nil
	}
	if opts.CheckpointVersion != "" || opts.UnsetCheckpointVersion {
		if err := rejectFieldDowngrade("checkpoint_version", before.CheckpointVersion, after.CheckpointVersion); err != nil {
			return err
		}
	}
	if opts.CheckpointMinVersion != "" || opts.UnsetCheckpointMinVersion {
		if err := rejectFieldDowngrade("checkpoint_min_version", before.CheckpointMinVersion, after.CheckpointMinVersion); err != nil {
			return err
		}
	}
	return nil
}

func rejectFieldDowngrade(field, beforeRaw, afterRaw string) error {
	before, err := ParseFormat(beforeRaw)
	if err != nil {
		return fmt.Errorf("%s existing value %q: %w", field, beforeRaw, err)
	}
	after, err := ParseFormat(afterRaw)
	if err != nil {
		return fmt.Errorf("%s: %w", field, err)
	}
	if Compare(after, before) < 0 {
		return fmt.Errorf("would downgrade %s from %q to %q; pass --force to allow this", field, beforeRaw, afterRaw)
	}
	return nil
}
