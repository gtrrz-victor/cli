package cli

import (
	"fmt"
	"io"

	"github.com/entireio/cli/cmd/entire/cli/auth"
	"github.com/spf13/cobra"
)

// newAuthUseCmd switches the active login context — the one the
// control-plane commands authenticate as. Clones resolve their context
// per cluster, so this primarily affects org/project/repo operations.
func newAuthUseCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "use <context>",
		Short: "Switch the active login context",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			if err := auth.SetCurrentContext(args[0]); err != nil {
				return err //nolint:wrapcheck // already a user-facing message
			}
			fmt.Fprintf(cmd.OutOrStdout(), "Now using context %q.\n", args[0])
			return nil
		},
	}
}

// newAuthContextsCmd lists the stored login contexts and marks the active
// one. Purely local — it reads contexts.json, no network.
func newAuthContextsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "contexts",
		Short: "List stored login contexts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runAuthContexts(cmd.OutOrStdout())
		},
	}
}

func runAuthContexts(w io.Writer) error {
	all, current, err := auth.Contexts()
	if err != nil {
		return err //nolint:wrapcheck // already a user-facing message
	}
	if len(all) == 0 {
		fmt.Fprintln(w, "No login contexts. Run 'entire login' to authenticate.")
		return nil
	}
	for _, c := range all {
		marker := " "
		if c.Name == current {
			marker = "*"
		}
		fmt.Fprintf(w, "%s %s\t%s\t%s\n", marker, c.Name, c.Handle, c.CoreURL)
	}
	return nil
}
