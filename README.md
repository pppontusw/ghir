# ghir

Queue-driven GitHub issue runner for agent CLIs (`claude`, `codex`, `gemini`, `cursor-agent`, `pi`).

It processes issues one-by-one in a controlled order, stores completion state per repository, writes logs per issue, and supports agent/model overrides.

## Prerequisites

- Go 1.22+
- `git`
- `gh` (authenticated with access to your repo/issues)
- At least one agent CLI in `PATH`:
  - `claude`
  - `codex`
  - `gemini`
  - `cursor-agent`
  - `pi`

## Quick Start

### 1) Install the binary

From this repo:

```bash
make install
```

By default this installs to `~/.local/bin/ghir`.

If `ghir` is not found, add this to `~/.zshrc`:

```bash
export PATH="$HOME/.local/bin:$PATH"
```

Then reload shell:

```bash
source ~/.zshrc
```

Verify:

```bash
ghir --help
```

### 2) Pick a workflow and run

`ghir` runs from **inside** the git repository you want to change. You can feed it work in several ways:

#### Pass issue numbers directly

The quickest way to get started — no config files needed:

```bash
# One issue
ghir --issue 1721

# Multiple issues, comma-separated
ghir --issues 1721,1706,1710
```

#### Process all open issues

Point ghir at the repo's open issue backlog:

```bash
# All open issues
ghir --all-open

# Only issues with a specific label
ghir --all-open --label bug

# Loop forever, picking up new issues as they're filed
ghir --all-open --loop
```

#### Use an issues file

For a persistent, ordered queue, create `.ticket-runner/issues.txt` in the repo root:

```text
# one issue id per line (processing order)
1721
1706
1710
```

Then run:

```bash
ghir                # processes the file by default
ghir --dry-run      # preview without running
```

You can also point at an explicit file:

```bash
ghir --issues-file my-issues.txt
```

#### Run from markdown files (no GitHub issues needed)

Put numbered `.md` files in a directory — the filename (or numeric prefix) determines processing order:

```text
tasks/
  1-fix-auth.md
  2-add-logging.md
  3-refactor-api.md
```

Then run:

```bash
# All .md files in the directory, sorted numerically
ghir --all-files tasks

# Specific files
ghir --files tasks/1-fix-auth.md,tasks/2-add-logging.md
```

This is useful when you want to batch independent tasks that aren't tied to GitHub issues — each file becomes its own work item with the file contents used as the prompt.

#### Custom prompt template

For any issue-based workflow, you can override the default prompt with `.ticket-runner/prompt.tmpl`:

```text
Fix issue #{{ISSUE_NUMBER}}: {{ISSUE_TITLE}}

{{ISSUE_BODY}}
```

Or point to an explicit template:

```bash
ghir --prompt-template my-prompt.tmpl --issues 1721
```

Template placeholders: `{{ISSUE_NUMBER}}`, `{{ISSUE_TITLE}}`, `{{ISSUE_BODY}}`.

## Common Commands

```bash
# Show queue state (works with any source)
ghir --status

# Reprocess already-completed items
ghir --force

# Reset completion state
ghir --reset          # reset all
ghir --reset 1710    # reset one

# Keep going when an item fails
ghir --issues 1721,1706,1710 --continue-on-error

# Control live console rendering
ghir --stream-view pretty    # condensed events (default)
ghir --stream-view raw       # passthrough output

# Publish one PR per issue or file
ghir --all-open --strategy pr-per-pass
ghir --all-files tasks --strategy pr-per-pass

# Create one PR after the full queue
ghir --issues 1721,1706 --strategy pr-at-end

# Chain PRs (each targets the previous branch)
ghir --issues 1721,1706,1710 --strategy pr-chain
```

## Terminal UI (`ghir tui`)

Use the TUI when you want guided configuration with command transparency instead of manually composing flags.

```bash
# Open the TUI directly
ghir tui

# Start on a specific workflow
ghir tui --mode files

# Load a saved preset at startup
ghir tui --load-preset daily

# Force plain-text rendering
ghir tui --no-color
```

The TUI stays thin over the existing CLI. The command rail always shows the exact `ghir` invocation that will be executed, and runs still go through the normal subprocess path.

Phases:
- `Configure`: choose workflow, source, agent, model, and run.
- `Run`: monitor queue status, stream output, retries, session waits, and log paths.
- `Summary`: inspect succeeded/failed items, rerun failures, reset completion markers, or return to configure with state preserved.

Workflow coverage:
- `Issues`: `--issue`, `--issues`, `--issues-file`, `--all-open`, optional `--label`, `--strategy`.
- `Files`: `--files`, `--all-files`, `--strategy`.
- `Improve`: `ghir improve` mode/strategy/iterations/loop/scope controls.

Keybindings:
- `J/K` or arrow keys: move within the active pane.
- `Enter`: edit or apply the focused item.
- `Space`: toggle booleans in configure.
- `R`: run from configure, or rerun failed subset from summary.
- `C`: copy the full command rail invocation.
- `S`: save the current setup as a preset.
- `L`: load a saved preset.
- `O`: open the selected log path during a run.
- `/`: search the failed-items list during summary or the active queue during a run.
- `?`: show keyboard help.
- `Q`: quit. During a run it requests a graceful stop first.

Limits and behavior:
- `NO_COLOR=1` or `ghir tui --no-color` keeps the UI readable without ANSI styling.
- TUI presets are stored by default in `~/.ticket-runner/tui-presets.json`.
- If no user-level preset file exists, the TUI still loads the legacy repo-local `.ticket-runner/tui-presets.json` if present.
- Improve runs do not support summary actions that rerun only failed passes or reset completion markers.
- The legacy `--experimental` flag is still accepted, but it is no longer required.

## Agent and Model Selection

`--agent` supports:
- `claude` (default)
- `codex`
- `gemini`
- `cursor-agent`
- `pi`

If `--model` is omitted, ghir applies built-in defaults by agent:
- `claude`: `opus`
- `codex`: `gpt-5.4`
- `gemini`: `gemini-3.1-pro-preview`
- `cursor-agent`: `auto`
- `pi`: `github-copilot/gpt-5.4:high`

Use `--model` to override model per run:

```bash
ghir --agent claude --model sonnet --issues 1721,1706
ghir --agent codex --model gpt-5.3-codex --issues 1721,1706
ghir --agent gemini --model gemini-3-flash-preview --issues 1721,1706
ghir --agent cursor-agent --model opus-4.6-thinking --issues 1721,1706
ghir --agent pi --model github-copilot/gpt-5.4:high --issues 1721,1706
```

*Note: Maps to `--model` natively for Claude, Codex, Cursor Agent, and pi, and `-m` for Gemini. pi runs in non-interactive mode via `pi -p` for raw output and `pi --mode json` for pretty streaming.*

Streaming view:
- `--stream-view pretty` (default): condensed event rendering for Codex, cursor-agent, Gemini, and pi JSON output.
- `--stream-view raw`: passthrough raw agent output to console.
- For other agents, `pretty` falls back to raw passthrough with a notice.

## State and Logs

For each target repository:

- Logs: `.ticket-runs/<issue>.log`
- Completion file: `.ticket-runs/.completed`

This means progress is isolated per repo.

## Normal Run Strategies

Standard issue/file runs support the same strategy flag as improve mode:

- `--strategy direct` (default): run on the current branch and commit locally.
- `--strategy pr-per-pass`: create one feature branch and PR per issue/file item.
- `--strategy pr-chain`: create a stack of PRs, where each later PR targets the previous PR branch.
- `--strategy pr-at-end`: run the full issue/file queue on one feature branch and open a single PR at the end.

Notes:

- `--loop` is only supported with `--strategy direct` for normal issue/file runs.
- File workflows only require `gh` when you choose a PR-based strategy.
- PR-based strategies require a working `origin` remote and authenticated `gh` CLI.

## Continuous Improvement Mode (`ghir improve`)

When you have spare tokens or want background refactors, you can run **continuous improvement passes** that let your agent clean up the repo without tying work to specific issues.

Basic usage:

```bash
# One cleanup pass on the current branch (direct commits)
ghir improve --mode cleanup --iterations 1

# One custom inline improve prompt
ghir improve --prompt "Audit the repo for flaky tests and fix one high-confidence case." --iterations 1

# Load a custom improve prompt from a file
ghir improve --prompt-file prompts/improve.txt --strategy pr-per-pass

# Security hardening pass, looping until you stop it (Ctrl+C)
ghir improve --mode security --loop

# Randomly choose between cleanup and refactor on each pass
ghir improve --mode cleanup,refactor --iterations 5

# Dead-code removal pass focused on a subdirectory
ghir improve --mode dead-code --scope backend/
```

Modes:

- `cleanup`: Light-touch polish: readability, style consistency, small simplifications.
- `quality`: Reduce one meaningful suppression or disabled-check cluster per pass, fixing all surfaced issues until the repo is green again.
- `refactor`: Structural changes: extract functions/classes, move code, improve interfaces and abstractions.
- `security`: Scans for and fixes obvious security issues and misconfigurations.
- `bugfix`: Prioritizes one coherent fix per pass: failing bugs first, then high-confidence defects, then small stability hardening if no clearer bug exists.
- `dead-code`: Finds and removes unused code, configuration, and assets (conservatively).
- `docs`: Adds or improves README, docstrings, API docs, and inline comments.
- `tests`: Adds unit tests, edge cases, and integration tests where valuable.
- `deps`: Updates dependencies, removes unused ones, fixes deprecation warnings.
- `perf`: Finds and fixes obvious performance inefficiencies.
- `a11y`: Improves accessibility in web UIs (ARIA, keyboard nav, focus, contrast).
- `errors`: Improves error messages, logging, and handling of failure paths.
- `types`: Adds types, fixes type errors, satisfies stricter linters.
- `logging`: Adds structured logging, metrics, and tracing where helpful.
- `mixed`: Rotates through all modes each pass.
- `cleanup,refactor,...`: Randomly picks one of the listed built-in modes on each pass. Repeats are allowed. `mixed` cannot be combined with other modes.

Strategies:

- Direct commits (default):
  - `--strategy direct`
  - Runs on the current branch.
  - Each pass creates a commit like `chore: cleanup pass 1`.

- PR-per-pass:
  - `--strategy pr-per-pass`
  - Detects the current branch, creates a unique feature branch per pass (e.g. `ghir/improve-cleanup-1-a3f7b2c1`), runs the improvement there, pushes it, and opens a PR via `gh pr create`.
  - Returns to your starting branch before moving on.
- PR-chain:
  - `--strategy pr-chain`
  - Creates a chain of PRs. The first pass branches from your current branch and opens a PR against the default branch. Each subsequent pass branches from the previous pass's branch and opens a PR against it.
- PR-at-end:
  - `--strategy pr-at-end`
  - Branches from your current branch, applies all improvement passes directly to the single new branch, and opens a single PR against the default branch at the end containing all iterations.

Other useful flags:

- `--prompt <text>`: Use inline custom improve prompt text instead of a built-in `--mode`.
- `--prompt-file <path>`: Load custom improve prompt text from a file instead of a built-in `--mode`.
- `--iterations <N>`: Number of passes to run (default `1`).
- `--loop`: Run passes continuously until interrupted (when combined with `--iterations 0`, it runs indefinitely).
- `--agent`, `--model`, `--stream-view`, `--no-color`, `--wait-buffer-sec`, and `--*-bin` flags work the same as in normal `ghir` runs.

Notes:

- `--prompt` and `--prompt-file` are mutually exclusive.
- Custom prompt runs use `custom` for improve branch/log/commit/PR labeling.
- Custom prompts replace built-in prompt selection, so they cannot be combined with `--mode`.
- Comma-separated `--mode` lists are CLI-only; TUI improve mode selection remains single-select.

Safety:

- Requires a clean working tree before starting (just like normal `ghir`).
- Reuses the same agent invocation, streaming, and session-limit handling as issue mode.
- For `pr-per-pass`, `pr-chain`, and `pr-at-end`, you must have a working `origin` remote and authenticated `gh` CLI.

## Safety and Failure Behavior

- Must run inside a git repository.
- Requires clean working tree before processing each issue.
- Stops on first non-retryable failure.
- Retries with wait on session/usage limits for:
  - `claude`
  - `codex`
  - `gemini`
- `cursor-agent` monthly quota/resource exhaustion is treated as non-retryable.
- `pi` session/quota failures are currently treated as non-retryable.
- Transient auth refresh failures such as `401 IDE token expired: unauthorized: token expired` are retried automatically.

## Development Commands

```bash
make help
make build
make install
make run ARGS="--help"
```

## Troubleshooting

- `ghir: command not found`
  - Ensure `~/.local/bin` is in `PATH`.
- `gh issue view ...` failures
  - Run `gh auth status` and confirm repo access.
- `ERROR: uncommitted changes detected`
  - Commit or stash local changes before running.

## License

MIT, see `LICENSE`.
