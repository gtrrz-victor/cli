# `entire inspect` Command

`entire inspect` (aliased as `entire review`) runs a named review profile. A
profile defines one canonical task (for example `general`, `security`, or
`accessibility`), a set of **inspector** agents that all run that task, and a
panel of **judges** that evaluate the inspectors' reports. With one judge that
judge writes the verdict; with two or more, each judge renders an independent
verdict and the **chair** merges them into one final verdict, surfacing
agreement and dissent. Inspector sessions are immutable facts attached to
checkpoints; the final verdict is stored locally in the review manifest for
findings/fix workflows.

## Command Surface

```
entire inspect                          # Interactive: pick a profile to run. Non-interactive: list profiles + error
entire inspect security                 # Run a named profile
entire inspect --profile accessibility  # Same, flag form
entire inspect --list                   # List configured profiles (inspectors + judges), marking the default
entire inspect --configure                    # Interactive: guided wizard. Non-interactive: list agents + profiles
entire inspect --configure --profile general --set-agents claude-code,codex --set-judge claude-code
                                               # Configure a profile non-interactively (no TUI)
entire inspect --configure --profile sec --set-slot claude-code=opus --set-slot codex --set-judge claude-code=opus --set-judge codex=gpt-5 --set-chair claude-code=opus
entire inspect --configure --profile general --set-model codex=gpt-5-codex --set-task "..."
entire inspect --edit --profile general       # Advanced skill-level config (skill picker)
entire inspect --agent <name>           # Run one inspector from the selected profile
entire inspect --agent <name> --model <model>  # Override that inspector's model for this run
entire inspect --agents                 # List the profile's inspectors (valid --agent values)
entire inspect --models                 # List models each agent advertises
entire inspect --models --agent codex   # ...filtered to one agent
entire inspect --prompt "focus on auth" # Add one-off instructions
entire inspect --findings               # Browse local review findings
```

A bare `entire inspect` never silently runs a default crew. In an interactive
terminal it opens a chooser listing the configured profiles (default
pre-selected); in a non-interactive context it prints the profiles and exits
with an error so automation must name a profile explicitly. To tag an
already-finished session as a review after the fact, use
`entire attach --review <session-id>` (the old `entire review attach`
subcommand was removed).

When no profiles are configured, interactive `entire inspect` runs a guided
setup: choose a review focus (or `Custom…` to write the task), build the
inspector crew (a single-screen add/edit/remove slot list seeded with all
launchable agents — the same agent may appear more than once on different or
identical models), then choose the judges (another slot list) and, when there
are ≥2, a chair. It saves the profile and asks before starting agents.

`entire inspect --configure` is the configuration entry point:
- With `--set-agents` / `--set-slot` / `--set-judge` / `--set-chair` /
  `--set-task` / `--set-model agent=model`, it writes the profile
  non-interactively (no TUI). `--set-*` writes preserve profile-level fields the
  flags don't touch (custom `task`, etc.).
- With no `--set-*` flags in an interactive terminal, it opens the guided
  wizard (which already lists the selectable agents).
- With no `--set-*` flags in a non-interactive context, it prints the discovery
  view: the **available review agents** (those with review-runner adapters,
  marking which have hooks installed) and the **currently configured profiles**,
  plus an example `--set-*` command. Defaults are intentionally simple:
  Claude/Codex use `/review`, Gemini uses the profile task directly, and Claude
  is preferred as the default judge when available.

When two or more adapter-backed inspectors are configured and `--agent` is not
set, `entire inspect` fans out to all configured inspectors. There is no per-run
multi-picker: the profile is the fan-out contract. Multi-inspector profiles must
resolve at least one judge; the judges run after inspectors finish and produce
the final verdict.

## Settings Schema

Profiles are configured in clone-local preferences (or settings) under
`review_profiles`:

```json
{
  "review_default_profile": "general",
  "review_profiles": {
    "general": {
      "task": "Review this change for correctness, regressions, tests, and maintainability.",
      "agents": {
        "claude-code": {"skills": ["/review"]},
        "codex": {"skills": ["/review"]}
      },
      "judges": [{"agent": "claude-code", "model": "opus"}]
    },
    "security": {
      "task": "Review this change for auth, injection, secrets, and privilege-boundary bugs.",
      "agents": {
        "claude-sonnet": {"agent": "claude-code", "model": "sonnet", "skills": ["/security-review"]},
        "codex": {"model": "gpt-5-codex", "skills": ["/review"], "prompt": "Focus on security."}
      },
      "judges": [
        {"agent": "claude-code", "model": "opus"},
        {"agent": "codex", "model": "gpt-5"}
      ],
      "chair": "claude-code:opus"
    }
  }
}
```

- The profile-level `task` is the shared work item.
- Each `agents` map entry is an **inspector** id. For simple entries the id is
  the agent name; to run the same agent more than once, use aliases and set
  `agent` plus `model`. Per-inspector `skills`, `prompt`, and `model` adapt the
  task to agent-specific mechanics.
- `judges` is the panel: each entry is an agent (+ optional model) that renders
  a verdict. A judge need not be one of the inspectors. `chair` (an `agent` or
  `agent:model` selector) names the judge that merges a ≥2-judge panel; it
  defaults to the first judge.
- **Back-compat:** the legacy `master` (an inspector id) and `master_agent` /
  `master_model` fields are still honored as a single judge when `judges` is
  empty. `entire inspect --configure` writes `judges`/`chair` going forward.

`entire inspect --models` lists the models each agent advertises via the
optional `agent.ModelLister` capability (`cmd/entire/cli/agent/model_lister.go`).
Only claude-code advertises a list (its curated, real aliases opus/sonnet/haiku).
Agents whose CLI has no enumeration command (codex, gemini) do not implement
`ListModels`; the picker offers only Default + Custom for them, and `--models`
notes there are none. The `--model` flag still forwards any value the agent CLI
accepts.

Settings fields: `EntireSettings.ReviewProfiles` and
`EntireSettings.ReviewDefaultProfile` in `cmd/entire/cli/settings/settings.go`.

## How It Works (env-var handshake)

1. `entire inspect` resolves a profile (positional/`--profile`, else the
   interactive chooser, else — non-interactively — an error). It composes
   inspector prompts via `review.ComposeReviewPrompt` and computes scope
   (mainline base ref via `review.ComputeScopeStats`, overridable with `--base`).
2. **For agents with review-runner adapters** (claude-code, codex, gemini-cli):
   the spawned process is given env vars
   `ENTIRE_REVIEW_{SESSION,AGENT,SKILLS,PROMPT,STARTING_SHA}` that the agent's
   `UserPromptSubmit` lifecycle hook reads to tag the session as
   `Kind = "agent_review"` with the configured skills/prompt. Each spawned
   process has its own env, so multiple worktrees and multi-agent runs are
   correct by construction (no shared marker file, no race).
3. **For agents without review-runner adapters yet**: `RunMarkerFallback` writes
   a `PendingReviewMarker` file and prints guidance — the user opens the agent
   themselves and runs the skills, then tags it with `entire attach --review`.
4. Inspectors run the selected profile's task; each session ends naturally.
5. In multi-inspector profiles, the judge panel runs after inspectors finish
   (see Multi-Agent UI). Each judge receives all inspector reports and renders a
   verdict; the chair merges a ≥2-judge panel into the final verdict.
6. On the next `git commit`, the PostCommit hook condenses inspector sessions
   into the checkpoint on `entire/checkpoints/v1`, with `Kind`, `ReviewSkills`,
   and `ReviewPrompt` recorded in `CommittedMetadata`.
7. The `CheckpointSummary` sets `HasReview = true` for O(1) lookup. `HasReview`
   is an umbrella "any review happened" flag.
8. `entire status` and the re-run guard read `HasReview` from the checkpoint
   metadata (no commit history walking).

## Checkpoint Metadata

Review metadata is stored at two levels on `entire/checkpoints/v1`:

- **`CommittedMetadata` (per-session)**: `kind: "agent_review"`, `review_skills:
  ["/skill1", "/skill2"]`, `review_prompt: "..."`
- **`CheckpointSummary` (per-checkpoint)**: `has_review: true` (umbrella; set
  when any session in the checkpoint has a review-kind `Kind`)

## Architecture

- **`AgentReviewer` interface** (`cmd/entire/cli/review/types/reviewer.go`):
  per-agent contract with `Name() string` and `Start(ctx, RunConfig)
  (Process, error)`. Each adapter-backed inspector implements this in its own
  package.
- **`ReviewerTemplate`** (`cmd/entire/cli/review/types/template.go`): shared
  scaffolding (spawn → pipe stdout → run parser → forward events → close +
  bounded stderr capture). Each agent supplies only its `BuildCmd` (argv/env)
  and `Parser` (stdout-to-Event stream).
- **`Sink` interface**: consumers of the event stream. Production sinks:
  `DumpSink` (post-run per-agent narrative), `TUISink` (Bubble Tea live
  dashboard), `SynthesisSink` (final verdict). Composed by
  `composeMultiAgentSinks` based on TTY detection.
- **`Run` / `RunMulti`** (`run.go`, `run_multi.go`): single- and N-agent
  orchestrators. In `RunMulti` each inspector runs concurrently in its own
  goroutine; events fan into a single dispatch loop so the serial-dispatch
  contract holds. Per-inspector skills/prompts are injected via
  `perAgentConfiguredReviewer`.
- **Judge resolution** (`profile.go`): `profileJudges` returns the resolved
  panel `[]judgeSpec` and the chair index (explicit `judges`, else the legacy
  `master_agent`, else the legacy worker `master`). `profileMasterIdentity`
  reports the chair/single judge for callers that need one identity.
- **Panel synthesis** (`synthesis_panel.go`): `PanelSynthesisProvider`
  implements `SynthesisProvider`, so `SynthesisSink` consumes it unchanged. It
  fans out to each judge in parallel; one judge collapses to that verdict; with
  ≥2 the chair merges the verdicts (`composeChairPrompt`) and the individual
  verdicts are appended as a `## Panel`. Failed judges are dropped; if all fail
  the error surfaces as "final report unavailable".
- **Env-var contract** (`env.go`): single source of truth for `ENTIRE_REVIEW_*`.
- **Scope detection** (`scope.go`): first existing of
  `origin/HEAD → origin/main → origin/master → main → master`, overridable via
  `--base <ref>` (validated through go-git's `ResolveRevision`).

## Multi-Agent UI

When `RunMulti` is dispatched in a TTY, the sink slice is
`[TUISink, DumpSink, SynthesisSink]`:

- **`TUISink` / `reviewTUIModel`**: live dashboard with one row per inspector;
  `Ctrl+O` drills into an agent's full event buffer; `Ctrl+C` cancels via the
  shared `CancelFunc`. `RunFinished` blocks on dismissal so `DumpSink` renders
  below rather than overlapping.
- **`SynthesisSink`** (`synthesis_sink.go`): after the dump it composes an
  adjudication prompt from all inspector narratives + per-run prompt + profile
  task and calls its `SynthesisProvider`. For a single judge that's an
  `AgentSynthesisProvider`; for a panel it's a `PanelSynthesisProvider` (judges
  in parallel → chair merge). Skipped when cancelled or fewer than 2 inspectors
  produced usable output. Provider failures degrade gracefully.
- **Sink composition** (`composeMultiAgentSinks` in `cmd.go`): pure helper
  taking explicit `isTTY`/`canPrompt` so tests don't depend on real TTY
  detection.

## Skill Discovery (Claude Code)

`DiscoverReviewSkills` (`cmd/entire/cli/agent/claudecode/discovery.go`) walks
three roots: plugin cache, user skills (`~/.claude/skills`), and user
commands/agents. `pickLatestVersion` picks ONE version directory per plugin
(highest valid semver, else lexicographic max) to avoid duplicate skill entries.

## Anti-Features (do NOT recreate)

- `PendingReviewMarker` for adapter-backed inspectors (env-var handshake makes
  it unnecessary; the marker only backs the manual-attach fallback)
- `WorktreePath`-style marker scoping / `AgentEntries` map (env per process)
- Marker overwrite tripwire / refuse-attach guard
- `--track-only` / `--postreview` / `--finalize` / empty review commits
- `Launcher` + `HeadlessLauncher` as separate interfaces (single `AgentReviewer`)
- Agent-specific stdout post-processing in shared multi-agent code (per-agent
  parsers own their format; shared code only sees `Event` variants)
- Fabricated "example" model lists for agents without an enumeration command
  (codex/gemini advertise nothing; Default + Custom only)
- A single "master" that both reviews and adjudicates as a worker slot (judges
  are a separate panel; a worker is never implicitly the judge)

## Key Files

- `cmd/entire/cli/review/cmd.go` — `NewCommand()`, `runReview` dispatch fork,
  `runReviewListProfiles` (`--list`), judge-panel wiring, `composeMultiAgentSinks`
- `cmd/entire/cli/review/picker.go` — guided setup, focus picker (presets +
  custom task), `pickSlotList` (inspectors + judges), chair picker, profile
  chooser
- `cmd/entire/cli/review/profile.go` — profile resolution, `profileJudges`,
  default tasks
- `cmd/entire/cli/review/synthesis_panel.go` — `PanelSynthesisProvider` +
  `composeChairPrompt`
- `cmd/entire/cli/review/synthesis_sink.go` / `synthesis_prompt.go` — final
  verdict sink + adjudication prompt
- `cmd/entire/cli/review/marker_fallback.go` — manual fallback for agents
  without review-runner adapters
- `cmd/entire/cli/review/prompt.go` / `scope.go` / `run.go` / `dump.go` /
  `run_multi.go` — core machinery
- `cmd/entire/cli/review/tui_sink.go` / `tui_model.go` / `tui_detail.go` — TUI
- `cmd/entire/cli/review/types/{reviewer,sink,template}.go` — interface contracts
- `cmd/entire/cli/review/env.go` — `ENTIRE_REVIEW_*` constants + skills codec
- `cmd/entire/cli/agent/{claudecode,codex,geminicli}/reviewer.go` — per-agent
  `AgentReviewer` implementations
- `cmd/entire/cli/agent/claudecode/models.go` — the only `ModelLister` (real
  Claude aliases)
- `cmd/entire/cli/lifecycle.go` — `adoptReviewEnv` reads `ENTIRE_REVIEW_*`
- `cmd/entire/cli/review_bridge.go` — `launchableReviewerFor`,
  `headHasReviewCheckpoint`
- `cmd/entire/cli/attach.go` — `entire attach --review` (post-hoc tagging;
  consumes a pending-review marker)
- `cmd/entire/cli/settings/settings.go` — `ReviewProfileConfig` (`Agents`,
  `Judges`, `Chair`, legacy `Master`/`MasterAgent`/`MasterModel`)
