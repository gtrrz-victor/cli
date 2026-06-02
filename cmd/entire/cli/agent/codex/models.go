package codex

import (
	"context"

	"github.com/entireio/cli/cmd/entire/cli/agent"
)

var _ agent.ModelLister = (*CodexAgent)(nil)

// ListModels returns example Codex model identifiers for `entire review
// --model`. Codex has no model-enumeration command, so these are advisory
// examples — `--model` forwards any value the codex CLI accepts.
func (c *CodexAgent) ListModels(_ context.Context) ([]agent.ModelInfo, error) {
	return []agent.ModelInfo{
		{ID: "gpt-5-codex", Note: "example — Codex-tuned"},
		{ID: "gpt-5", Note: "example"},
		{ID: "o3", Note: "example"},
	}, nil
}
