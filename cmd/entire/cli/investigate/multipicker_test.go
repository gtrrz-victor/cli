package investigate

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPickInvestigateAgents_RequiresTwo(t *testing.T) {
	t.Parallel()
	_, err := PickInvestigateAgents(context.Background(), []AgentChoice{{Name: "claude-code"}})
	require.Error(t, err)
	require.Contains(t, err.Error(), "at least 2")
}

func TestPickInvestigateAgents_ContextCancelled(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := PickInvestigateAgents(ctx, []AgentChoice{
		{Name: "claude-code"}, {Name: "codex"},
	})
	require.ErrorIs(t, err, ErrInvestigatePickerCancelled)
}

func TestPickInvestigateAgents_ResultSortedAlphabetically(t *testing.T) {
	t.Parallel()
	got := sortAgentChoices([]AgentChoice{
		{Name: "codex"},
		{Name: "claude-code"},
		{Name: "gemini-cli"},
	})
	require.Equal(t, []AgentChoice{
		{Name: "claude-code"},
		{Name: "codex"},
		{Name: "gemini-cli"},
	}, got)
}

// TestPickInvestigateAgents_PerRunPromptOptional documents the contract
// that PerRun defaults to the empty string when the user skips the text
// area. The huh form itself isn't drivable from a non-TTY test, but the
// zero-value return path is exercised by the cancellation test above;
// this test pins the type-level guarantee that consumers can rely on
// "no per-run prompt" being represented as PerRun == "".
func TestPickInvestigateAgents_PerRunPromptOptional(t *testing.T) {
	t.Parallel()
	var zero PickedInvestigate
	require.Empty(t, zero.PerRun)
	require.Empty(t, zero.Names)
}
