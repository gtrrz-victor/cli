package agent

import "context"

// ModelInfo describes one model an agent can run via `--model`.
type ModelInfo struct {
	// ID is the value passed to the agent CLI's --model flag (an exact model
	// identifier or a provider alias such as "sonnet").
	ID string
	// Note is an optional short human hint (e.g. "alias", "faster",
	// "example") shown alongside the ID. It carries no behavior.
	Note string
}

// ModelLister is an optional capability for agents that can advertise the
// models usable with `entire review --model`.
//
// Built-in agents whose CLI has no model-enumeration command (claude-code,
// codex, gemini) return a curated, intentionally non-exhaustive list of common
// models/aliases — `--model` ultimately accepts anything the agent CLI does.
// Agents whose CLI can enumerate models live (e.g. Pi's `pi --list-models`)
// may shell out instead.
type ModelLister interface {
	Agent

	// ListModels returns the advertised models for this agent. The list is
	// advisory; callers must still allow arbitrary `--model` values.
	ListModels(ctx context.Context) ([]ModelInfo, error)
}

// AsModelLister returns the agent as a ModelLister if it implements the
// capability. Unlike AsTextGenerator this does not consult CapabilityDeclarer:
// the model list is advisory only, so a plain type assertion is sufficient and
// keeps the external-agent capability protocol unchanged.
func AsModelLister(ag Agent) (ModelLister, bool) {
	if ag == nil {
		return nil, false
	}
	ml, ok := ag.(ModelLister)
	return ml, ok
}
