package tui

import (
	"slices"
	"testing"
)

func TestBuildCommandArgsIssuesWorkflowParity(t *testing.T) {
	t.Parallel()

	waitBuffer := 30
	state := CommandState{
		Workflow: WorkflowIssues,
		Runtime: CommandRuntime{
			Agent:         "codex",
			Model:         "gpt-5",
			StreamView:    "raw",
			WaitBufferSec: &waitBuffer,
			NoColor:       true,
			CodexBin:      "/usr/local/bin/codex",
			GHBin:         "/usr/local/bin/gh",
		},
		Issues: IssueCommandState{
			Source:          IssueSourceFile,
			Strategy:        "pr-per-pass",
			IssuesFile:      "custom/issues.txt",
			DryRun:          true,
			Force:           true,
			ContinueOnError: true,
			PromptTemplate:  "custom/prompt.tmpl",
			LogDir:          ".ticket-runs/custom",
			DoneFile:        ".ticket-runs/custom/.completed",
		},
	}

	want := []string{
		"--issues-file", "custom/issues.txt",
		"--strategy", "pr-per-pass",
		"--dry-run",
		"--force",
		"--continue-on-error",
		"--prompt-template", "custom/prompt.tmpl",
		"--log-dir", ".ticket-runs/custom",
		"--done-file", ".ticket-runs/custom/.completed",
		"--agent", "codex",
		"--model", "gpt-5",
		"--stream-view", "raw",
		"--wait-buffer-sec", "30",
		"--no-color",
		"--codex-bin", "/usr/local/bin/codex",
		"--gh-bin", "/usr/local/bin/gh",
	}

	if got := BuildCommandArgs(state); !slices.Equal(got, want) {
		t.Fatalf("args mismatch:\n got: %v\nwant: %v", got, want)
	}
}

func TestBuildCommandArgsFilesWorkflowParity(t *testing.T) {
	t.Parallel()

	state := CommandState{
		Workflow: WorkflowFiles,
		Runtime: CommandRuntime{
			Agent: "cursor-agent",
		},
		Files: FileCommandState{
			Source:          FileSourceAllFiles,
			AllFiles:        "tasks",
			Loop:            true,
			PromptTemplate:  "templates/review.tmpl",
			ResolvedQueue:   []string{"tasks/1.md", "tasks/2.md", "tasks/10.md"},
			StagedQueue:     []string{"tasks/1.md", "tasks/2.md", "tasks/10.md"},
			ContinueOnError: true,
		},
	}

	want := []string{
		"--all-files", "tasks",
		"--continue-on-error",
		"--loop",
		"--prompt-template", "templates/review.tmpl",
		"--agent", "cursor-agent",
	}

	if got := BuildCommandArgs(state); !slices.Equal(got, want) {
		t.Fatalf("args mismatch:\n got: %v\nwant: %v", got, want)
	}
}

func TestBuildCommandArgsImproveWorkflowParity(t *testing.T) {
	t.Parallel()

	iterations := 3
	waitBuffer := 0
	state := CommandState{
		Workflow: WorkflowImprove,
		Runtime: CommandRuntime{
			Agent:         "gemini",
			Model:         "gemini-2.5-pro",
			StreamView:    "raw",
			WaitBufferSec: &waitBuffer,
			GHBin:         "gh-enterprise",
		},
		Improve: ImproveCommandState{
			Mode:       "bugfix",
			Iterations: &iterations,
			Loop:       true,
			Strategy:   "pr-per-pass",
			Scope:      "backend/",
		},
	}

	want := []string{
		"improve",
		"--mode", "bugfix",
		"--iterations", "3",
		"--loop",
		"--strategy", "pr-per-pass",
		"--scope", "backend/",
		"--agent", "gemini",
		"--model", "gemini-2.5-pro",
		"--stream-view", "raw",
		"--wait-buffer-sec", "0",
		"--gh-bin", "gh-enterprise",
	}

	if got := BuildCommandArgs(state); !slices.Equal(got, want) {
		t.Fatalf("args mismatch:\n got: %v\nwant: %v", got, want)
	}
}

func TestBuildCommandArgsImproveInlinePromptParity(t *testing.T) {
	t.Parallel()

	state := CommandState{
		Workflow: WorkflowImprove,
		Improve: ImproveCommandState{
			PromptSource: ImprovePromptSourceInline,
			Prompt:       "custom improve prompt",
		},
	}

	want := []string{
		"improve",
		"--prompt", "custom improve prompt",
	}

	if got := BuildCommandArgs(state); !slices.Equal(got, want) {
		t.Fatalf("args mismatch:\n got: %v\nwant: %v", got, want)
	}
}

func TestBuildCommandArgsImprovePromptFileParity(t *testing.T) {
	t.Parallel()

	state := CommandState{
		Workflow: WorkflowImprove,
		Improve: ImproveCommandState{
			PromptSource: ImprovePromptSourceFile,
			PromptFile:   "prompts/improve.txt",
		},
	}

	want := []string{
		"improve",
		"--prompt-file", "prompts/improve.txt",
	}

	if got := BuildCommandArgs(state); !slices.Equal(got, want) {
		t.Fatalf("args mismatch:\n got: %v\nwant: %v", got, want)
	}
}

func TestBuildCommandArgsUsesOrderedIssuesWhenQueueIsReordered(t *testing.T) {
	t.Parallel()

	state := CommandState{
		Workflow: WorkflowIssues,
		Runtime: CommandRuntime{
			Agent: "codex",
		},
		Issues: IssueCommandState{
			Source:        IssueSourceAllOpen,
			Strategy:      "pr-at-end",
			Label:         "ghir",
			Loop:          true,
			ResolvedQueue: []string{"13", "19", "20"},
			StagedQueue:   []string{"20", "19"},
		},
	}

	want := []string{
		"--issues", "20,19",
		"--strategy", "pr-at-end",
		"--agent", "codex",
	}

	if got := BuildCommandArgs(state); !slices.Equal(got, want) {
		t.Fatalf("args mismatch:\n got: %v\nwant: %v", got, want)
	}
}

func TestBuildCommandArgsUsesOrderedFilesWhenQueueIsSubsetted(t *testing.T) {
	t.Parallel()

	state := CommandState{
		Workflow: WorkflowFiles,
		Files: FileCommandState{
			Source:        FileSourceAllFiles,
			Strategy:      "pr-chain",
			AllFiles:      "tasks",
			ResolvedQueue: []string{"tasks/1.md", "tasks/2.md", "tasks/10.md"},
			StagedQueue:   []string{"tasks/10.md", "tasks/1.md"},
		},
	}

	want := []string{
		"--files", "tasks/10.md,tasks/1.md",
		"--strategy", "pr-chain",
	}

	if got := BuildCommandArgs(state); !slices.Equal(got, want) {
		t.Fatalf("args mismatch:\n got: %v\nwant: %v", got, want)
	}
}

func TestHasStagedQueueOverrideTreatsEmptySubsetAsOverride(t *testing.T) {
	t.Parallel()

	if hasStagedQueueOverride([]string{"13", "19"}, nil) {
		t.Fatalf("expected nil staged queue to keep source semantics")
	}
	if !hasStagedQueueOverride([]string{"13", "19"}, []string{}) {
		t.Fatalf("expected empty staged queue to count as an override")
	}
}

func TestBuildCommandStringIncludesFullInvocation(t *testing.T) {
	t.Parallel()

	state := CommandState{
		Workflow: WorkflowFiles,
		Files: FileCommandState{
			Source:         FileSourceExplicit,
			Files:          []string{"docs/Plan A.md", "docs/B's note.md"},
			PromptTemplate: "templates/agent plan.tmpl",
		},
	}

	want := "ghir --files 'docs/Plan A.md,docs/B'\\''s note.md' --prompt-template 'templates/agent plan.tmpl'"
	if got := BuildCommandString(state); got != want {
		t.Fatalf("command mismatch:\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildCommandArgsIncludesPiRuntimeOptions(t *testing.T) {
	t.Parallel()

	state := CommandState{
		Workflow: WorkflowFiles,
		Runtime: CommandRuntime{
			Agent: "pi",
			Model: "github-copilot/gpt-5.4:high",
			PiBin: "/usr/local/bin/pi",
			GHBin: "/usr/local/bin/gh",
		},
		Files: FileCommandState{
			Source:   FileSourceAllFiles,
			AllFiles: "tasks",
		},
	}

	want := []string{
		"--all-files", "tasks",
		"--agent", "pi",
		"--model", "github-copilot/gpt-5.4:high",
		"--pi-bin", "/usr/local/bin/pi",
		"--gh-bin", "/usr/local/bin/gh",
	}

	if got := BuildCommandArgs(state); !slices.Equal(got, want) {
		t.Fatalf("args mismatch:\n got: %v\nwant: %v", got, want)
	}
}

func TestBuildCommandArgsAreDeterministic(t *testing.T) {
	t.Parallel()

	state := CommandState{
		Workflow: WorkflowImprove,
		Runtime: CommandRuntime{
			Agent: "codex",
			Model: "gpt-5-codex",
		},
		Improve: ImproveCommandState{
			Mode:     "cleanup",
			Strategy: "direct",
		},
	}

	first := BuildCommandArgs(state)
	first[0] = "mutated"
	second := BuildCommandArgs(state)
	want := []string{"improve", "--agent", "codex", "--model", "gpt-5-codex"}

	if !slices.Equal(second, want) {
		t.Fatalf("args mismatch after repeated builds:\n got: %v\nwant: %v", second, want)
	}
}
