package importclaude

import (
	"strings"
	"testing"
)

func TestSplitTurns_TwoPromptsBoundedByNext(t *testing.T) {
	t.Parallel()
	full := []byte(strings.Join([]string{
		`{"type":"user","uuid":"u1","parentUuid":"","timestamp":"2026-06-20T00:00:00Z","message":{"role":"user","content":"first"}}`,
		`{"type":"assistant","uuid":"a1","message":{"id":"m1","model":"claude-x","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":10,"output_tokens":5}}}`,
		`{"type":"user","uuid":"u2","parentUuid":"a1","timestamp":"2026-06-20T00:01:00Z","message":{"role":"user","content":"second"}}`,
		`{"type":"assistant","uuid":"a2","message":{"id":"m2","model":"claude-x","content":[{"type":"text","text":"done"}],"usage":{"input_tokens":20,"output_tokens":7}}}`,
	}, "\n") + "\n")

	turns, err := SplitTurns(full, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 2 {
		t.Fatalf("want 2 turns, got %d", len(turns))
	}
	if turns[0].LineStart != 0 || turns[0].LineEnd != 2 {
		t.Errorf("turn0 bounds = [%d,%d), want [0,2)", turns[0].LineStart, turns[0].LineEnd)
	}
	if turns[0].Prompt != "first" || turns[1].Prompt != "second" {
		t.Errorf("prompts = %q,%q", turns[0].Prompt, turns[1].Prompt)
	}
	if turns[0].UUID != "u1" || turns[1].ParentUUID != "a1" {
		t.Errorf("uuid/parent wrong: %+v %+v", turns[0], turns[1])
	}
	if turns[0].Model != "claude-x" {
		t.Errorf("turn0 model = %q, want claude-x", turns[0].Model)
	}
	if turns[0].Tokens == nil || turns[0].Tokens.OutputTokens != 5 {
		t.Errorf("turn0 tokens not bounded to its own turn: %+v", turns[0].Tokens)
	}
	if turns[1].Tokens == nil || turns[1].Tokens.OutputTokens != 7 {
		t.Errorf("turn1 tokens wrong: %+v", turns[1].Tokens)
	}
}

func TestSplitTurns_ToolResultIsNotATurn(t *testing.T) {
	t.Parallel()
	full := []byte(strings.Join([]string{
		`{"type":"user","uuid":"u1","message":{"role":"user","content":"do it"}}`,
		`{"type":"assistant","uuid":"a1","message":{"id":"m1","content":[{"type":"tool_use","id":"t1","name":"Bash","input":{}}],"usage":{"output_tokens":3}}}`,
		`{"type":"user","uuid":"r1","message":{"content":[{"type":"tool_result","tool_use_id":"t1","content":"out"}]}}`,
	}, "\n") + "\n")
	turns, err := SplitTurns(full, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(turns) != 1 {
		t.Fatalf("tool_result must not start a turn; want 1 turn, got %d", len(turns))
	}
}
