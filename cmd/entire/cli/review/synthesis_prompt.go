// Package review — see env.go for package-level rationale.
//
// synthesis_prompt.go builds the LLM prompt that asks a configured summary
// provider to synthesize a unified verdict across N per-agent review reports.
// It is a pure-function composer with no I/O; all logic is testable without
// network calls or TTY state.
package review

import (
	"fmt"
	"strings"

	reviewtypes "github.com/entireio/cli/cmd/entire/cli/review/types"
)

// composeSynthesisPrompt builds the LLM prompt asking the provider to
// synthesize a unified verdict across N agent reviews. Format:
//
//	You reviewed the same code change with N agents. Here are their reports:
//
//	─── claude-code ───
//	<narrative from agent's AssistantText events, joined>
//
//	─── codex ───
//	<narrative>
//
//	...
//
//	<instructions to write a tight verdict: lead with a one-line decision,
//	then only the sections that have real content (Common/Unique findings,
//	Disagreements, Priority order), proportional to the size of the change>
//
//	<perRunPrompt, if any — appended as user's per-run instructions>
//
// The instructions deliberately do not mandate a fixed multi-section template:
// forcing every header produced padded "none" filler on small/clean changes,
// so sections are opt-in and the judge is told to stay proportional.
//
// Agents with no usable narrative (empty AssistantText) are filtered out
// upstream by usableAgentRuns, so the header count and the body are both
// scoped to agents that produced narrative output. SynthesisSink already
// guards on len(usable) >= 2 before calling, so the empty case won't reach
// the LLM in production.
func composeSynthesisPrompt(summary reviewtypes.RunSummary, perRunPrompt string, profileName string, task string) string {
	usable := usableAgentRuns(summary)
	if len(usable) == 0 {
		return ""
	}

	var b strings.Builder

	fmt.Fprintf(&b, "You reviewed the same code change with %d agents. You are the final reviewer; critically adjudicate their reports instead of blindly summarizing.\n", len(usable))
	if profileName != "" {
		fmt.Fprintf(&b, "Review profile: %s\n", profileName)
	}
	if strings.TrimSpace(task) != "" {
		fmt.Fprintf(&b, "Canonical task: %s\n", strings.TrimSpace(task))
	}
	b.WriteString("\nHere are their reports:\n")

	for _, run := range usable {
		narrative := joinAssistantText(run.Buffer)
		if narrative == "" {
			continue
		}
		fmt.Fprintf(&b, "\n─── %s ───\n", run.Name)
		b.WriteString(narrative)
		b.WriteString("\n")
	}

	b.WriteString(`
Critically evaluate the worker reports. Do not blindly summarize.

Rules:
  - Prefer findings backed by concrete evidence (file, function, behavior, test, or diff detail).
  - Discard unsupported or speculative claims unless they are clearly labeled as needing verification.
  - Identify contradictions between workers and decide which claim is better supported.
  - Merge duplicate findings.

Write a tight final report:
  - Open with a one-line verdict (approve / approve with nits / request changes) and a one-sentence rationale.
  - Then list only the findings that matter, highest priority first, each as a single bullet with an evidence pointer.
  - Include a section only when it has real content; omit empty sections instead of writing "none". Use these as needed, in this order: Common findings, Unique findings, Disagreements (or rejected false positives), Priority order / next actions.

Be brief and proportional to the change: a small or clean change should get a verdict and a few bullets, nothing more. Do not pad, restate the diff, or invent findings to fill a template.`)

	if perRunPrompt != "" {
		b.WriteString("\n\nPer-run user instructions:\n")
		b.WriteString(perRunPrompt)
	}

	return b.String()
}

// usableAgentRuns returns agent runs that have non-empty AssistantText
// narrative in their event buffer, in the original order from the summary.
// The filter is on narrative content alone — Status is not checked. In
// practice this drops most cancelled and errored runs (they typically don't
// produce assistant output before exiting), but a cancelled agent that
// emitted text mid-stream is still considered usable. The synthesis prompt
// uses what the agent actually said, regardless of how the run terminated.
func usableAgentRuns(summary reviewtypes.RunSummary) []reviewtypes.AgentRun {
	var result []reviewtypes.AgentRun
	for _, run := range summary.AgentRuns {
		if joinAssistantText(run.Buffer) == "" {
			continue
		}
		result = append(result, run)
	}
	return result
}
