package auth

import (
	"fmt"

	"github.com/entireio/cli/internal/entireclient/contexts"
	"github.com/entireio/cli/internal/entireclient/tokenstore"
	"github.com/entireio/cli/internal/entireclient/userdirs"
)

// RemoveCurrentContext deletes the active context from contexts.json and
// its keyring token, clearing current_context. It is a no-op (returns nil)
// when there is no current context. Used by logout.
func RemoveCurrentContext() error {
	// Read-modify-write in a single locked Modify so the context we delete
	// is exactly the one we capture the keychain slot from (separate Load +
	// Modify would race a concurrent `auth use`).
	var svc, handle string
	if err := contexts.Modify(userdirs.Config(), func(f *contexts.File) (bool, error) {
		current := f.Find(f.CurrentContext)
		if current == nil {
			return false, nil
		}
		svc, handle = current.KeychainService, current.Handle
		// Delete clears current_context because we're deleting the active
		// one — logged out means logged out, no switch to another identity.
		f.Delete(current.Name)
		return true, nil
	}); err != nil {
		return fmt.Errorf("remove current context: %w", err)
	}
	deleteContextKeychain(svc, handle)
	return nil
}

// RemoveContext deletes the named context from contexts.json and its keyring
// tokens. A missing context is a no-op. Used by `logout --all-contexts` to
// drain every saved login. File.Delete clears current_context when name was
// the active one, so removing the current context this way also logs it out.
func RemoveContext(name string) error {
	var svc, handle string
	if err := contexts.Modify(userdirs.Config(), func(f *contexts.File) (bool, error) {
		c := f.Find(name)
		if c == nil {
			return false, nil
		}
		svc, handle = c.KeychainService, c.Handle
		f.Delete(name)
		return true, nil
	}); err != nil {
		return fmt.Errorf("remove context %q: %w", name, err)
	}
	deleteContextKeychain(svc, handle)
	return nil
}

// deleteContextKeychain best-effort removes a context's keyring slots,
// sequenced off the context just removed from contexts.json. A missing entry
// is fine — the contexts.json removal is what makes us "logged out". Both the
// access slot and its paired refresh slot must go: leaving the long-lived
// refresh token behind would let any later keyring-capable process mint fresh
// access tokens after logout.
func deleteContextKeychain(svc, handle string) {
	if svc == "" || handle == "" {
		return
	}
	_ = tokenstore.Delete(svc, handle)                            //nolint:errcheck // best-effort; contexts.json removal is the source of truth for logout
	_ = tokenstore.Delete(tokenstore.RefreshService(svc), handle) //nolint:errcheck // best-effort; absent refresh slot is fine
}

// SetCurrentContext makes name the active context. Returns an error when
// no context with that name exists (a stale current pointer is a foot-gun).
func SetCurrentContext(name string) error {
	if err := contexts.Modify(userdirs.Config(), func(f *contexts.File) (bool, error) {
		if f.Find(name) == nil {
			return false, fmt.Errorf("no login context named %q (run `entire auth contexts` to list)", name)
		}
		if f.CurrentContext == name {
			return false, nil
		}
		f.CurrentContext = name
		return true, nil
	}); err != nil {
		return fmt.Errorf("set current context: %w", err)
	}
	return nil
}

// Contexts returns all stored login contexts and the current context name,
// for listing/switching. Order matches on-disk order.
func Contexts() ([]*contexts.Context, string, error) {
	f, err := contexts.Load(userdirs.Config())
	if err != nil {
		return nil, "", fmt.Errorf("load contexts: %w", err)
	}
	return f.Contexts, f.CurrentContext, nil
}
