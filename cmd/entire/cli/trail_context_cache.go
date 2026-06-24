package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/entireio/cli/cmd/entire/cli/api"
	"github.com/entireio/cli/cmd/entire/cli/auth"
	"github.com/entireio/cli/cmd/entire/cli/gitremote"
	"github.com/entireio/cli/cmd/entire/cli/logging"
	"github.com/entireio/cli/cmd/entire/cli/settings"
)

const (
	trailEnablementCacheTTL       = time.Hour
	trailEnablementRefreshTimeout = 3 * time.Second
)

type trailEnablementCacheStatus int

const (
	trailEnablementCacheUnknown trailEnablementCacheStatus = iota
	trailEnablementCacheEnabled
	trailEnablementCacheDisabled
)

type trailEnablementScope struct {
	Forge     string
	Owner     string
	Repo      string
	RepoKey   string
	APIBase   string
	AuthKey   string
	Supported bool
}

// trailsEnabledForRepo reads the local cache only.
func trailsEnabledForRepo(ctx context.Context) bool {
	return cachedTrailsEnablementForRepo(ctx, time.Now()) == trailEnablementCacheEnabled
}

func cachedTrailsEnablementForRepo(ctx context.Context, now time.Time) trailEnablementCacheStatus {
	prefs, err := settings.LoadClonePreferences(ctx)
	if err != nil || prefs.TrailsEnabled == nil || prefs.TrailsEnabledCheckedAt == nil {
		return trailEnablementCacheUnknown
	}

	scope, err := currentTrailEnablementScope(ctx)
	if err != nil || !trailEnablementCacheMatchesScope(prefs, scope) || trailEnablementCacheExpired(*prefs.TrailsEnabledCheckedAt, now) {
		return trailEnablementCacheUnknown
	}

	if *prefs.TrailsEnabled {
		return trailEnablementCacheEnabled
	}
	return trailEnablementCacheDisabled
}

func trailEnablementCacheMatchesScope(prefs *settings.ClonePreferences, scope trailEnablementScope) bool {
	return prefs.TrailsEnabledRepoKey == scope.RepoKey &&
		prefs.TrailsEnabledAPIBase == scope.APIBase &&
		prefs.TrailsEnabledAuthKey == scope.AuthKey
}

func trailEnablementCacheExpired(checkedAt time.Time, now time.Time) bool {
	if checkedAt.IsZero() {
		return true
	}
	if now.Before(checkedAt) {
		return true
	}
	return now.Sub(checkedAt) > trailEnablementCacheTTL
}

func currentTrailEnablementScope(ctx context.Context) (trailEnablementScope, error) {
	rawURL, err := gitremote.GetRemoteURL(ctx, "origin")
	if err != nil {
		return trailEnablementScope{}, fmt.Errorf("get origin remote: %w", err)
	}
	if strings.TrimSpace(rawURL) == "" {
		return trailEnablementScope{}, errors.New("get origin remote: empty URL")
	}
	info, err := gitremote.ParseURL(rawURL)
	if err != nil {
		return trailEnablementScope{}, fmt.Errorf("parse origin remote: %w", err)
	}
	authKey, err := auth.LocalIdentityCacheKey()
	if err != nil {
		return trailEnablementScope{}, fmt.Errorf("resolve auth cache key: %w", err)
	}
	return trailEnablementScope{
		Forge:     info.Forge,
		Owner:     info.Owner,
		Repo:      info.Repo,
		RepoKey:   trailEnablementRepoKey(info.Forge, info.Owner, info.Repo),
		APIBase:   api.BaseURL(),
		AuthKey:   authKey,
		Supported: info.Forge != "",
	}, nil
}

func trailEnablementRepoKey(forge, owner, repo string) string {
	return strings.Join([]string{forge, owner, repo}, "/")
}

func saveTrailsEnabledForRepo(ctx context.Context, enabled bool) error {
	scope, err := currentTrailEnablementScope(ctx)
	if err != nil {
		return err
	}
	return saveTrailsEnabledForScope(ctx, scope, enabled, time.Now())
}

func saveTrailsEnabledForRemote(ctx context.Context, forge, owner, repo string, enabled bool) error {
	authKey, err := auth.LocalIdentityCacheKey()
	if err != nil {
		return fmt.Errorf("resolve auth cache key: %w", err)
	}
	scope := trailEnablementScope{
		Forge:     forge,
		Owner:     owner,
		Repo:      repo,
		RepoKey:   trailEnablementRepoKey(forge, owner, repo),
		APIBase:   api.BaseURL(),
		AuthKey:   authKey,
		Supported: forge != "",
	}
	return saveTrailsEnabledForScope(ctx, scope, enabled, time.Now())
}

func saveTrailsEnabledForScope(ctx context.Context, scope trailEnablementScope, enabled bool, checkedAt time.Time) error {
	prefs, err := settings.LoadClonePreferences(ctx)
	if err != nil {
		return fmt.Errorf("load clone preferences: %w", err)
	}
	enabledCopy := enabled
	checkedAtUTC := checkedAt.UTC()
	prefs.TrailsEnabled = &enabledCopy
	prefs.TrailsEnabledCheckedAt = &checkedAtUTC
	prefs.TrailsEnabledRepoKey = scope.RepoKey
	prefs.TrailsEnabledAPIBase = scope.APIBase
	prefs.TrailsEnabledAuthKey = scope.AuthKey
	if err := settings.SaveClonePreferences(ctx, prefs); err != nil {
		return fmt.Errorf("save clone preferences: %w", err)
	}
	return nil
}

func refreshTrailsEnabledCacheIfStale(ctx context.Context) error {
	if cachedTrailsEnablementForRepo(ctx, time.Now()) != trailEnablementCacheUnknown {
		return nil
	}
	scope, err := currentTrailEnablementScope(ctx)
	if err != nil {
		return err
	}
	if !scope.Supported {
		return saveTrailsEnabledForScope(ctx, scope, false, time.Now())
	}
	client, err := NewAuthenticatedAPIClient(ctx, false)
	if err != nil {
		return err
	}
	_, err = refreshTrailsEnabledCacheForScope(ctx, client, scope)
	return err
}

func refreshTrailsEnabledCache(ctx context.Context, client *api.Client) (bool, error) {
	scope, err := currentTrailEnablementScope(ctx)
	if err != nil {
		return false, err
	}
	return refreshTrailsEnabledCacheForScope(ctx, client, scope)
}

func refreshTrailsEnabledCacheForScope(ctx context.Context, client *api.Client, scope trailEnablementScope) (bool, error) {
	if !scope.Supported {
		if err := saveTrailsEnabledForScope(ctx, scope, false, time.Now()); err != nil {
			return false, err
		}
		return false, nil
	}
	enabled, err := client.TrailsEnabled(ctx, scope.Forge, scope.Owner, scope.Repo)
	if err != nil {
		return false, fmt.Errorf("check trails enablement: %w", err)
	}
	if err := saveTrailsEnabledForScope(ctx, scope, enabled, time.Now()); err != nil {
		return false, err
	}
	return enabled, nil
}

func saveTrailsEnabledForRepoBestEffort(ctx context.Context, enabled bool) {
	if err := saveTrailsEnabledForRepo(ctx, enabled); err != nil {
		logging.Debug(ctx, "failed to cache trails enablement", "error", err)
	}
}

func saveTrailsEnabledForRemoteBestEffort(ctx context.Context, forge, owner, repo string, enabled bool) {
	if err := saveTrailsEnabledForRemote(ctx, forge, owner, repo, enabled); err != nil {
		logging.Debug(ctx, "failed to cache trails enablement", "error", err)
	}
}

func refreshTrailsEnabledCacheBestEffort(ctx context.Context, client *api.Client) {
	refreshCtx, cancel := context.WithTimeout(ctx, trailEnablementRefreshTimeout)
	defer cancel()
	if _, err := refreshTrailsEnabledCache(refreshCtx, client); err != nil {
		logging.Debug(ctx, "trails enablement refresh skipped", "error", err)
	}
}

func noteTrailCommandEnablement(ctx context.Context, client *api.Client, commandErr error) {
	if commandErr == nil {
		saveTrailsEnabledForRepoBestEffort(ctx, true)
		return
	}
	refreshTrailsEnabledCacheBestEffort(ctx, client)
}

func runAuthenticatedTrailAPI(ctx context.Context, errW io.Writer, insecureHTTP bool, fn func(context.Context, *api.Client) error) error {
	return runAuthenticatedDataAPI(ctx, errW, insecureHTTP, func(ctx context.Context, client *api.Client) error {
		err := fn(ctx, client)
		noteTrailCommandEnablement(ctx, client, err)
		return err
	})
}
