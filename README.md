# ghir

Queue-driven GitHub issue runner for agent CLIs (`claude`, `codex`, `gemini`, `cursor-agent`).

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

### 2) Configure a target repository

In the repo where you want to run tickets, create `.ticket-runner/issues.txt`:

```text
# one issue id per line (processing order)
1721
1706
1710
```

Optional prompt override: `.ticket-runner/prompt.tmpl`.

Template placeholders:
- `{{ISSUE_NUMBER}}`
- `{{ISSUE_TITLE}}`
- `{{ISSUE_BODY}}`

### 3) First run

```bash
cd /path/to/target-repo
ghir --dry-run
ghir
```

## Common Commands

```bash
# Show queue state
ghir --status

# Process specific issues without creating issues.txt
ghir --issues 1721,1706

# Process one issue (forced re-run of that issue)
ghir --issue 1710

# Reprocess already completed issues
ghir --force

# Control live console rendering
ghir --agent codex --stream-view pretty   # default
ghir --stream-view raw

# Reset completion state
ghir --reset
ghir --reset 1710
```

## Agent and Model Selection

`--agent` supports:
- `claude` (default)
- `codex`
- `gemini`
- `cursor-agent`

Use `--model` to override model per run:

```bash
ghir --agent claude --model sonnet --issues 1721,1706
ghir --agent codex --model gpt-5.3-codex --issues 1721,1706
ghir --agent gemini --model gemini-3-pro-preview --issues 1721,1706
ghir --agent cursor-agent --model auto --issues 1721,1706
```

Flag mapping:
- Claude: `--model`
- Codex: `--model`
- Gemini: `-m`
- Cursor Agent: `--model`

Streaming view:
- `--stream-view pretty` (default): condensed event rendering for Codex JSON output.
- `--stream-view raw`: passthrough raw agent output to console.
- For non-Codex agents, `pretty` currently falls back to raw passthrough with a notice.

## State and Logs

For each target repository:

- Logs: `.ticket-runs/<issue>.log`
- Completion file: `.ticket-runs/.completed`

This means progress is isolated per repo.

## Safety and Failure Behavior

- Must run inside a git repository.
- Requires clean working tree before processing each issue.
- Stops on first non-retryable failure.
- Retries with wait on session/usage limits for:
  - `claude`
  - `codex`
  - `gemini`
- `cursor-agent` monthly quota/resource exhaustion is treated as non-retryable.

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
