# ticket-runner

Go rewrite of `run-tickets.sh` so you can build one binary and run it from any git repository.

## Build

```bash
go build -o ticket-runner .
```

Or install globally:

```bash
go install .
```

## Repository setup

In each target repository, create `.ticket-runner/issues.txt`:

```text
# one issue id per line
1721
1706
1710
```

Optional: create `.ticket-runner/prompt.tmpl` to override the default prompt. Available placeholders:

- `{{ISSUE_NUMBER}}`
- `{{ISSUE_TITLE}}`
- `{{ISSUE_BODY}}`

## Usage

```bash
ticket-runner --dry-run
ticket-runner --status
ticket-runner --issue 1710
ticket-runner --force
ticket-runner --reset
ticket-runner --reset 1710
ticket-runner --issues 1721,1706,1710
ticket-runner --agent codex --issues 1721,1706
ticket-runner --agent gemini --issues 1721,1706
ticket-runner --agent cursor-agent --issues 1721,1706
ticket-runner --agent claude --model sonnet --issues 1721,1706
```

## Notes

- The tool must run inside a git repository.
- Logs are written to `.ticket-runs/<issue>.log`.
- Completion tracking is stored in `.ticket-runs/.completed`.
- For `claude`, `codex`, and `gemini`, usage/session limits trigger wait-and-retry.
- For `cursor-agent`, monthly quota/resource exhaustion is treated as non-retryable.
- `--agent` supports `claude` (default), `codex`, `gemini`, and `cursor-agent`.
- `--model` applies a direct model override and maps to each CLI's native flag:
  - Claude: `--model`
  - Codex: `--model`
  - Gemini: `-m`
  - Cursor Agent: `--model`
