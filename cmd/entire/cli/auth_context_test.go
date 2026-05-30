package cli

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/entireio/cli/cmd/entire/cli/auth"
	"github.com/entireio/cli/internal/entireclient/tokenstore"
)

// makeContextJWT builds a JWT-shaped token (non-"none" alg) carrying the
// given claims, which is all RecordLoginContext needs.
func makeContextJWT(t *testing.T, payloadJSON string) string {
	t.Helper()
	enc := base64.RawURLEncoding
	header := enc.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	return header + "." + enc.EncodeToString([]byte(payloadJSON)) + "." + enc.EncodeToString([]byte("sig"))
}

func TestRunAuthContexts(t *testing.T) {
	cfgDir := t.TempDir()
	t.Setenv("ENTIRE_CONFIG_DIR", cfgDir)
	restore := tokenstore.UseFileBackendForTesting(filepath.Join(t.TempDir(), "tokens.json"))
	t.Cleanup(restore)

	var empty bytes.Buffer
	if err := runAuthContexts(&empty); err != nil {
		t.Fatalf("runAuthContexts (empty): %v", err)
	}
	if !strings.Contains(empty.String(), "No login contexts") {
		t.Fatalf("empty listing = %q, want a 'No login contexts' hint", empty.String())
	}

	exp := time.Now().Add(time.Hour).Unix()
	token := makeContextJWT(t, fmt.Sprintf(`{"iss":"https://core.example.com","handle":"alice","exp":%d}`, exp))
	if _, err := auth.RecordLoginContext(token, true); err != nil {
		t.Fatalf("RecordLoginContext: %v", err)
	}

	var out bytes.Buffer
	if err := runAuthContexts(&out); err != nil {
		t.Fatalf("runAuthContexts: %v", err)
	}
	got := out.String()
	if !strings.Contains(got, "* core.example.com") {
		t.Fatalf("listing = %q, want current-marked core.example.com", got)
	}
	if !strings.Contains(got, "alice") {
		t.Fatalf("listing = %q, want handle alice", got)
	}
}
