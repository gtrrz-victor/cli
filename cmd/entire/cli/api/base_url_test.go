package api

import (
	"errors"
	"os"
	"strings"
	"testing"
)

func TestBaseURL_Default(t *testing.T) {
	t.Setenv(BaseURLEnvVar, "")

	if got := BaseURL(); got != DefaultBaseURL {
		t.Fatalf("BaseURL() = %q, want %q", got, DefaultBaseURL)
	}
}

func TestBaseURL_Override(t *testing.T) {
	t.Setenv(BaseURLEnvVar, " http://localhost:8787/ ")

	if got := BaseURL(); got != "http://localhost:8787" {
		t.Fatalf("BaseURL() = %q, want %q", got, "http://localhost:8787")
	}
}

func TestResolveURLFromBase_RejectsNonHTTPScheme(t *testing.T) {
	t.Parallel()

	for _, scheme := range []string{"ftp://example.com", "file:///etc/passwd", "ssh://host"} {
		_, err := ResolveURLFromBase(scheme, "/path")
		if err == nil {
			t.Errorf("ResolveURLFromBase(%q, ...) = nil error, want scheme error", scheme)
		}
	}
}

func TestRequireSecureURL_AllowsHTTPS(t *testing.T) {
	t.Parallel()

	if err := RequireSecureURL("https://entire.io"); err != nil {
		t.Fatalf("RequireSecureURL(https) = %v, want nil", err)
	}
}

func TestRequireSecureURL_RejectsHTTP(t *testing.T) {
	t.Parallel()

	err := RequireSecureURL("http://localhost:8787")
	if err == nil {
		t.Fatal("RequireSecureURL(http) = nil, want error")
	}

	if !errors.Is(err, ErrInsecureHTTP) {
		t.Fatalf("RequireSecureURL(http) = %v, want ErrInsecureHTTP", err)
	}
}

func TestAuthBaseURL_FallsBackToDefault(t *testing.T) {
	// Default is split-host: an unset ENTIRE_AUTH_BASE_URL must NOT inherit
	// from ENTIRE_API_BASE_URL — local-dev that only sets the data host
	// would otherwise silently point auth at a host that can't mint tokens.
	t.Setenv(BaseURLEnvVar, "https://example.test")
	t.Setenv(AuthBaseURLEnvVar, "")

	if got := AuthBaseURL(); got != DefaultAuthBaseURL {
		t.Fatalf("AuthBaseURL() = %q, want %q", got, DefaultAuthBaseURL)
	}
}

func TestAuthBaseURL_OverrideTakesPrecedence(t *testing.T) {
	t.Setenv(BaseURLEnvVar, "https://data.example.test")
	t.Setenv(AuthBaseURLEnvVar, " https://auth.example.test/ ")

	if got := AuthBaseURL(); got != "https://auth.example.test" {
		t.Fatalf("AuthBaseURL() = %q, want trimmed/normalized override", got)
	}
}

func TestAuthBaseURL_CanonicalisesScheme_HostCase_DefaultPort(t *testing.T) {
	// Same canonicalisation tokenmanager.New applies internally — must match
	// or the keyring key login wrote diverges from the one the manager later
	// reads, producing spurious "not logged in" errors on every data-API call.
	t.Setenv(AuthBaseURLEnvVar, "HTTPS://AUTH.example.com:443/")

	if got := AuthBaseURL(); got != "https://auth.example.com" {
		t.Fatalf("AuthBaseURL() = %q, want canonicalised origin", got)
	}
}

func TestNormalizeOriginURL(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, want string
	}{
		{"https://example.com", "https://example.com"},
		{"https://example.com/", "https://example.com"},
		{"HTTPS://Example.COM", "https://example.com"},
		{"https://example.com:443", "https://example.com"},
		{"http://example.com:80", "http://example.com"},
		{"https://example.com:8443", "https://example.com:8443"},
		{"https://example.com/some/path?q=1#frag", "https://example.com"},
		{"  https://example.com/  ", "https://example.com"},
		{"not a url", "not a url"},
		{"", ""},
	}
	for _, tc := range cases {
		if got := NormalizeOriginURL(tc.in); got != tc.want {
			t.Errorf("NormalizeOriginURL(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestResolveURL(t *testing.T) {
	t.Setenv(BaseURLEnvVar, "http://localhost:8787/")

	got, err := ResolveURL("/oauth/device/code")
	if err != nil {
		t.Fatalf("ResolveURL() error = %v", err)
	}

	if got != "http://localhost:8787/oauth/device/code" {
		t.Fatalf("ResolveURL() = %q, want %q", got, "http://localhost:8787/oauth/device/code")
	}
}

// TestRejectRemovedAuthEnv pins the retired-env gate: any set value — even
// empty — errors with the --server replacement hint; unset passes.
func TestRejectRemovedAuthEnv(t *testing.T) {
	t.Run("unset passes", func(t *testing.T) {
		// LookupEnv, not Getenv: the gate rejects a present-but-empty var
		// too, so an empty export in the parent shell must also skip.
		if _, ok := os.LookupEnv(AuthBaseURLEnvVar); ok {
			t.Skipf("%s set in test environment", AuthBaseURLEnvVar)
		}
		if err := RejectRemovedAuthEnv(); err != nil {
			t.Fatalf("RejectRemovedAuthEnv() with unset var: %v", err)
		}
	})
	t.Run("set errors", func(t *testing.T) {
		t.Setenv(AuthBaseURLEnvVar, "https://custom.example")
		err := RejectRemovedAuthEnv()
		if err == nil || !strings.Contains(err.Error(), "entire login --server") {
			t.Fatalf("err = %v, want --server hint", err)
		}
	})
	t.Run("set-but-empty errors", func(t *testing.T) {
		t.Setenv(AuthBaseURLEnvVar, "")
		if err := RejectRemovedAuthEnv(); err == nil {
			t.Fatal("RejectRemovedAuthEnv() with empty-but-set var: want error")
		}
	})
}
