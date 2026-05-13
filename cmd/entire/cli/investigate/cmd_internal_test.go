package investigate

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/entireio/cli/cmd/entire/cli/settings"
	"github.com/entireio/cli/cmd/entire/cli/testutil"
)

// TestSaveInvestigateConfig_WritesLocalFile verifies that
// saveInvestigateConfig persists into .entire/settings.local.json (not the
// committed .entire/settings.json). Mirrors the review-side behaviour so
// agent picker output stays out of project settings.
//
// NOTE: This test uses t.Chdir, which Go forbids combining with
// t.Parallel(). Do not add t.Parallel() here.
func TestSaveInvestigateConfig_WritesLocalFile(t *testing.T) {
	tmp := t.TempDir()
	t.Chdir(tmp)
	testutil.InitRepo(t, tmp)

	cfg := &settings.InvestigateConfig{
		Agents:   []string{"claude-code", "codex"},
		MaxTurns: 4,
		Quorum:   2,
	}
	require.NoError(t, saveInvestigateConfig(context.Background(), cfg))

	// settings.json should NOT contain investigate.
	base, err := os.ReadFile(filepath.Join(tmp, ".entire/settings.json"))
	if err == nil {
		require.NotContains(t, string(base), `"investigate"`,
			"investigate must not be written to project settings")
	}

	// settings.local.json should contain investigate.
	local, err := os.ReadFile(filepath.Join(tmp, ".entire/settings.local.json"))
	require.NoError(t, err)
	require.Contains(t, string(local), `"agents"`)
	require.Contains(t, string(local), `"claude-code"`)
}

// TestResolveDocPaths_PerRunIsolation verifies that two runs with the
// same topic but different run IDs land in distinct directories, so
// they don't stomp each other's findings/timeline.
func TestResolveDocPaths_PerRunIsolation(t *testing.T) {
	t.Parallel()

	const root = "/repo"
	const topic = "why is checkout flaky"

	findings1, timeline1 := resolveDocPaths(root, "aaaaaaaaaaaa", topic, "")
	findings2, timeline2 := resolveDocPaths(root, "bbbbbbbbbbbb", topic, "")

	require.Equal(t,
		filepath.Join(root, ".entire", "investigations", "aaaaaaaaaaaa-why-is-checkout-flaky", "findings.md"),
		findings1,
	)
	require.Equal(t,
		filepath.Join(root, ".entire", "investigations", "aaaaaaaaaaaa-why-is-checkout-flaky", "timeline.md"),
		timeline1,
	)
	require.Equal(t,
		filepath.Join(root, ".entire", "investigations", "bbbbbbbbbbbb-why-is-checkout-flaky", "findings.md"),
		findings2,
	)
	require.Equal(t,
		filepath.Join(root, ".entire", "investigations", "bbbbbbbbbbbb-why-is-checkout-flaky", "timeline.md"),
		timeline2,
	)

	require.NotEqual(t, findings1, findings2,
		"two runs with same topic must not share findings doc")
	require.NotEqual(t, timeline1, timeline2,
		"two runs with same topic must not share timeline doc")
}

// TestResolveDocPaths_OverrideHonored verifies the --output override
// still produces a findings/timeline pair using the parallel
// <findings-without-ext>-timeline.md naming, ignoring run ID and slug.
func TestResolveDocPaths_OverrideHonored(t *testing.T) {
	t.Parallel()

	const root = "/repo"
	const topic = "anything"
	const runID = "deadbeefcafe"

	// Absolute override is used verbatim.
	findings, timeline := resolveDocPaths(root, runID, topic, "/tmp/out.md")
	require.Equal(t, "/tmp/out.md", findings)
	require.Equal(t, "/tmp/out-timeline.md", timeline)

	// Relative override is anchored at worktree root.
	findings, timeline = resolveDocPaths(root, runID, topic, "docs/result.md")
	require.Equal(t, filepath.Join(root, "docs/result.md"), findings)
	require.Equal(t, filepath.Join(root, "docs/result-timeline.md"), timeline)
}
