# ghir — Project Overview & Technical Summary

A detailed explanation of what this project is, how it works, its tech stack, and architecture.

---

## 1. What Is This Project?

**ghir** (GitHub Issue Runner) is a **queue-driven CLI tool** that processes GitHub issues (or local markdown task files) one-by-one by invoking an **AI agent CLI** (e.g. Claude, Codex, Gemini, or Cursor Agent). It is designed to:

- Run inside a **git repository** and hand each issue/task to an agent as a prompt.
- Process issues in a **controlled order** (from a file, CSV, or “all open”).
- **Track completion state** per repository so already-done issues are skipped on subsequent runs.
- **Write logs per issue** and support **agent/model overrides** and **custom prompt templates**.

In short: you give it a list of issues (or a directory of `.md` files), and it runs your chosen agent on each item in sequence, commits the agent’s work, and marks items complete—with retries and session-limit handling where supported.

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
   It checks that required binaries exist: `git`, `gh` (unless in file-only mode), and the selected agent binary (e.g. `claude`, `codex`, `gemini`, `cursor-agent`).

4. **Completion state**  
   Completion is stored in a file (default: `.ticket-runs/.completed`). Each line is one completed issue ID or file path. Already-completed items are skipped unless you pass `--force` or `--issue <id>`.

5. **Per-item processing**  
   For each pending item:
   - **Fetch details**: For GitHub issues, `gh issue view <id> --json title,body`. For file mode, read the `.md` file and derive title/body (e.g. first `# ` line as title).
   - **Build prompt**: From a template (default in-code or `.ticket-runner/prompt.tmpl`) with placeholders like `{{ISSUE_NUMBER}}`, `{{ISSUE_TITLE}}`, `{{ISSUE_BODY}}`, `{{FILE_PATH}}`.
   - **Enforce clean tree**: Refuse to run if there are uncommitted changes (safety).
   - **Run agent**: Invoke the agent CLI with the prompt; stdout/stderr are teed to a log file (e.g. `.ticket-runs/<issue>.log`) and optionally to the console with “pretty” or “raw” streaming.
   - **Post-run**:
     - If the agent **committed** (HEAD changed), or if there are **uncommitted changes**, the runner may commit them with a conventional message and then mark the item completed.
     - If the log indicates a **session/usage limit** (Claude, Codex, Gemini), the tool can **wait** until the reported reset time (plus a buffer) and **retry** the same issue.
     - On **non-retryable failure**, processing stops (unless `--continue-on-error`).

6. **Optional loop**  
   With `--loop` and `--all-open` or `--all-files`, ghir can run indefinitely, processing open issues or new `.md` files, then sleeping (e.g. 5 minutes) and repeating.

---

## 3. Tech Stack

| Layer | Technology |
|-------|------------|
| **Language** | Go 1.22+ |
| **Module** | Single module `ghir` (see `go.mod`); **no third-party dependencies** — only standard library. |
| **Build** | `make build` / `make install`; binary name is configurable (default install name: `ghir`). |
| **External tools** | **git** (repo root, status, commit), **gh** (GitHub CLI for issues; not used in file-only mode), and **one agent CLI** in `PATH`: `claude`, `codex`, `gemini`, or `cursor-agent`. |
| **State** | File-based: issues file, completion file (`.completed`), log directory (`.ticket-runs/`), optional prompt template. |
| **Platform** | POSIX-style environment (Linux, macOS, etc.); shell scripts (e.g. `sync-to-public.sh`) use Bash. |

There is no database, no daemon, and no network code inside ghir itself—only process execution (`exec.Command`) and file I/O. GitHub API access is entirely via the `gh` CLI.

---

## 4. Architecture (Code Structure)

The project is a **single-package Go program** (`package main`) in one primary file, `main.go`, plus tests.

### 4.1 Core Types

- **`options`** — All CLI flags: dry-run, issue/file selection, paths (log dir, done file, prompt template), agent/model, binary paths, stream view, colors, `--continue-on-error`, `--loop`, etc.
- **`runner`** — Holds `opts`, `repoRoot`, `fileMode`, `doneFile`, `doneSet` (map of completed IDs), and a `palette` for colored output.
- **`issueDetails`** — Title and body (from GitHub or from file).
- **`issueResult`** — Enum: `resultSuccess`, `resultFailed`, `resultRetry` (used for session-limit retry path).

### 4.2 Main Flow (entrypoint)

1. **`main()`**  
   Parses args → finds repo root → applies repo-relative defaults for issues file, log dir, done file, prompt template → constructs a `runner` → runs preflight checks.  
   If `--reset`: handle reset and exit.  
   Otherwise: run the main loop once (or repeatedly if `--loop`). A single “run” is `runOnce(r)`.

2. **`runOnce(r)`**  
   Loads issue/list via `loadIssues()` → if `--status`, prints status and exits → prints banner → optionally sets GitHub labels `ghir:queued` → for each issue, calls `processIssue()`; on `resultRetry`, retries after session-limit wait; on failure, stops unless `--continue-on-error` → prints summary (succeeded/failed counts).

### 4.3 Issue/task loading

- **`loadIssues()`** — Dispatches to: `loadFilePaths`, `loadAllFiles`, single-issue slice, `fetchOpenIssues`, `parseCSVIssues`, or `readIssuesFile` depending on flags.
- **`readIssuesFile`** — Reads `.ticket-runner/issues.txt` (or path from `--issues-file`); skips empty and `#` lines; validates numeric issue IDs.
- **`fetchOpenIssues`** — Runs `gh issue list --state open --limit 4000 --json number` (and optional `--label`), then sorts numerically.
- **File mode** — `loadFilePaths` / `loadAllFiles` produce a list of paths (e.g. `tasks/1.md`); these are used as “issue” IDs for completion and logging.

### 4.4 Processing a single issue/task

**`processIssue(idx, total, issue, isResume)`**:

1. **Fetch details** — `fetchIssueDetails(issue)`: in file mode reads the `.md` and parses title/body; otherwise `gh issue view <issue> --json title,body`.
2. **Dry run** — If `--dry-run`, print what would run and return success.
3. **Skip if completed** — Unless `--force` or single-issue run, skip when `isCompleted(issue)`.
4. **Set label** — In GitHub mode, set issue label to `ghir:running` (no-op in file mode / dry run).
5. **Clean tree check** — `workingTreeDirty()`; if dirty, error and stop.
6. **Record HEAD** — For later “did the agent commit?” check.
7. **Build prompt** — `buildPrompt(issue, details, isResume)` from template (default or custom) with `{{ISSUE_NUMBER}}`, `{{ISSUE_TITLE}}`, `{{ISSUE_BODY}}`, `{{FILE_PATH}}`.
8. **Run agent** — `runAgent(prompt, logPath)`:
   - Creates log file; builds command via `buildAgentCommand(prompt)` (agent-specific args).
   - Streams output to log and optionally to console via a **stream renderer** (pretty vs raw).
   - Returns exit code and full log content (for session-limit and error detection).
9. **Retries** — Up to 3 retries on **internal server errors** (5xx, overloaded) detected in log output.
10. **Session limit handling** — If `detectSessionLimit(logOutput, agent, exitCode)`:
    - Optionally commit partial work (if tree became dirty).
    - Compute wait time from agent-specific parsing (`waitDurationClaude`, `waitDurationCodex`, `waitDurationGemini`).
    - `waitForSessionReset(waitSeconds, resetTime)` (countdown, then resume).
    - Return `resultRetry` so the caller retries this issue.
11. **Success path** — If exit code 0: compare HEAD to pre-run HEAD; if different, mark completed and set label `ghir:done`. If tree is dirty but HEAD unchanged, do a fallback commit with a conventional message, then mark completed. If no changes at all, still mark completed (“no changes needed”).

### 4.5 Agent invocation and streaming

- **`buildAgentCommand(prompt)`** — Builds `exec.Cmd` per agent, e.g.:
  - **claude**: `claude --print --verbose --output-format text --dangerously-skip-permissions [--model ...]` with prompt on stdin.
  - **codex**: `codex exec --json --dangerously-bypass-approvals-and-sandbox [--model ...] <prompt>`.
  - **gemini**: `gemini --output-format json --yolo [-m model] -p <prompt>`.
  - **cursor-agent**: `cursor-agent --print --output-format json --force [--model ...] <prompt>`.

- **Stream views** — `--stream-view pretty` (default) or `raw`. For Codex, Cursor Agent, and Gemini, “pretty” uses a **line-by-line JSON renderer** (`codexPrettyRenderer`, `cursorAgentPrettyRenderer`, `geminiPrettyRenderer`) to show condensed events (e.g. `[cmd] ...`, `[done]`, `[error]`) instead of raw JSON. Claude and others fall back to raw with a notice.

- **`consoleStreamWriter`** — Writes agent output to both the log file and the console; buffers by line and passes each line to the selected `streamRenderer`; `Flush()` at the end to handle remaining buffer and `FinalLines()`.

### 4.6 Session limit and error detection

- **Regexes** — Various patterns for Claude (“usage limit”, “resets at …”), Codex (`resets_at`, `resets_in_seconds`, `usage_limit_reached`), Gemini (quota/capacity/429, duration string).
- **`detectSessionLimit`** — True if the log (and optionally exit code) indicates a retryable limit; for `cursor-agent` the tool does not treat quota as retryable.
- **`waitDuration*`** — Parse reset time or duration from log and return (waitSeconds, resetTime); add `--wait-buffer-sec` to the wait.
- **Internal server errors** — Detected via regex (500/502/503/504, “overloaded”); trigger up to 3 retries with delay.

### 4.7 Completion and labels

- **`markCompleted(issue)`** — Appends the issue ID to `.completed` and adds it to `doneSet`; in GitHub mode sets label `ghir:done` and clears other ghir labels.
- **`setIssueLabel(issue, "ghir:queued"|"ghir:running"|"ghir:done")`** — Implemented via `gh issue edit --add-label ... --remove-label ...` (no-op in file mode).

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
- `ghir --loop` with `--all-open` or `--all-files` — Continuous run.

### 5.3 Default prompts

- **GitHub issues**: Instructs the agent to implement the issue, run checks/tests, and commit with “fix:” or “feat:” and “Closes #{{ISSUE_NUMBER}}”; no push.
- **File mode**: Same idea but keyed by `{{FILE_PATH}}` and no “Closes #” in the default template.

---

## 6. Safety and Failure Behavior

- **Must run inside a git repository** — Exits with error otherwise.
- **Clean working tree** — Before each issue, the working tree must be clean (no uncommitted changes); otherwise the run is aborted with a hint to commit or stash.
- **Stop on first non-retryable failure** — Unless `--continue-on-error` is set.
- **Retries**:
  - **Session/usage limits** (Claude, Codex, Gemini): wait until reset time + buffer, then retry the same issue.
  - **Internal server errors**: up to 3 retries with 5-second delay.
- **cursor-agent** — Monthly quota/resource exhaustion is treated as **non-retryable** (no automatic wait/retry).

---

## 7. Development and Testing

- **Makefile** — `make help`, `make build`, `make install`, `make run ARGS="..."`. Version/commit/date are injected via `-ldflags` into `main.version`, `main.commit`, `main.date`.
- **Tests** — All in `package main`:
  - **`main_test.go`** — Unit tests for `parseArgs`, path resolution, issue parsing, prompt building, session-limit detection, wait duration parsing, etc.
  - **`preflight_test.go`** — `checkBinary`, `preflightChecks` (including file mode vs issue mode and missing `gh`).
  - **`hints_test.go`** — Error messages and hints (e.g. missing issues file, `gh auth login`).
  - **`smoke_test.go`** — Integration smoke: temp git repo, stub `gh` and agent binary, run ghir and assert completion and commit.
- **No external Go deps** — Tests use only standard library and (in smoke) temp dirs and stub scripts.

---

## 8. File Structure (summary)

```
ghir/
├── main.go              # Full application: CLI, runner, agents, streaming, retries
├── main_test.go         # Unit tests (args, parsing, prompts, session limit, etc.)
├── preflight_test.go    # Binary and preflight checks
├── hints_test.go        # Error hint tests
├── smoke_test.go        # Integration smoke test (git + stubbed gh + agent)
├── go.mod               # Module ghir, Go 1.22
├── Makefile             # build, install, run (with version ldflags)
├── README.md            # User-facing quick start and commands
├── PROJECT_OVERVIEW.md  # This document
├── LICENSE              # MIT
├── sync-to-public.sh    # Script to publish repo snapshot (excludes .ticket-runner, .ticket-runs, etc.)
├── tasks/               # Example/local task .md files (optional)
└── ...
```

When installed, the binary is typically `ghir` (or the name set by `INSTALL_NAME` in the Makefile).

---

## 9. Summary

| Aspect | Summary |
|--------|--------|
| **What** | Queue-driven GitHub issue (or local .md task) runner that invokes an AI agent CLI per item, commits results, and tracks completion. |
| **How** | Load list of issues/files → for each pending item: fetch details, build prompt, run agent, handle commits and session limits, mark complete. |
| **Tech** | Go 1.22, no external Go deps; depends on `git`, `gh`, and one of `claude`/`codex`/`gemini`/`cursor-agent`. |
| **State** | File-based: `.ticket-runner/issues.txt`, `.ticket-runs/.completed`, `.ticket-runs/<id>.log`, optional `.ticket-runner/prompt.tmpl`. |
| **Safety** | Requires git repo and clean tree per run; optional retries for session limits and 5xx; can commit partial work on limit. |
| **Extensibility** | Custom prompt template, agent/model override, file mode (no GitHub), `--continue-on-error`, `--loop`. |

This document reflects the behavior and structure of the **ghir** codebase as of the current implementation.
