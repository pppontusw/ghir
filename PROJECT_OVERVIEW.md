# ghir — Project Overview & Technical Summary

A detailed explanation of what this project is, how it works, its tech stack, and architecture.

---

## 1. What Is This Project?

**ghir** (GitHub Issue Runner) is a **queue-driven CLI tool** that processes GitHub issues (or local markdown task files) one-by-one by invoking an **AI agent CLI** (e.g. Claude, Codex, Gemini, Cursor Agent, or Pi). It is designed to:

- Run inside a **git repository** and hand each issue/task to an agent as a prompt.
- Process issues in a **controlled order** (from a file, CSV, or "all open").
- **Track completion state** per repository so already-done issues are skipped on subsequent runs.
- **Write logs per issue** and support **agent/model overrides** and **custom prompt templates**.
- Optionally open **PRs** via configurable strategies (direct commit, PR-per-pass, PR-chain, PR-at-end).
- Run **continuous improvement passes** (`ghir improve`) for background refactoring without tying work to specific issues.
- Provide a **terminal UI** (`ghir tui`) for guided configuration with full command transparency.

In short: you give it a list of issues (or a directory of `.md` files), and it runs your chosen agent on each item in sequence, commits the agent's work, and marks items complete—with retries and session-limit handling where supported.

---

## 2. High-Level How It Works

1. **Discovery**
   You run `ghir` from the root of a git repo. It finds the repo root via `git rev-parse --show-toplevel`.

2. **Issue/task source**
   The list of work items comes from one of:
   - **Issues file**: `.ticket-runner/issues.txt` (one issue ID per line).
   - **Comma-separated**: `--issues 1721,1706`.
   - **Single issue**: `--issue 1710`.
   - **All open issues**: `--all-open` (optionally `--label <label>`).
   - **File mode**: `--files path/to/a.md,path/to/b.md` or `--all-files <dir>` (all `.md` in a directory).

3. **Preflight**
   It checks that required binaries exist: `git`, `gh` (unless in file-only mode with direct strategy), and the selected agent binary (e.g. `claude`, `codex`, `gemini`, `cursor-agent`, `pi`).

4. **Completion state**
   Completion is stored in a file (default: `.ticket-runs/.completed`). Each line is one completed issue ID or file path. Already-completed items are skipped unless you pass `--force` or `--issue <id>`.

5. **Per-item processing**
   For each pending item:
   - **Fetch details**: For GitHub issues, `gh issue view <id> --json title,body`. For file mode, read the `.md` file and derive title/body (e.g. first `# ` line as title).
   - **Build prompt**: From a template (default in-code or `.ticket-runner/prompt.tmpl`) with placeholders like `{{ISSUE_NUMBER}}`, `{{ISSUE_TITLE}}`, `{{ISSUE_BODY}}`, `{{FILE_PATH}}`.
   - **Enforce clean tree**: Refuse to run if there are uncommitted changes (safety).
   - **Run agent**: Invoke the agent CLI with the prompt; stdout/stderr are teed to a log file (e.g. `.ticket-runs/<issue>.log`) and optionally to the console with "pretty" or "raw" streaming.
   - **Post-run**:
     - If the agent **committed** (HEAD changed), or if there are **uncommitted changes**, the runner may commit them with a conventional message and then mark the item completed.
     - If the log indicates a **session/usage limit** (Claude, Codex, Gemini), the tool can **wait** until the reported reset time (plus a buffer) and **retry** the same issue.
     - On **non-retryable failure**, processing stops (unless `--continue-on-error`).

6. **Optional loop**
   With `--loop` and `--all-open` or `--all-files`, ghir can run indefinitely, processing open issues or new `.md` files, then sleeping (e.g. 5 minutes) and repeating. Loop is only supported with `--strategy direct`.

---

## 3. Tech Stack

| Layer | Technology |
|-------|------------|
| **Language** | Go 1.22+ |
| **Module** | Single module `ghir` (see `go.mod`); two direct dependencies for TUI: `charmbracelet/bubbletea` and `charmbracelet/lipgloss`. Core CLI has no third-party deps. |
| **Build** | `make build` / `make install`; binary name is configurable (default install name: `ghir`). |
| **External tools** | **git** (repo root, status, commit), **gh** (GitHub CLI for issues/PRs; not used in file-only mode with direct strategy), and **one agent CLI** in `PATH`: `claude`, `codex`, `gemini`, `cursor-agent`, or `pi`. |
| **State** | File-based: issues file, completion file (`.completed`), log directory (`.ticket-runs/`), optional prompt template. |
| **Platform** | POSIX-style environment (Linux, macOS). |

There is no database, no daemon, and no network code inside ghir itself—only process execution (`exec.Command`) and file I/O. GitHub API access is entirely via the `gh` CLI.

---

## 4. Architecture (Code Structure)

The project is organized as a Go module with four packages: `main`, `ghir/tui`, `ghir/streaming`, and `ghir/defaults`.

### 4.1 Package: `main` (root)

Core CLI, runner, agents, strategies, improve mode.

| File | Responsibility |
|------|---------------|
| `main.go` | Entry point: dispatch to issue/file run, improve, or TUI mode. |
| `args.go` | CLI argument parsing (`parseArgs`, `parseImproveArgs`, `parseTUIArgs`), usage/help text. |
| `agent.go` | `agentConfig` type, `buildAgentCommand` per agent, default models, agent/stream-view validation. |
| `runner.go` | `runner` struct and per-issue processing: fetch details, build prompt, invoke agent, handle commits, retries, labels, banner output. |
| `strategy.go` | PR strategy implementations: `runOnceDirect`, `runOncePRPerPass`, `runOncePRChain`, `runOncePRAtEnd`. Branch creation, push, `gh pr create`. |
| `improve.go` | `ghir improve` entry point: `runImprove`, improve strategies (direct/PR-per-pass/PR-chain/PR-at-end), prompt building, pass loop. |
| `improve_prompts.go` | Built-in improve mode prompt templates (cleanup, quality, refactor, security, bugfix, dead-code, docs, tests, deps, perf, a11y, errors, types, logging, mixed). Custom prompt loading. |
| `session.go` | Session/usage-limit detection regexes (Claude, Codex, Gemini), wait-duration parsing, countdown, internal-server-error retry logic. |
| `issues.go` | Issue list loading: `loadIssues`, `readIssuesFile`, `fetchOpenIssues`, `loadFilePaths`, `loadAllFiles`. |
| `repo.go` | Git helpers: `findRepoRoot`, `workingTreeDirty`, `headSHA`, `commitCoAuthorSuffix`, path resolution for repo-relative defaults. |
| `prompts.go` | Default prompt templates for issue mode and file mode. |
| `stream.go` | `consoleStreamWriter`: line-buffered writer that routes agent output through a `streaming.Renderer` to the console. |
| `tui.go` | `ghir tui` entry point: parse TUI args, launch Bubble Tea program. |

### 4.2 Package: `ghir/tui`

Terminal UI built on Bubble Tea. Three-phase state machine: **Configure** → **Run** → **Summary**.

| File | Responsibility |
|------|---------------|
| `model.go` | Top-level Bubble Tea model, phase routing, key dispatch, view composition. |
| `configure.go` | Configure phase: form fields for workflow/source/agent/model/strategy/runtime, field update/validation, view rendering. |
| `run.go` | Run phase: subprocess monitoring, stream output parsing, retry/session-wait detection, progress display. |
| `summary.go` | Summary phase: succeeded/failed item lists, rerun/reset actions, return-to-configure. |
| `command_builder.go` | Converts TUI form state (`CommandState`) into CLI `[]string` args and back into `options`/`improveOptions`. |
| `command_rail.go` | Renders the always-visible command rail showing the exact `ghir` invocation. |
| `validator.go` | Validates form state, prevents invalid flag combinations, shared enum constants. |
| `preflight.go` | Pre-run checks: git repo, required binaries, clean working tree. |
| `presets.go` | Save/load named presets to `~/.ticket-runner/tui-presets.json`. |
| `queue.go` | Run-scoped queue staging: reorder, remove, add items before execution. |
| `keyboard.go` | Keybinding definitions. |
| `styles.go` | Lipgloss style definitions. |
| `log_access.go` | Open log files from run phase. |
| `run_process.go` | Subprocess execution wrapper. |

### 4.3 Package: `ghir/streaming`

| File | Responsibility |
|------|---------------|
| `render.go` | Stream renderers: `codexPrettyRenderer`, `geminiPrettyRenderer`, `cursorAgentPrettyRenderer`, `rawRenderer`. Factory function `NewRenderer`. Suppresses noisy agent output lines (YOLO, credentials). |

### 4.4 Package: `ghir/defaults`

| File | Responsibility |
|------|---------------|
| `defaults.go` | Shared path constants (`IssuesFile`, `PromptTemplate`, `LogDir`, `DoneFileName`) used by both the main runner and TUI. |

### 4.5 Main Flow (entry point)

1. **`main()`**
   Parses args → detects subcommand (`tui`, `improve`, or default issue/file run) → finds repo root → applies repo-relative defaults for issues file, log dir, done file, prompt template → constructs a `runner` → runs preflight checks.
   If `--reset`: handle reset and exit.
   Otherwise: run the main loop once (or repeatedly if `--loop`).

2. **`runOnce*(r, issues)`**
   Dispatches to the selected strategy: `runOnceDirect`, `runOncePRPerPass`, `runOncePRChain`, or `runOncePRAtEnd`. Each strategy handles branching/PR creation around the core per-issue processing.

3. **`processIssue(idx, total, issue, isResume)`**
   Fetch details → dry-run check → skip-if-completed → set label → clean-tree check → record HEAD → build prompt → run agent → retries → session-limit handling → commit → mark completed.

### 4.6 Agent invocation and streaming

- **`buildAgentCommand(prompt)`** — Builds `exec.Cmd` per agent, e.g.:
  - **claude**: `claude --print --verbose --output-format text --dangerously-skip-permissions [--model ...]` with prompt on stdin.
  - **codex**: `codex exec --json --dangerously-bypass-approvals-and-sandbox [--model ...] <prompt>`.
  - **gemini**: `gemini --output-format json --yolo [-m model] -p <prompt>`.
  - **cursor-agent**: `cursor-agent --print --output-format json --force [--model ...] <prompt>`.
  - **pi**: `pi -p [--model ...] <prompt>`.

- **Stream views** — `--stream-view pretty` (default) or `raw`. For Codex, Cursor Agent, and Gemini, "pretty" uses a line-by-line JSON renderer to show condensed events. Other agents fall back to raw with a notice.

### 4.7 Session limit and error detection

- **Regexes** — Various patterns for Claude ("usage limit", "resets at …"), Codex (`resets_at`, `resets_in_seconds`, `usage_limit_reached`), Gemini (quota/capacity/429, duration string).
- **`detectSessionLimit`** — True if the log (and optionally exit code) indicates a retryable limit; for `cursor-agent` and `pi` the tool does not treat quota/session failures as retryable.
- **`waitDuration*`** — Parse reset time or duration from log and return (waitSeconds, resetTime); add `--wait-buffer-sec` to the wait.
- **Transient agent retries** — Internal server errors (500/502/503/504, "overloaded") and expired auth-token failures (for example `401 IDE token expired: unauthorized: token expired`) trigger up to 3 retries with delay.

### 4.8 Strategies

All four strategies are available for both normal issue/file runs and improve mode:

- **`direct`** (default): Run on the current branch, commit locally.
- **`pr-per-pass`**: Create a unique feature branch per issue/pass, push, open a PR via `gh pr create`.
- **`pr-chain`**: Create a chain of PRs where each targets the previous PR's branch.
- **`pr-at-end`**: Run the full queue on one feature branch, open a single PR at the end.

### 4.9 Continuous improvement mode

`ghir improve` runs improvement passes independent of specific issues:

- 15 built-in modes (cleanup, quality, refactor, security, bugfix, dead-code, docs, tests, deps, perf, a11y, errors, types, logging, mixed) with detailed prompt templates.
- Comma-separated mode lists (`--mode cleanup,refactor`) randomly pick one mode per pass.
- Custom prompt support via `--prompt <text>` or `--prompt-file <path>`.
- `--scope <path>` to limit improvements to a subdirectory.
- `--iterations N` and `--loop` for pass control.

### 4.10 Terminal UI

`ghir tui` provides guided configuration via a Bubble Tea application:

- **Configure phase**: Choose workflow (issues/files/improve), set source, agent, model, strategy, and runtime options. Real-time validation prevents invalid combinations.
- **Run phase**: Monitor queue progress, stream output, retries, session waits, and log paths.
- **Summary phase**: Inspect results, rerun failures, reset completion markers, or return to configure.
- **Command rail**: Always-visible display of the exact `ghir` CLI invocation being built.
- **Presets**: Save and load named configurations to `~/.ticket-runner/tui-presets.json`.

---

## 5. Configuration and Usage

### 5.1 Repository layout (conventions)

- **Issues list**: `.ticket-runner/issues.txt` (one numeric issue ID per line; `#` comments allowed).
- **Optional prompt template**: `.ticket-runner/prompt.tmpl` with `{{ISSUE_NUMBER}}`, `{{ISSUE_TITLE}}`, `{{ISSUE_BODY}}`, `{{FILE_PATH}}`.
- **State**: `.ticket-runs/` (log directory), `.ticket-runs/.completed` (completion list).

All of these paths can be overridden via flags (`--issues-file`, `--prompt-template`, `--log-dir`, `--done-file`).

### 5.2 Common commands (from README)

- `ghir --dry-run` — Show what would run.
- `ghir` — Process queue (issues file or default source).
- `ghir --status` — Show completion status.
- `ghir --issues 1721,1706` — Process specific issues (no file).
- `ghir --issue 1710` — Process one issue (forced re-run).
- `ghir --force` — Reprocess completed issues.
- `ghir --reset` / `ghir --reset 1710` — Clear completion state (all or one).
- `ghir --agent codex --stream-view pretty` — Choose agent and stream view.
- `ghir --loop` with `--all-open` or `--all-files` — Continuous run (direct strategy only).
- `ghir --all-open --strategy pr-per-pass` — One PR per issue.
- `ghir --issues 1,2 --strategy pr-at-end` — Single PR after full queue.
- `ghir improve --mode cleanup --iterations 1` — One cleanup pass.
- `ghir improve --mode cleanup,refactor --iterations 5` — Random mode rotation.
- `ghir improve --prompt "Fix flaky tests." --iterations 1` — Custom prompt.
- `ghir tui` — Launch terminal UI.

### 5.3 Default prompts

- **GitHub issues**: Instructs the agent to implement the issue, run checks/tests, and commit with "fix:" or "feat:" and "Closes #{{ISSUE_NUMBER}}"; no push.
- **File mode**: Same idea but keyed by `{{FILE_PATH}}` and no "Closes #" in the default template.

---

## 6. Safety and Failure Behavior

- **Must run inside a git repository** — Exits with error otherwise.
- **Clean working tree** — Before each issue, the working tree must be clean (no uncommitted changes); otherwise the run is aborted with a hint to commit or stash.
- **Stop on first non-retryable failure** — Unless `--continue-on-error` is set.
- **Retries**:
  - **Session/usage limits** (Claude, Codex, Gemini): wait until reset time + buffer, then retry the same issue.
  - **Transient agent failures**: internal server errors and expired auth-token failures are retried up to 3 times with 5-second delay.
- **cursor-agent** — Monthly quota/resource exhaustion is treated as **non-retryable** (no automatic wait/retry).
- **pi** — Session/quota failures are currently treated as **non-retryable** (no automatic wait/retry), but transient expired-token auth failures are retried automatically.
- **PR-based strategies** require a working `origin` remote and authenticated `gh` CLI.
- **`--loop`** is only supported with `--strategy direct` for normal issue/file runs.

---

## 7. Development and Testing

- **Makefile** — `make help`, `make build`, `make install`, `make run ARGS="..."`, `make q` (fmt+lint+typecheck+build), `make t` (tests). Version/commit/date are injected via `-ldflags` into `main.version`, `main.commit`, `main.date`. Linting uses `staticcheck` with unused-code checks.
- **Tests** — Spread across both `package main` and `package tui`:
  - **`main_test.go`** — Unit tests for `parseArgs`, path resolution, issue parsing, prompt building, session-limit detection, wait duration parsing, improve mode, strategy validation.
  - **`preflight_test.go`** — `checkBinary`, `preflightChecks` (including file mode vs issue mode and missing `gh`).
  - **`hints_test.go`** — Error messages and hints (e.g. missing issues file, `gh auth login`).
  - **`smoke_test.go`** — Integration smoke: temp git repo, stub `gh` and agent binary, run ghir and assert completion and commit. Covers multiple strategies.
  - **`agent_test.go`** — Agent command building and default model tests.
  - **`runner_loading_test.go`** — Runner loading and initialization tests.
  - **`validation_parity_test.go`** — Ensures TUI validation rules match CLI parser logic.
  - **`command_builder_parity_test.go`** — Ensures TUI-built commands round-trip correctly through CLI parsing.
  - **`tui/model_test.go`** — TUI model state transitions, phase routing, run monitoring.
  - **`tui/command_builder_test.go`** — TUI form state to CLI args conversion.
  - **`tui/validator_test.go`** — TUI form validation rules.
  - **`tui/preflight_test.go`** — TUI preflight checks.
  - **`tui/presets_test.go`** — Preset save/load/merge.
  - **`tui/queue_test.go`** — Queue staging operations.
  - **`tui/headless_flow_test.go`** — Headless TUI execution flow.

---

## 8. File Structure (summary)

```
ghir/
├── main.go                  # Entry point: dispatch to run/improve/TUI
├── args.go                  # CLI argument parsing and usage text
├── agent.go                 # Agent config, command building, defaults
├── runner.go                # Per-issue processing, prompts, commits, labels
├── strategy.go              # PR strategy implementations (direct, pr-per-pass, pr-chain, pr-at-end)
├── improve.go               # ghir improve mode: pass loop, strategies, prompt building
├── improve_prompts.go       # Built-in improve mode prompt templates (15 modes)
├── session.go               # Session-limit detection, wait/retry, internal server error retry
├── issues.go                # Issue/file list loading
├── repo.go                  # Git helpers, path resolution, co-author constant
├── prompts.go               # Default issue/file prompt templates
├── stream.go                # Console stream writer (line-buffered renderer bridge)
├── tui.go                   # TUI entry point
├── tui/                     # Terminal UI package (Bubble Tea)
│   ├── model.go             # Top-level model, phase routing
│   ├── configure.go         # Configure phase forms
│   ├── run.go               # Run phase monitoring
│   ├── summary.go           # Summary phase
│   ├── command_builder.go   # Form state ↔ CLI args
│   ├── command_rail.go      # Command display rail
│   ├── validator.go         # Form validation, shared enums
│   ├── preflight.go         # Pre-run checks
│   ├── presets.go           # Preset save/load
│   ├── queue.go             # Queue staging
│   ├── keyboard.go          # Keybindings
│   ├── styles.go            # Lipgloss styles
│   ├── log_access.go        # Log file opener
│   └── run_process.go       # Subprocess wrapper
├── streaming/
│   └── render.go            # Stream renderers (pretty/raw per agent)
├── defaults/
│   └── defaults.go          # Shared path constants
├── *_test.go                # Tests (main package)
├── tui/*_test.go            # Tests (TUI package)
├── go.mod                   # Module: ghir, Go 1.22
├── go.sum                   # Dependency checksums
├── Makefile                 # Build, install, lint, test targets
├── README.md                # User-facing documentation
├── PROJECT_OVERVIEW.md      # This document
├── docs/TUI_SPEC.md         # TUI design specification
├── LICENSE                  # MIT
└── tasks/                   # Example local task .md files
```

When installed, the binary is typically `ghir` (or the name set by `INSTALL_NAME` in the Makefile).

---

## 9. Summary

| Aspect | Summary |
|--------|---------|
| **What** | Queue-driven GitHub issue (or local .md task) runner that invokes an AI agent CLI per item, commits results, and tracks completion. Also supports continuous improvement passes and a terminal UI. |
| **How** | Load list of issues/files → for each pending item: fetch details, build prompt, run agent, handle commits and session limits, mark complete. Strategy layer handles branching and PRs. |
| **Tech** | Go 1.22; `charmbracelet/bubbletea` + `lipgloss` for TUI; depends on `git`, `gh`, and one of `claude`/`codex`/`gemini`/`cursor-agent`/`pi`. |
| **State** | File-based: `.ticket-runner/issues.txt`, `.ticket-runs/.completed`, `.ticket-runs/<id>.log`, optional `.ticket-runner/prompt.tmpl`. |
| **Safety** | Requires git repo and clean tree per run; optional retries for session limits and 5xx; can commit partial work on limit. |
| **Extensibility** | Custom prompt template, agent/model override, file mode (no GitHub), strategies (direct/pr-per-pass/pr-chain/pr-at-end), `ghir improve` with 15 built-in modes + custom prompts, terminal UI with presets, `--continue-on-error`, `--loop`. |

This document reflects the behavior and structure of the **ghir** codebase as of the current implementation.
