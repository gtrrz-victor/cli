package auth

import (
	"testing"

	"github.com/entireio/cli/cmd/entire/cli/api"
)

func TestResolveProvider_DefaultsToV1(t *testing.T) {
	t.Parallel()

	cases := []string{"", "v1", "  ", "unknown", "v3"}
	for _, in := range cases {
		got := resolveProvider(in)
		if got.DeviceCodePath != "/oauth/device/code" {
			t.Errorf("resolveProvider(%q).DeviceCodePath = %q, want v1's", in, got.DeviceCodePath)
		}
		if got.STSPath != "" {
			t.Errorf("resolveProvider(%q).STSPath = %q, want empty for v1", in, got.STSPath)
		}
	}
}

func TestResolveProvider_V2(t *testing.T) {
	t.Parallel()

	got := resolveProvider("v2")
	if got.DeviceCodePath != "/device_authorization" {
		t.Errorf("v2 DeviceCodePath = %q", got.DeviceCodePath)
	}
	if got.TokenPath != "/oauth/token" {
		t.Errorf("v2 TokenPath = %q", got.TokenPath)
	}
	// Token poll and RFC 8693 exchange share the same OIDC endpoint —
	// grant_type differentiates them on the wire.
	if got.STSPath != "/oauth/token" {
		t.Errorf("v2 STSPath = %q", got.STSPath)
	}
	if got.AuthTokensPath != "/api/v1/auth/tokens" {
		t.Errorf("v2 AuthTokensPath = %q", got.AuthTokensPath)
	}
}

// --- effectiveProviderVersion ----------------------------------------------
//
// These tests use t.Setenv and therefore cannot be t.Parallel — Go's
// testing harness panics if either is paired with the other. The
// trade-off is fine because effectiveProviderVersion is a tiny pure
// function over env state; the bodies are short.

// TestEffectiveProviderVersion_ExplicitEnvWins pins the highest-
// priority rule: ENTIRE_AUTH_PROVIDER_VERSION overrides everything
// else, including a split-host configuration that would otherwise
// auto-pick v2.
func TestEffectiveProviderVersion_ExplicitEnvWins(t *testing.T) {
	// Split-host config — would auto-pick v2 if env var were unset.
	t.Setenv(api.BaseURLEnvVar, "https://api.example.com")
	t.Setenv(api.AuthBaseURLEnvVar, "https://auth.example.com")

	t.Setenv(ProviderVersionEnvVar, "v1")
	if got := effectiveProviderVersion(); got != "v1" {
		t.Errorf("explicit v1 override = %q, want v1", got)
	}

	t.Setenv(ProviderVersionEnvVar, "v2")
	if got := effectiveProviderVersion(); got != "v2" {
		t.Errorf("explicit v2 override = %q, want v2", got)
	}

	// Unrecognised value falls through to resolveProvider's default
	// (v1) — the version string is returned verbatim here, and the
	// routing table catches unknowns. We don't want an unrecognised
	// value to suddenly invoke the auto-detect path, because that
	// would hide typos behind a silent upgrade.
	t.Setenv(ProviderVersionEnvVar, "v3")
	if got := effectiveProviderVersion(); got != "v3" {
		t.Errorf("unrecognised value = %q, want passthrough %q", got, "v3")
	}
}

// TestEffectiveProviderVersion_SplitHostPicksV2 is the actual user-
// facing change: setting ENTIRE_AUTH_BASE_URL to an origin different
// from the data API origin opts into v2 without an extra env var.
func TestEffectiveProviderVersion_SplitHostPicksV2(t *testing.T) {
	t.Setenv(ProviderVersionEnvVar, "")
	t.Setenv(api.BaseURLEnvVar, "https://api.example.com")
	t.Setenv(api.AuthBaseURLEnvVar, "https://auth.example.com")

	if got := effectiveProviderVersion(); got != "v2" {
		t.Errorf("split-host auto-detect = %q, want v2", got)
	}
}

// TestEffectiveProviderVersion_SameHostPicksV1 covers a user who set
// ENTIRE_AUTH_BASE_URL redundantly to the same value as the data API
// — that's not actually a split-host configuration, so the auto-
// detect should stay on v1 rather than picking v2 against a single-
// host server that may not serve OIDC endpoints. The canonicalisation
// inside AuthBaseURL/BaseURL handles cosmetic differences (trailing
// slash, case) so this rule isn't fooled by formatting.
func TestEffectiveProviderVersion_SameHostPicksV1(t *testing.T) {
	t.Setenv(ProviderVersionEnvVar, "")
	t.Setenv(api.BaseURLEnvVar, "https://api.example.com")
	t.Setenv(api.AuthBaseURLEnvVar, "https://api.example.com/")

	if got := effectiveProviderVersion(); got != "v1" {
		t.Errorf("same-host auto-detect = %q, want v1 (no split-host signal)", got)
	}
}

// TestEffectiveProviderVersion_UnsetDefaultsToV1 covers the entire.io
// default: nothing set, AuthBaseURL falls back to BaseURL, no split
// signal, stay on v1. This is the path most existing users hit and
// the one that must not silently break.
func TestEffectiveProviderVersion_UnsetDefaultsToV1(t *testing.T) {
	t.Setenv(ProviderVersionEnvVar, "")
	t.Setenv(api.BaseURLEnvVar, "")
	t.Setenv(api.AuthBaseURLEnvVar, "")

	if got := effectiveProviderVersion(); got != "v1" {
		t.Errorf("unset env = %q, want v1 (entire.io default)", got)
	}
}

func TestSetProviderForTest_Overrides(t *testing.T) {
	t.Parallel()

	custom := Provider{
		ClientID:       "test-cli",
		DeviceCodePath: "/custom/device",
		TokenPath:      "/custom/token",
		STSPath:        "/custom/sts",
		AuthTokensPath: "/custom/tokens",
	}
	SetProviderForTest(t, custom)

	got := CurrentProvider()
	if got.DeviceCodePath != "/custom/device" {
		t.Errorf("DeviceCodePath = %q, want override", got.DeviceCodePath)
	}
	if got.STSPath != "/custom/sts" {
		t.Errorf("STSPath = %q, want override", got.STSPath)
	}
}
