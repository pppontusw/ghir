package tui

import (
	"strings"
	"testing"
)

func TestValidateCommandStateParserParity(t *testing.T) {
	t.Parallel()

	iterationsZero := 0
	iterationsNegative := -1

	tests := []struct {
		name    string
		state   CommandState
		wantErr string
	}{
		{
			name: "issue file conflict",
			state: CommandState{
				Workflow: WorkflowIssues,
				Issues: IssueCommandState{
					Source: IssueSourceAllOpen,
				},
				Files: FileCommandState{
					Source:   FileSourceAllFiles,
					AllFiles: "tasks",
				},
			},
			wantErr: "--files/--all-files cannot be combined with --issue/--all-open/--issues/--issues-file",
		},
		{
			name: "issue loop requires all open",
			state: CommandState{
				Workflow: WorkflowIssues,
				Issues: IssueCommandState{
					Source: IssueSourceSingle,
					Loop:   true,
				},
			},
			wantErr: "--loop requires either --all-open or --all-files",
		},
		{
			name: "single issue must be numeric",
			state: CommandState{
				Workflow: WorkflowIssues,
				Issues: IssueCommandState{
					Source:      IssueSourceSingle,
					SingleIssue: "abc",
				},
			},
			wantErr: `--issue must be numeric: "abc"`,
		},
		{
			name: "issue strategy enum",
			state: CommandState{
				Workflow: WorkflowIssues,
				Issues: IssueCommandState{
					Source:   IssueSourceCSV,
					Issues:   []string{"13"},
					Strategy: "nope",
				},
			},
			wantErr: "--strategy must be one of: direct, pr-per-pass, pr-chain, pr-at-end",
		},
		{
			name: "issue loop strategy restriction",
			state: CommandState{
				Workflow: WorkflowIssues,
				Issues: IssueCommandState{
					Source:   IssueSourceAllOpen,
					Loop:     true,
					Strategy: "pr-per-pass",
				},
			},
			wantErr: "--loop is only supported with --strategy direct",
		},
		{
			name: "file strategy enum",
			state: CommandState{
				Workflow: WorkflowFiles,
				Files: FileCommandState{
					Source:   FileSourceAllFiles,
					AllFiles: "tasks",
					Strategy: "nope",
				},
			},
			wantErr: "--strategy must be one of: direct, pr-per-pass, pr-chain, pr-at-end",
		},
		{
			name: "file loop strategy restriction",
			state: CommandState{
				Workflow: WorkflowFiles,
				Files: FileCommandState{
					Source:   FileSourceAllFiles,
					AllFiles: "tasks",
					Loop:     true,
					Strategy: "pr-chain",
				},
			},
			wantErr: "--loop is only supported with --strategy direct",
		},
		{
			name: "improve mode enum",
			state: CommandState{
				Workflow: WorkflowImprove,
				Improve: ImproveCommandState{
					Mode: "nope",
				},
			},
			wantErr: "--mode must be one of: mixed, cleanup, quality, refactor, security, bugfix, dead-code, docs, tests, deps, perf, a11y, errors, types, logging",
		},
		{
			name: "improve strategy enum",
			state: CommandState{
				Workflow: WorkflowImprove,
				Improve: ImproveCommandState{
					Strategy: "nope",
				},
			},
			wantErr: "--strategy must be one of: direct, pr-per-pass, pr-chain, pr-at-end",
		},
		{
			name: "improve inline prompt requires value",
			state: CommandState{
				Workflow: WorkflowImprove,
				Improve: ImproveCommandState{
					PromptSource: ImprovePromptSourceInline,
				},
			},
			wantErr: "--prompt requires a value",
		},
		{
			name: "improve prompt file requires value",
			state: CommandState{
				Workflow: WorkflowImprove,
				Improve: ImproveCommandState{
					PromptSource: ImprovePromptSourceFile,
				},
			},
			wantErr: "--prompt-file requires a value",
		},
		{
			name: "iterations zero requires loop",
			state: CommandState{
				Workflow: WorkflowImprove,
				Improve: ImproveCommandState{
					Iterations: &iterationsZero,
				},
			},
			wantErr: "--iterations must be positive unless --loop is set",
		},
		{
			name: "iterations must be non-negative",
			state: CommandState{
				Workflow: WorkflowImprove,
				Improve: ImproveCommandState{
					Iterations: &iterationsNegative,
				},
			},
			wantErr: "--iterations must be a non-negative integer",
		},
		{
			name: "agent enum",
			state: CommandState{
				Runtime: CommandRuntime{
					Agent: "nope",
				},
			},
			wantErr: "--agent must be one of: claude, codex, gemini, cursor-agent, pi",
		},
		{
			name: "stream view enum",
			state: CommandState{
				Runtime: CommandRuntime{
					StreamView: "minimal",
				},
			},
			wantErr: "--stream-view must be one of: pretty, raw",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			report := ValidateCommandState(tt.state)
			if !report.HasErrors() {
				t.Fatalf("expected validation errors, got %#v", report.Items())
			}
			if !reportContains(report, tt.wantErr) {
				t.Fatalf("expected report to contain %q, got %#v", tt.wantErr, report.Items())
			}
		})
	}
}

func TestValidateCommandStatePassesValidConfig(t *testing.T) {
	t.Parallel()

	report := ValidateCommandState(CommandState{
		Workflow: WorkflowFiles,
		Runtime: CommandRuntime{
			Agent:      "claude",
			StreamView: "pretty",
		},
		Files: FileCommandState{
			Source:   FileSourceAllFiles,
			AllFiles: "tasks",
			Loop:     true,
			Strategy: "direct",
		},
	})

	if report.HasErrors() {
		t.Fatalf("expected no validation errors, got %#v", report.Items())
	}
	if !reportContains(report, "Parser-equivalent validation passed.") {
		t.Fatalf("expected success info, got %#v", report.Items())
	}
}

func TestValidateCommandStateNormalizesStrategyValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		state CommandState
	}{
		{
			name: "issues",
			state: CommandState{
				Workflow: WorkflowIssues,
				Runtime: CommandRuntime{
					Agent:      "claude",
					StreamView: "pretty",
				},
				Issues: IssueCommandState{
					Source:   IssueSourceAllOpen,
					Strategy: " PR-CHAIN ",
				},
			},
		},
		{
			name: "files",
			state: CommandState{
				Workflow: WorkflowFiles,
				Runtime: CommandRuntime{
					Agent:      "claude",
					StreamView: "pretty",
				},
				Files: FileCommandState{
					Source:   FileSourceAllFiles,
					AllFiles: "tasks",
					Strategy: " PR-PER-PASS ",
				},
			},
		},
		{
			name: "improve",
			state: CommandState{
				Workflow: WorkflowImprove,
				Runtime: CommandRuntime{
					Agent:      "codex",
					StreamView: "pretty",
				},
				Improve: ImproveCommandState{
					Strategy: " PR-AT-END ",
				},
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			report := ValidateCommandState(tt.state)
			if report.HasErrors() {
				t.Fatalf("expected no validation errors, got %#v", report.Items())
			}
		})
	}
}

func TestValidateCommandStatePassesValidImproveCustomPromptConfig(t *testing.T) {
	t.Parallel()

	report := ValidateCommandState(CommandState{
		Workflow: WorkflowImprove,
		Runtime: CommandRuntime{
			Agent:      "codex",
			StreamView: "pretty",
		},
		Improve: ImproveCommandState{
			PromptSource: ImprovePromptSourceInline,
			Prompt:       "custom improve prompt",
			Strategy:     "direct",
		},
	})

	if report.HasErrors() {
		t.Fatalf("expected no validation errors, got %#v", report.Items())
	}
}

func reportContains(report DiagnosticReport, needle string) bool {
	for _, item := range report.Items() {
		if strings.Contains(item.Message, needle) || strings.Contains(item.Hint, needle) {
			return true
		}
	}
	return false
}
