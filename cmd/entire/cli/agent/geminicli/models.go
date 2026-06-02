package geminicli

import (
	"context"

	"github.com/entireio/cli/cmd/entire/cli/agent"
)

var _ agent.ModelLister = (*GeminiCLIAgent)(nil)

// ListModels returns example Gemini model identifiers for `entire review
// --model`. The Gemini CLI has no model-enumeration command, so these are
// advisory examples — `--model` forwards any value the gemini CLI accepts.
func (g *GeminiCLIAgent) ListModels(_ context.Context) ([]agent.ModelInfo, error) {
	return []agent.ModelInfo{
		{ID: "gemini-2.5-pro", Note: "example"},
		{ID: "gemini-2.5-flash", Note: "example — faster"},
	}, nil
}
