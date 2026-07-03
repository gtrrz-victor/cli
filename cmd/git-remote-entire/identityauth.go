package main

// EXPERIMENT (jwt-latency-experiment branch): mint a jurisdiction identity
// token (ADR 20260612, scope=openid, aud=https://<jurisdiction>.<family>)
// instead of a per-(repo,action) repo-scoped token, and send that on git
// smart-HTTP requests. The data plane authorizes it live against regional
// SpiceDB. Enabled with ENTIRE_GIT_AUTH=identity.

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/entireio/cli/internal/entireclient/httputil"
	"github.com/entireio/cli/internal/remotehelper/debuglog"
)

const identityAuthMode = "identity"

func identityAuthEnabled() bool {
	return os.Getenv("ENTIRE_GIT_AUTH") == identityAuthMode
}

// identityTokenSource satisfies the same Token(ctx, audienceSuffix, action)
// seam as repocreds.Cache but ignores the repo/action: one identity token
// covers every repo the account can reach, so a single mint serves the whole
// process. audienceSuffix/action params are kept so setAuth doesn't care
// which mode is active.
type identityTokenSource struct {
	coreURL string
	login   func(context.Context) (string, error)
	client  *http.Client

	mu        sync.Mutex
	token     string
	expiresAt time.Time
}

func newIdentityTokenSource(coreURL string, login func(context.Context) (string, error), client *http.Client) *identityTokenSource {
	return &identityTokenSource{coreURL: coreURL, login: login, client: client}
}

// Token returns the memoized identity token, minting on first use or after
// expiry (1m safety margin, mirroring repocreds.SafetyMargin).
func (s *identityTokenSource) Token(ctx context.Context, _, _ string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token != "" && time.Now().Before(s.expiresAt) {
		return s.token, nil
	}

	loginJWT, err := s.login(ctx)
	if err != nil {
		return "", fmt.Errorf("refresh login token: %w", err)
	}

	audience, err := identityAudience(s.coreURL, loginJWT)
	if err != nil {
		return "", err
	}

	// The identity token must be minted by the core owning the target
	// jurisdiction (isJurisdictionAudience is an exact match on that core's
	// own audience). For a repo homed outside the login's jurisdiction the
	// exchange goes to the sibling core, which accepts our login JWT via the
	// foreign-session path (validateForeignSessionExchange) and mints the
	// identity token in the same single POST. ENTIRE_IDENTITY_CORE overrides
	// the exchange target for that cross-juris case.
	coreURL := s.coreURL
	if override := strings.TrimSpace(os.Getenv("ENTIRE_IDENTITY_CORE")); override != "" {
		coreURL = strings.TrimRight(override, "/")
	}

	form := url.Values{}
	form.Set("grant_type", httputil.GrantTypeTokenExchange)
	form.Set("subject_token", loginJWT)
	form.Set("subject_token_type", "urn:ietf:params:oauth:token-type:access_token")
	form.Set("requested_token_type", "urn:ietf:params:oauth:token-type:access_token")
	form.Set("audience", audience)
	form.Set("scope", "openid")
	form.Set("client_id", "entire-cli") // same client as repocreds' oauthClientID

	token, expiresIn, err := httputil.PostOAuthToken(ctx, s.client, coreURL, form)
	if err != nil {
		return "", fmt.Errorf("identity token exchange (aud=%s at %s): %w", audience, coreURL, err)
	}
	if strings.TrimSpace(token) == "" {
		return "", errors.New("identity token exchange returned an empty access token")
	}

	ttl := time.Duration(expiresIn) * time.Second
	margin := min(time.Minute, ttl/2)
	s.token = token
	s.expiresAt = time.Now().Add(ttl - margin)
	debuglog.Printf("identity token minted: aud=%s ttl=%s", audience, ttl)
	return token, nil
}

// identityAudience derives the jurisdiction audience the data plane pins
// identity tokens to (its ENTIRE_JURISDICTION_AUDIENCE). Precedence:
// ENTIRE_IDENTITY_AUDIENCE verbatim; else https://<home_jurisdiction from
// the login JWT>.<domain family of coreURL>. Cross-jurisdiction targets
// (repo homed outside the login's jurisdiction) are out of scope for this
// experiment — the /.well-known doc would need to advertise the cluster's
// jurisdiction for that.
func identityAudience(coreURL, loginJWT string) (string, error) {
	if aud := strings.TrimSpace(os.Getenv("ENTIRE_IDENTITY_AUDIENCE")); aud != "" {
		return aud, nil
	}
	jurisdiction, err := homeJurisdictionFromLoginJWT(loginJWT)
	if err != nil {
		return "", err
	}
	family, err := domainFamily(coreURL)
	if err != nil {
		return "", err
	}
	return "https://" + jurisdiction + "." + family, nil
}

// domainFamily maps the login core's host to the environment apex the
// jurisdiction audience is templated on (prod entire.io / staging partial.to),
// mirroring auth.entireDomainFamily.
func domainFamily(coreURL string) (string, error) {
	u, err := url.Parse(coreURL)
	if err != nil {
		return "", fmt.Errorf("parse core URL %q: %w", coreURL, err)
	}
	host := strings.ToLower(u.Hostname())
	switch {
	case host == "entire.io" || strings.HasSuffix(host, ".entire.io"):
		return "entire.io", nil
	case host == "partial.to" || strings.HasSuffix(host, ".partial.to"):
		return "partial.to", nil
	}
	return "", fmt.Errorf("core %q is not in a known domain family; set ENTIRE_IDENTITY_AUDIENCE explicitly", coreURL)
}

// homeJurisdictionFromLoginJWT reads the home_jurisdiction claim without
// verifying the signature (we only route with it; the server re-verifies).
// Copied from auth.homeJurisdictionFromLoginJWT, which is unexported.
func homeJurisdictionFromLoginJWT(loginJWT string) (string, error) {
	parts := strings.Split(loginJWT, ".")
	if len(parts) < 2 {
		return "", errors.New("login token is not a JWT")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return "", fmt.Errorf("decode login token payload: %w", err)
	}
	var claims struct {
		HomeJurisdiction string `json:"home_jurisdiction"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil {
		return "", fmt.Errorf("parse login token payload: %w", err)
	}
	if claims.HomeJurisdiction == "" {
		return "", errors.New("login token has no home_jurisdiction claim; set ENTIRE_IDENTITY_AUDIENCE explicitly")
	}
	return claims.HomeJurisdiction, nil
}
