package auth

import (
	"errors"
	"fmt"

	"github.com/entireio/cli/internal/entireclient/contexts"
	"github.com/entireio/cli/internal/entireclient/tokenstore"
	"github.com/entireio/cli/internal/entireclient/userdirs"
)

// RemoveCurrentContext deletes the active context's keyring tokens and its
// contexts.json entry, clearing current_context. It is a no-op (returns nil)
// when there is no current context. Used by logout.
func RemoveCurrentContext() error {
	f, err := contexts.Load(userdirs.Config())
	if err != nil {
		return fmt.Errorf("remove current context: %w", err)
	}
	current := f.Find(f.CurrentContext)
	if current == nil {
		return nil
	}
	return RemoveContext(current.Name)
}

// RemoveContext deletes the named context's keyring tokens, then its
// contexts.json entry. A missing context is a no-op. Used by logout and
// `logout --all-contexts`. File.Delete clears current_context when name was
// the active one, so removing the current context this way also logs it out.
//
// Credential deletion comes first and is part of the success contract:
// removing the entry and then failing the keyring delete would report
// "Logged out." while the long-lived refresh token survives on the machine,
// mintable by any keyring-capable process. The inverse partial failure
// (slots gone, entry left) is benign — the context reads as not logged in
// and a retried logout no-ops the deletes.
func RemoveContext(name string) error {
	f, err := contexts.Load(userdirs.Config())
	if err != nil {
		return fmt.Errorf("remove context %q: %w", name, err)
	}
	c := f.Find(name)
	if c == nil {
		return nil
	}
	if err := deleteContextKeychain(c.KeychainService, c.Handle); err != nil {
		return fmt.Errorf("remove credentials for %q: %w", name, err)
	}
	if err := contexts.Modify(userdirs.Config(), func(f *contexts.File) (bool, error) {
		if f.Find(name) == nil {
			return false, nil
		}
		f.Delete(name)
		return true, nil
	}); err != nil {
		return fmt.Errorf("remove context %q: %w", name, err)
	}
	return nil
}

// deleteContextKeychain removes a context's keyring slots. A missing entry
// is fine; any other failure surfaces so logout doesn't claim success over
// surviving credentials. The refresh slot goes first — it's the long-lived
// credential, and if the second delete then fails, the leftover access
// token at least expires on its own.
func deleteContextKeychain(svc, handle string) error {
	if svc == "" || handle == "" {
		return nil
	}
	if err := tokenstore.Delete(tokenstore.RefreshService(svc), handle); err != nil && !errors.Is(err, tokenstore.ErrNotFound) {
		return fmt.Errorf("delete refresh token: %w", err)
	}
	if err := tokenstore.Delete(svc, handle); err != nil && !errors.Is(err, tokenstore.ErrNotFound) {
		return fmt.Errorf("delete access token: %w", err)
	}
	return nil
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
