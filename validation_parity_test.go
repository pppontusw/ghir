package main

import (
	"strings"
	"testing"

	appshell "ghir/tui"
)

func TestTUIValidationParityWithCLIParsers(t *testing.T) {
	t.Parallel()

	iterationsZero := 0
	iterationsNegative := -1
	waitNegative := -1

	tests := []struct {
		name    string
		state   appshell.CommandState
		args    []string
		improve bool
		wantErr string
	}{
		{
			name: "issue file conflict",
			state: appshell.CommandState{
				Workflow: appshell.WorkflowIssues,
				Issues: appshell.IssueCommandState{
					Source: appshell.IssueSourceAllOpen,
				},
				Files: appshell.FileCommandState{
					Source:   appshell.FileSourceAllFiles,
					AllFiles: "tasks",
				},
			},
			args:    []string{"--all-open", "--all-files", "tasks"},
			wantErr: "--files/--all-files cannot be combined with --issue/--all-open/--issues/--issues-file",
		},
		{
			name: "issue loop requires all open",
			state: appshell.CommandState{
				Workflow: appshell.WorkflowIssues,
				Issues: appshell.IssueCommandState{
					Source: appshell.IssueSourceSingle,
					Loop:   true,
				},
			},
			args:    []string{"--issue", "13", "--loop"},
			wantErr: "--loop requires either --all-open or --all-files",
		},
		{
			name: "single issue must be numeric",
			state: appshell.CommandState{
				Workflow: appshell.WorkflowIssues,
				Issues: appshell.IssueCommandState{
					Source:      appshell.IssueSourceSingle,
					SingleIssue: "abc",
				},
			},
			args:    []string{"--issue", "abc"},
			wantErr: `--issue must be numeric: "abc"`,
		},
		{
			name: "issue strategy enum",
			state: appshell.CommandState{
				Workflow: appshell.WorkflowIssues,
				Issues: appshell.IssueCommandState{
					Source:   appshell.IssueSourceCSV,
					Issues:   []string{"13"},
					Strategy: "nope",
				},
			},
			args:    []string{"--issues", "13", "--strategy", "nope"},
			wantErr: "--strategy must be one of: direct, pr-per-pass, pr-chain, pr-at-end",
		},
		{
			name: "issue loop strategy restriction",
			state: appshell.CommandState{
				Workflow: appshell.WorkflowIssues,
				Issues: appshell.IssueCommandState{
					Source:   appshell.IssueSourceAllOpen,
					Loop:     true,
					Strategy: "pr-per-pass",
				},
			},
			args:    []string{"--all-open", "--loop", "--strategy", "pr-per-pass"},
			wantErr: "--loop is only supported with --strategy direct",
		},
		{
			name: "file strategy enum",
			state: appshell.CommandState{
				Workflow: appshell.WorkflowFiles,
				Files: appshell.FileCommandState{
					Source:   appshell.FileSourceAllFiles,
					AllFiles: "tasks",
					Strategy: "nope",
				},
			},
			args:    []string{"--all-files", "tasks", "--strategy", "nope"},
			wantErr: "--strategy must be one of: direct, pr-per-pass, pr-chain, pr-at-end",
		},
		{
			name: "file loop strategy restriction",
			state: appshell.CommandState{
				Workflow: appshell.WorkflowFiles,
				Files: appshell.FileCommandState{
					Source:   appshell.FileSourceAllFiles,
					AllFiles: "tasks",
					Loop:     true,
					Strategy: "pr-chain",
				},
			},
			args:    []string{"--all-files", "tasks", "--loop", "--strategy", "pr-chain"},
			wantErr: "--loop is only supported with --strategy direct",
		},
		{
			name: "improve mode enum",
			state: appshell.CommandState{
				Workflow: appshell.WorkflowImprove,
				Improve: appshell.ImproveCommandState{
					Mode: "nope",
				},
			},
			args:    []string{"--mode", "nope"},
			improve: true,
			wantErr: "--mode must be one of: mixed, cleanup, quality, refactor, security, bugfix, dead-code, docs, tests, deps, perf, a11y, errors, types, logging",
		},
		{
			name: "improve strategy enum",
			state: appshell.CommandState{
				Workflow: appshell.WorkflowImprove,
				Improve: appshell.ImproveCommandState{
					Strategy: "nope",
				},
			},
			args:    []string{"--strategy", "nope"},
			improve: true,
			wantErr: "--strategy must be one of: direct, pr-per-pass, pr-chain, pr-at-end",
		},
		{
			name: "improve inline prompt valid",
			state: appshell.CommandState{
				Workflow: appshell.WorkflowImprove,
				Improve: appshell.ImproveCommandState{
					PromptSource: appshell.ImprovePromptSourceInline,
					Prompt:       "custom improve prompt",
				},
			},
			args:    []string{"--prompt", "custom improve prompt"},
			improve: true,
		},
		{
			name: "improve prompt file valid",
			state: appshell.CommandState{
				Workflow: appshell.WorkflowImprove,
				Improve: appshell.ImproveCommandState{
					PromptSource: appshell.ImprovePromptSourceFile,
					PromptFile:   "prompts/improve.txt",
				},
			},
			args:    []string{"--prompt-file", "prompts/improve.txt"},
			improve: true,
		},
		{
			name: "iterations zero requires loop",
			state: appshell.CommandState{
				Workflow: appshell.WorkflowImprove,
				Improve: appshell.ImproveCommandState{
					Iterations: &iterationsZero,
				},
			},
			args:    []string{"--iterations", "0"},
			improve: true,
			wantErr: "--iterations must be positive unless --loop is set",
		},
		{
			name: "iterations must be non-negative",
			state: appshell.CommandState{
				Workflow: appshell.WorkflowImprove,
				Improve: appshell.ImproveCommandState{
					Iterations: &iterationsNegative,
				},
			},
			args:    []string{"--iterations", "-1"},
			improve: true,
			wantErr: "--iterations must be a non-negative integer",
		},
		{
			name: "agent enum",
			state: appshell.CommandState{
				Runtime: appshell.CommandRuntime{
					Agent: "nope",
				},
			},
			args:    []string{"--agent", "nope"},
			wantErr: "--agent must be one of: claude, codex, gemini, cursor-agent, pi",
		},
		{
			name: "stream view enum",
			state: appshell.CommandState{
				Runtime: appshell.CommandRuntime{
					StreamView: "minimal",
				},
			},
			args:    []string{"--stream-view", "minimal"},
			wantErr: "--stream-view must be one of: pretty, raw",
		},
		{
			name: "wait buffer must be non-negative",
			state: appshell.CommandState{
				Runtime: appshell.CommandRuntime{
					WaitBufferSec: &waitNegative,
				},
			},
			args:    []string{"--wait-buffer-sec", "-1"},
			wantErr: "--wait-buffer-sec must be a non-negative integer",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			report := appshell.ValidateCommandState(tt.state)
			if tt.wantErr == "" {
				if report.HasErrors() {
					t.Fatalf("expected no validation errors, got %#v", report.Items())
				}
			} else {
				if !report.HasErrors() {
					t.Fatalf("expected validation errors, got %#v", report.Items())
				}
				if !reportContainsMessage(report, tt.wantErr) {
					t.Fatalf("expected validation report to contain %q, got %#v", tt.wantErr, report.Items())
				}
			}

			var err error
			if tt.improve {
				_, err = parseImproveArgs(tt.args)
			} else {
				_, err = parseArgs(tt.args)
			}
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected parser success, got %v", err)
				}
			} else {
				if err == nil {
					t.Fatalf("expected parser error containing %q, got nil", tt.wantErr)
				}
				if err.Error() != tt.wantErr {
					t.Fatalf("parser mismatch: got %q want %q", err.Error(), tt.wantErr)
				}
			}
		})
	}
}

func TestTUIValidationParityAcceptsCLIValidConfigurations(t *testing.T) {
	t.Parallel()

	iterations := 2
	waitBuffer := 30

	tests := []struct {
		name    string
		state   appshell.CommandState
		improve bool
	}{
		{
			name: "issues workflow",
			state: appshell.CommandState{
				Workflow: appshell.WorkflowIssues,
				Runtime: appshell.CommandRuntime{
					Agent:         "codex",
					StreamView:    "raw",
					WaitBufferSec: &waitBuffer,
				},
				Issues: appshell.IssueCommandState{
					Source:   appshell.IssueSourceCSV,
					Issues:   []string{"13", "19"},
					Strategy: "pr-per-pass",
				},
			},
		},
		{
			name: "files workflow",
			state: appshell.CommandState{
				Workflow: appshell.WorkflowFiles,
				Runtime: appshell.CommandRuntime{
					Agent:      "claude",
					StreamView: "pretty",
				},
				Files: appshell.FileCommandState{
					Source:   appshell.FileSourceAllFiles,
					AllFiles: "tasks",
					Strategy: "pr-at-end",
				},
			},
		},
		{
			name: "improve workflow",
			state: appshell.CommandState{
				Workflow: appshell.WorkflowImprove,
				Runtime: appshell.CommandRuntime{
					Agent:      "gemini",
					StreamView: "pretty",
				},
				Improve: appshell.ImproveCommandState{
					PromptSource: appshell.ImprovePromptSourceInline,
					Prompt:       "custom improve prompt",
					Iterations:   &iterations,
					Strategy:     "pr-per-pass",
				},
			},
			improve: true,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			report := appshell.ValidateCommandState(tt.state)
			if report.HasErrors() {
				t.Fatalf("expected no validation errors, got %#v", report.Items())
			}

			args := appshell.BuildCommandArgs(tt.state)
			var err error
			if tt.improve {
				_, err = parseImproveArgs(args[1:])
			} else {
				_, err = parseArgs(args)
			}
			if err != nil {
				t.Fatalf("parser rejected valid TUI args %v: %v", args, err)
			}
		})
	}
}

func reportContainsMessage(report appshell.DiagnosticReport, needle string) bool {
	for _, item := range report.Items() {
		if strings.Contains(item.Message, needle) || strings.Contains(item.Hint, needle) {
			return true
		}
	}
	return false
}
