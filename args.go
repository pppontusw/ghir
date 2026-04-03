package main

import (
	"fmt"
	"slices"
	"strconv"
	"strings"

	"ghir/tui"
)

type tuiOptions struct {
	Mode         string
	LoadPreset   string
	Experimental bool
	NoColor      bool
	Help         bool
}

func parseArgs(args []string) (options, error) {
	opts := options{
		agentConfig: defaultAgentConfig(),
		Strategy:    tui.DefaultStrategy,
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--dry-run":
			opts.DryRun = true
		case "--issue":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.SingleIssue = val
			i = next
		case "--all-open":
			opts.AllOpen = true
		case "--label":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.Label = val
			i = next
		case "--force":
			opts.Force = true
		case "--status":
			opts.Status = true
		case "--reset":
			opts.Reset = true
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				opts.ResetIssue = args[i+1]
				i++
			}
		case "--issues":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.IssuesCSV = val
			i = next
		case "--issues-file":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.IssuesFile = val
			i = next
		case "--files":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.Files = val
			i = next
		case "--all-files":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.AllFiles = val
			i = next
		case "--log-dir":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.LogDir = val
			i = next
		case "--strategy":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.Strategy = strings.ToLower(val)
			i = next
		case "--done-file":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.DoneFile = val
			i = next
		case "--prompt-template":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.PromptTemplate = val
			i = next
		default:
			if next, handled, err := handleAgentFlag(arg, args, i, &opts.agentConfig); err != nil {
				return opts, err
			} else if handled {
				i = next
				continue
			}
			switch arg {
			case "--continue-on-error":
				opts.ContinueOnError = true
			case "--loop":
				opts.Loop = true
			case "--version":
				opts.Version = true
			case "-h", "--help":
				opts.Help = true
			default:
				return opts, fmt.Errorf("unknown option: %s", arg)
			}
		}
	}

	hasFileFlags := opts.Files != "" || opts.AllFiles != ""
	hasIssueFlags := opts.SingleIssue != "" || opts.AllOpen || opts.IssuesCSV != "" || opts.IssuesFile != ""
	if hasFileFlags && hasIssueFlags {
		return opts, fmt.Errorf("--files/--all-files cannot be combined with --issue/--all-open/--issues/--issues-file")
	}
	if !slices.Contains(tui.ValidStrategies, opts.Strategy) {
		return opts, fmt.Errorf("--strategy must be one of: %s", strings.Join(tui.ValidStrategies, ", "))
	}
	if opts.Loop && !opts.AllOpen && opts.AllFiles == "" {
		return opts, fmt.Errorf("--loop requires either --all-open or --all-files")
	}
	if opts.Loop && opts.Strategy != tui.DefaultStrategy {
		return opts, fmt.Errorf("--loop is only supported with --strategy %s", tui.DefaultStrategy)
	}
	if opts.SingleIssue != "" && !tui.MatchIssueNumber(opts.SingleIssue) {
		return opts, fmt.Errorf("--issue must be numeric: %q", opts.SingleIssue)
	}
	if err := validateAgentConfig(opts.agentConfig); err != nil {
		return opts, err
	}

	return opts, nil
}

func parseTUIArgs(args []string) (tuiOptions, error) {
	opts := tuiOptions{}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--mode":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.Mode = strings.ToLower(val)
			i = next
		case "--load-preset":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.LoadPreset = val
			i = next
		case "--experimental":
			opts.Experimental = true
		case "--no-color":
			opts.NoColor = true
		case "-h", "--help":
			opts.Help = true
		default:
			return opts, fmt.Errorf("unknown option: %s", arg)
		}
	}

	validTUIModes := []string{"issues", "files", "improve"}
	if opts.Mode != "" && !slices.Contains(validTUIModes, opts.Mode) {
		return opts, fmt.Errorf("--mode must be one of: issues, files, improve")
	}

	return opts, nil
}

func parseImproveArgs(args []string) (improveOptions, error) {
	opts := improveOptions{
		agentConfig: defaultAgentConfig(),
		Mode:        "cleanup",
		ModeList:    []string{"cleanup"},
		Iterations:  1,
		Strategy:    "direct",
	}

	for i := 0; i < len(args); i++ {
		arg := args[i]
		switch arg {
		case "--mode":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.Mode = strings.ToLower(val)
			opts.ModeExplicit = true
			i = next
		case "--prompt":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.Prompt = val
			i = next
		case "--prompt-file":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.PromptFile = val
			i = next
		case "--iterations":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			n, convErr := strconv.Atoi(val)
			if convErr != nil || n < 0 {
				return opts, fmt.Errorf("--iterations must be a non-negative integer")
			}
			opts.Iterations = n
			i = next
		case "--loop":
			opts.Loop = true
		case "--strategy":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.Strategy = strings.ToLower(val)
			i = next
		case "--scope":
			val, next, err := requireValue(arg, args, i)
			if err != nil {
				return opts, err
			}
			opts.Scope = val
			i = next
		default:
			if next, handled, err := handleAgentFlag(arg, args, i, &opts.agentConfig); err != nil {
				return opts, err
			} else if handled {
				i = next
				continue
			}
			switch arg {
			case "-h", "--help":
				opts.Help = true
			default:
				return opts, fmt.Errorf("unknown option: %s", arg)
			}
		}
	}

	if strings.TrimSpace(opts.Prompt) != "" && strings.TrimSpace(opts.PromptFile) != "" {
		return opts, fmt.Errorf("--prompt and --prompt-file cannot be combined")
	}
	if (strings.TrimSpace(opts.Prompt) != "" || strings.TrimSpace(opts.PromptFile) != "") && opts.ModeExplicit {
		return opts, fmt.Errorf("--mode cannot be combined with --prompt or --prompt-file")
	}
	mode, modeList, err := parseImproveModeValue(opts.Mode)
	if err != nil {
		return opts, err
	}
	opts.Mode = mode
	opts.ModeList = modeList
	if !slices.Contains(tui.ValidImproveStrategies, opts.Strategy) {
		return opts, fmt.Errorf("--strategy must be one of: %s", strings.Join(tui.ValidImproveStrategies, ", "))
	}
	if err := validateAgentConfig(opts.agentConfig); err != nil {
		return opts, err
	}

	if opts.Iterations == 0 && !opts.Loop {
		return opts, fmt.Errorf("--iterations must be positive unless --loop is set")
	}

	return opts, nil
}

func parseImproveModeValue(value string) (string, []string, error) {
	mode := strings.ToLower(strings.TrimSpace(value))
	if mode == "" {
		mode = "cleanup"
	}

	parts := strings.Split(mode, ",")
	if len(parts) == 1 {
		mode = strings.TrimSpace(parts[0])
		if !slices.Contains(tui.ValidImproveModes, mode) {
			return "", nil, fmt.Errorf("--mode must be one of: %s", strings.Join(tui.ValidImproveModes, ", "))
		}
		return mode, []string{mode}, nil
	}

	modeList := make([]string, 0, len(parts))
	seen := make(map[string]struct{}, len(parts))
	for _, part := range parts {
		part = strings.ToLower(strings.TrimSpace(part))
		if part == "" {
			return "", nil, fmt.Errorf("--mode cannot contain empty entries")
		}
		if part == "mixed" {
			return "", nil, fmt.Errorf("--mode cannot combine mixed with other modes")
		}
		if !slices.Contains(improveModes, part) {
			return "", nil, fmt.Errorf("--mode must be one of: %s", strings.Join(tui.ValidImproveModes, ", "))
		}
		if _, ok := seen[part]; ok {
			return "", nil, fmt.Errorf("--mode cannot contain duplicate entries: %s", part)
		}
		seen[part] = struct{}{}
		modeList = append(modeList, part)
	}

	return strings.Join(modeList, ","), modeList, nil
}

func requireValue(flag string, args []string, idx int) (string, int, error) {
	if idx+1 >= len(args) {
		return "", idx, fmt.Errorf("%s requires a value", flag)
	}
	if strings.HasPrefix(args[idx+1], "--") {
		return "", idx, fmt.Errorf("%s requires a value", flag)
	}
	return args[idx+1], idx + 1, nil
}

// setAgentStringFlag parses a value for a string-valued agent flag and applies setter.
// Returns (nextIndex, true, nil) on success, or (i, true, err) on parse error.
func setAgentStringFlag(arg string, args []string, i int, setter func(string)) (int, bool, error) {
	val, next, err := requireValue(arg, args, i)
	if err != nil {
		return i, true, err
	}
	setter(val)
	return next, true, nil
}

// handleAgentFlag processes a single agent-related flag. If the flag is agent-related,
// it updates cfg and returns (nextIndex, true, nil). If not, returns (i, false, nil).
// On error, returns (i, true, err).
func handleAgentFlag(arg string, args []string, i int, cfg *agentConfig) (next int, handled bool, err error) {
	switch arg {
	case "--agent":
		return setAgentStringFlag(arg, args, i, func(v string) { cfg.Agent = strings.ToLower(v) })
	case "--model":
		return setAgentStringFlag(arg, args, i, func(v string) { cfg.Model = v })
	case "--claude-bin":
		return setAgentStringFlag(arg, args, i, func(v string) { cfg.ClaudeBin = v })
	case "--codex-bin":
		return setAgentStringFlag(arg, args, i, func(v string) { cfg.CodexBin = v })
	case "--gemini-bin":
		return setAgentStringFlag(arg, args, i, func(v string) { cfg.GeminiBin = v })
	case "--cursor-bin":
		return setAgentStringFlag(arg, args, i, func(v string) { cfg.CursorBin = v })
	case "--pi-bin":
		return setAgentStringFlag(arg, args, i, func(v string) { cfg.PiBin = v })
	case "--gh-bin":
		return setAgentStringFlag(arg, args, i, func(v string) { cfg.GHBin = v })
	case "--wait-buffer-sec":
		val, next, parseErr := requireValue(arg, args, i)
		if parseErr != nil {
			return i, true, parseErr
		}
		waitSec, convErr := strconv.Atoi(val)
		if convErr != nil || waitSec < 0 {
			return i, true, fmt.Errorf("--wait-buffer-sec must be a non-negative integer")
		}
		cfg.WaitBufferSec = waitSec
		return next, true, nil
	case "--stream-view":
		return setAgentStringFlag(arg, args, i, func(v string) { cfg.StreamView = strings.ToLower(v) })
	case "--no-color":
		cfg.NoColor = true
		return i, true, nil
	default:
		return i, false, nil
	}
}

func printUsage() {
	fmt.Print(`ghir

Usage:
  ghir [options]
  ghir improve [options]
  ghir tui [options]

  Options:
  --dry-run                     Show what would run without invoking the agent CLI
  --strategy <direct|pr-per-pass|pr-chain|pr-at-end>  Apply changes directly, open one PR per item, chain PRs, or open one PR after the full queue (default: direct)
  --issue <id>                  Process exactly one issue (forced re-run)
  --all-open                    Process all open issues in the repository
  --label <label>               Filter issues by label when using --all-open
  --force                       Re-run even if issue is marked completed
  --status                      Show completion status for configured issues
  --reset [id]                  Reset all completions, or one issue if id is provided
  --issues <id1,id2,...>        Comma-separated issue list (overrides file)
  --issues-file <path>          Issue list file (default: .ticket-runner/issues.txt)
  --files <path1,path2,...>      Comma-separated markdown file paths
  --all-files <dir>              Process all *.md files in directory
  --prompt-template <path>      Optional template with {{ISSUE_NUMBER}}, {{ISSUE_TITLE}}, {{ISSUE_BODY}}
  --agent <claude|codex|gemini|cursor-agent|pi> Agent CLI to run (default: claude)
  --model <model-id>            Override model for selected agent
  --log-dir <path>              Log directory (default: .ticket-runs)
  --done-file <path>            Completion file (default: <log-dir>/.completed)
  --claude-bin <name/path>      Claude CLI command (default: claude)
  --codex-bin <name/path>       Codex CLI command (default: codex)
  --gemini-bin <name/path>      Gemini CLI command (default: gemini)
  --cursor-bin <name/path>      Cursor-agent CLI command (default: cursor-agent)
  --pi-bin <name/path>          pi CLI command (default: pi)
  --gh-bin <name/path>          GitHub CLI command (default: gh)
  --stream-view <pretty|raw>    Console streaming view (default: pretty)
  --wait-buffer-sec <seconds>   Extra wait seconds after reset time (default: 120)
  --no-color                    Disable ANSI colors
  --continue-on-error           Continue processing remaining issues after a failure
  --version                     Show version information
  -h, --help                    Show this help

Subcommands:
  improve                       Continuous improvement mode
  tui                           Terminal UI for configure -> run -> summary workflows
`)
}

func printImproveUsage() {
	fmt.Print(`ghir improve - Continuous improvement mode

Usage:
  ghir improve [options]

Options:
  --mode <mode>                               Improvement focus: cleanup, quality, refactor, security, bugfix, dead-code, docs, tests, deps, perf, a11y, errors, types, logging, mixed, or a comma-separated list of built-in modes (default: cleanup)
  --prompt <text>                             Use inline custom improve prompt text instead of a built-in mode
  --prompt-file <path>                        Load custom improve prompt text from a file instead of a built-in mode
  --iterations <N>                            Number of passes to run (default: 1)
  --loop                                      Run passes continuously until interrupted (iterations acts as an optional cap)
  --strategy <direct|pr-per-pass|pr-chain|pr-at-end>  Apply changes directly to the current branch, create a PR per pass, chain PRs, or a single PR at the end (default: direct)
  --scope <path>                              Optional repo-relative path to focus improvements on

Agent and streaming options:
  --agent <claude|codex|gemini|cursor-agent|pi>  Agent CLI to run (default: claude)
  --model <model-id>                          Override model for selected agent
  --claude-bin <name/path>                    Claude CLI command (default: claude)
  --codex-bin <name/path>                     Codex CLI command (default: codex)
  --gemini-bin <name/path>                    Gemini CLI command (default: gemini)
  --cursor-bin <name/path>                    Cursor-agent CLI command (default: cursor-agent)
  --pi-bin <name/path>                        pi CLI command (default: pi)
  --gh-bin <name/path>                        GitHub CLI command (default: gh)
  --stream-view <pretty|raw>                  Console streaming view (default: pretty)
  --wait-buffer-sec <seconds>                 Extra wait seconds after reset time (default: 120)
  --no-color                                  Disable ANSI colors
  -h, --help                                  Show this help
`)
}

func printTUIUsage() {
	fmt.Print(`ghir tui - Terminal UI for configure -> run -> summary workflows

Usage:
  ghir tui [options]

Options:
  --mode <issues|files|improve>  Preselect the workflow to open
  --load-preset <name>           Load a saved TUI preset by name
  --no-color                     Disable ANSI styling in the TUI shell
  -h, --help                     Show this help

Compatibility:
  --experimental                 Accepted for backward compatibility; no longer required
`)
}
