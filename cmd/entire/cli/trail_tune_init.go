package cli

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/entireio/cli/cmd/entire/cli/interactive"
	"github.com/entireio/cli/cmd/entire/cli/paths"
	"github.com/entireio/cli/cmd/entire/cli/runnerdefaults"

	"charm.land/huh/v2"
)

// ensureRunnersPresent scaffolds the default runner set when a repo has none
// yet, so `tune` doubles as onboarding. It is a no-op when runners already
// exist, and returns an error when the user declined or creation failed.
// Writing is gated on confirmation (interactive prompt, or the --yes flag for
// non-interactive runs).
func ensureRunnersPresent(w, errW io.Writer, repoRoot string, assumeYes bool) error {
	dir := runnersDir(repoRoot)
	existing, _ := filepath.Glob(filepath.Join(dir, "*.json")) //nolint:errcheck // bad pattern only; treated as "none found"
	if len(existing) > 0 {
		return nil
	}

	defaults, err := runnerdefaults.Files()
	if err != nil {
		return fmt.Errorf("loading default runners: %w", err)
	}

	if !assumeYes {
		if !interactive.CanPromptInteractively() {
			return fmt.Errorf("no runner configs found under %s; re-run with --yes to create the default set (%d runners)", dir, len(defaults))
		}
		confirmed, err := confirmCreateRunners(len(defaults))
		if err != nil {
			return err
		}
		if !confirmed {
			return errors.New("no runner configs created (declined)")
		}
	}

	if err := os.MkdirAll(dir, 0o755); err != nil { //nolint:gosec // config dir, conventional perms
		return fmt.Errorf("creating %s: %w", dir, err)
	}
	for _, f := range defaults {
		dest := filepath.Join(dir, f.Name)
		if err := os.WriteFile(dest, f.Data, 0o644); err != nil { //nolint:gosec // runner configs are repo-committed, world-readable config
			return fmt.Errorf("writing %s: %w", dest, err)
		}
		fmt.Fprintf(w, "created %s\n", filepath.Join(paths.EntireDir, "runners", f.Name))
	}
	fmt.Fprintf(errW, "Created %d default runner(s); tailoring them to this repo…\n", len(defaults))
	return nil
}

func confirmCreateRunners(n int) (bool, error) {
	var ok bool
	form := NewAccessibleForm(
		huh.NewGroup(
			huh.NewConfirm().
				Title(fmt.Sprintf("No trail runners found. Create the default set (%d runners) in .entire/runners/?", n)).
				Description("Written from the built-in defaults, then tailored to this repo.").
				Value(&ok),
		),
	)
	if err := form.Run(); err != nil {
		return false, fmt.Errorf("runner-creation prompt cancelled: %w", err)
	}
	return ok, nil
}
