package cli

import (
	"bytes"
	"context"
	"testing"

	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"

	"github.com/entireio/cli/internal/coreapi"
)

// runListRender builds a minimal command wired to renderCoreList via runCoreList so
// the rendering path (table / empty state / --json) is exercised without a
// server: fn returns items directly.
func runListRender(t *testing.T, jsonFlag bool, items []coreapi.Org) (stdout, stderr string) {
	t.Helper()
	prev := activeCoreClient
	activeCoreClient = func(context.Context) (*coreapi.Client, error) {
		// The URL is never contacted: fn below returns items directly, so
		// the client only needs to construct.
		return coreapi.NewWithBearer("http://127.0.0.1:0", "tok")
	}
	t.Cleanup(func() { activeCoreClient = prev })

	cmd := &cobra.Command{
		Use: "list",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return runCoreList(cmd, "No organizations found.", orgColumns, orgRow,
				func(context.Context, *coreapi.Client) ([]coreapi.Org, error) { return items, nil })
		},
	}
	cmd.Flags().Bool("json", false, "Output raw JSON instead of a table")
	var out, errW bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errW)
	args := []string{}
	if jsonFlag {
		args = append(args, "--json")
	}
	cmd.SetArgs(args)
	require.NoError(t, cmd.ExecuteContext(t.Context()))
	return out.String(), errW.String()
}

// Not parallel: swaps the package-level activeCoreClient seam.
func TestRunCoreList_EmptyHumanMessageOnStdout(t *testing.T) {
	out, errOut := runListRender(t, false, nil)
	require.Contains(t, out, "No organizations found.")
	require.Empty(t, errOut, "empty-state message must go to stdout")
}

// Not parallel: swaps the package-level activeCoreClient seam.
func TestRunCoreList_EmptyJSONIsArray(t *testing.T) {
	out, _ := runListRender(t, true, nil)
	require.JSONEq(t, "[]", out, "empty --json list must be [], not null")
}

// Not parallel: swaps the package-level activeCoreClient seam.
func TestRunCoreList_RendersRows(t *testing.T) {
	out, errOut := runListRender(t, false, []coreapi.Org{{ID: testDeleteULID, Name: "acme", Region: "us"}})
	require.Contains(t, out, "NAME")
	require.Contains(t, out, "acme")
	require.Empty(t, errOut)
}
