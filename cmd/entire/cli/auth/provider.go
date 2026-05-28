package auth

import (
	"os"
	"strings"
	"sync"

	"github.com/entireio/cli/cmd/entire/cli/api"
)

// ProviderVersionEnvVar explicitly selects which OAuth surface this CLI
// talks to.
//
// Recognised values:
//
//   - "v1"  — legacy single-host device-flow surface
//   - "v2"  — OIDC-standard surface (with RFC 8693 STS for split-host)
//
// Unset (or unrecognised): the version is inferred from the deployment
// shape — see effectiveProviderVersion. A user-set ENTIRE_AUTH_BASE_URL
// that differs from the data API origin signals split-host, which only
// the v2 surface can talk to, so v2 is picked automatically. The env
// var is only needed to override that inference (e.g. force v2 against
// a v2-capable single-host server, or force v1 to suppress the
// auto-upgrade).
//
// Read once at process startup via CurrentProvider; later flips within
// the same process are intentionally ignored. Tests inject via
// SetProviderForTest rather than mutating the env mid-run.
const ProviderVersionEnvVar = "ENTIRE_AUTH_PROVIDER_VERSION"

// Provider captures the per-surface bits of OAuth wiring.
//
// STSPath is the RFC 8693 token-exchange endpoint. v1 is the legacy
// single-host surface where the auth and data API live at the same
// origin; the same-host shortcut in tokenmanager.Token always wins and
// STS is never invoked, so v1.STSPath is left empty. v2 exposes a
// dedicated STS path because it's used in split-host deployments
// (e.g. us.auth.partial.to mints, partial.to consumes).
//
// AuthTokensPath is the base path for the auth-tokens management
// endpoint family (list / revoke). Routed at the api.Client layer via
// (*api.Client).WithAuthTokensPath so the provider table is the single
// source of truth — no env-var duplication between auth/ and api/.
type Provider struct {
	ClientID       string
	DeviceCodePath string
	TokenPath      string
	STSPath        string
	AuthTokensPath string
}

var providers = map[string]Provider{
	"v1": { //nolint:gosec // OAuth client_id and endpoint paths, not credentials
		ClientID:       "entire-cli",
		DeviceCodePath: "/oauth/device/code",
		TokenPath:      "/oauth/token",
		AuthTokensPath: "/api/v1/auth/tokens",
	},
	"v2": { //nolint:gosec // OAuth client_id and endpoint paths, not credentials
		// Matches an OIDC-standard auth server's discovery doc — confirmed
		// against us.auth.partial.to's /.well-known/openid-configuration.
		// Device authorization, token poll, and RFC 8693 exchange all hit
		// the standard endpoints; grant_type differentiates token vs
		// exchange at the shared /oauth/token endpoint.
		ClientID:       "entire-cli",
		DeviceCodePath: "/device_authorization",
		TokenPath:      "/oauth/token",
		STSPath:        "/oauth/token",
		// API token management lives on the data API (not the auth host).
		// auth.go / logout.go pass api.AuthBaseURL() for the keyring key,
		// but the AuthTokensPath calls should route to api.BaseURL() in
		// split-host setups — see TODO in auth.go's newAuthHostAPIClient.
		AuthTokensPath: "/api/v1/auth/tokens",
	},
}

// resolveProvider returns the Provider matching version. Defaulting
// (rather than erroring) on unrecognised values keeps old binaries safe
// if a future v3 ever lands. Pure function — no env reads — so unit
// tests can exercise the routing table without env-var gymnastics.
func resolveProvider(version string) Provider {
	switch strings.TrimSpace(version) {
	case "v2":
		return providers["v2"]
	default:
		return providers["v1"]
	}
}

// effectiveProviderVersion picks the version string fed into
// resolveProvider for this process. Resolution order:
//
//  1. ENTIRE_AUTH_PROVIDER_VERSION explicit value wins. "v1"/"v2" route
//     directly; anything unrecognised falls through resolveProvider's
//     default (v1), so a typo doesn't silently auto-upgrade.
//  2. Split-host detected (AuthBaseURL != BaseURL) → "v2". An explicit
//     ENTIRE_AUTH_BASE_URL pointing at a different origin than the data
//     API is the operator's signal that the auth surface lives
//     elsewhere; only v2's OIDC-standard paths know how to talk to a
//     dedicated auth host. The data host's defaults (entire.io as of
//     2026-05) don't serve the v2 surface, so picking v2 when no split
//     is configured would break login.
//  3. Otherwise → "v1". Single-host deployments stay on the surface
//     their backend currently exposes; users on a v2-capable single-
//     host server can opt in via the env var.
//
// Mixing env var + split-host: the env var always wins, so a user who
// wants v1 even with a split-host config (e.g. mid-migration testing)
// can set ENTIRE_AUTH_PROVIDER_VERSION=v1 explicitly.
func effectiveProviderVersion() string {
	if v := strings.TrimSpace(os.Getenv(ProviderVersionEnvVar)); v != "" {
		return v
	}
	if api.AuthBaseURL() != api.BaseURL() {
		return "v2"
	}
	return "v1"
}

var (
	providerOnce     sync.Once
	resolvedProvider Provider

	// providerForTest, when non-nil, short-circuits CurrentProvider so
	// tests can install a specific Provider without racing the
	// process-wide sync.Once (which freezes the first observation
	// forever). Mutated only via SetProviderForTest. Production code
	// never reads this var.
	providerForTest *Provider
	providerTestMu  sync.Mutex
)

// CurrentProvider returns the active Provider for this process.
//
// Resolution: call effectiveProviderVersion exactly once on first
// invocation, freeze the result, and return the same Provider on every
// subsequent call. The function reads ENTIRE_AUTH_PROVIDER_VERSION,
// ENTIRE_AUTH_BASE_URL, and ENTIRE_API_BASE_URL — all three must be
// set before the first CurrentProvider call. Tests that need a
// different provider use SetProviderForTest, which bypasses the
// singleton entirely.
func CurrentProvider() Provider {
	providerTestMu.Lock()
	override := providerForTest
	providerTestMu.Unlock()
	if override != nil {
		return *override
	}
	providerOnce.Do(func() {
		resolvedProvider = resolveProvider(effectiveProviderVersion())
	})
	return resolvedProvider
}

// SetProviderForTest installs p as the Provider returned by
// CurrentProvider for the duration of the test, and registers a
// t.Cleanup to remove the override. Test-only.
//
// Takes a tiny interface rather than *testing.T so production builds
// don't import testing.
func SetProviderForTest(t interface {
	Helper()
	Cleanup(f func())
}, p Provider) {
	t.Helper()
	providerTestMu.Lock()
	prev := providerForTest
	providerForTest = &p
	providerTestMu.Unlock()
	t.Cleanup(func() {
		providerTestMu.Lock()
		providerForTest = prev
		providerTestMu.Unlock()
	})
}
