package main

import (
	"testing"

	appshell "ghir/tui"
)

func TestTUIBuildCommandArgsParseIssuesWorkflow(t *testing.T) {
	t.Parallel()

	args := appshell.BuildCommandArgs(appshell.CommandState{
		Workflow: appshell.WorkflowIssues,
		Runtime: appshell.CommandRuntime{
			Agent: "codex",
		},
		Issues: appshell.IssueCommandState{
			Source:        appshell.IssueSourceAllOpen,
			Strategy:      "pr-per-pass",
			Label:         "ghir",
			ResolvedQueue: []string{"13", "19", "20"},
			StagedQueue:   []string{"20", "19"},
		},
	})

	opts, err := parseArgs(args)
	if err != nil {
		t.Fatalf("parseArgs returned unexpected error: %v", err)
	}
	if opts.IssuesCSV != "20,19" {
		t.Fatalf("issues csv mismatch: got %q want %q", opts.IssuesCSV, "20,19")
	}
	if opts.Strategy != "pr-per-pass" {
		t.Fatalf("strategy mismatch: got %q want %q", opts.Strategy, "pr-per-pass")
	}
	if opts.AllOpen {
		t.Fatalf("expected staged queue to map away from --all-open")
	}
	if opts.Agent != "codex" {
		t.Fatalf("agent mismatch: got %q want %q", opts.Agent, "codex")
	}
}

func TestTUIBuildCommandArgsParseFilesWorkflow(t *testing.T) {
	t.Parallel()

	args := appshell.BuildCommandArgs(appshell.CommandState{
		Workflow: appshell.WorkflowFiles,
		Files: appshell.FileCommandState{
			Source:        appshell.FileSourceAllFiles,
			Strategy:      "pr-chain",
			AllFiles:      "tasks",
			ResolvedQueue: []string{"tasks/1.md", "tasks/2.md", "tasks/10.md"},
			StagedQueue:   []string{"tasks/10.md", "tasks/1.md"},
		},
	})

	opts, err := parseArgs(args)
	if err != nil {
		t.Fatalf("parseArgs returned unexpected error: %v", err)
	}
	if opts.Files != "tasks/10.md,tasks/1.md" {
		t.Fatalf("files mismatch: got %q want %q", opts.Files, "tasks/10.md,tasks/1.md")
	}
	if opts.Strategy != "pr-chain" {
		t.Fatalf("strategy mismatch: got %q want %q", opts.Strategy, "pr-chain")
	}
	if opts.AllFiles != "" {
		t.Fatalf("expected staged queue to map away from --all-files")
	}
}

func TestTUIBuildCommandArgsParseImproveWorkflow(t *testing.T) {
	t.Parallel()

	iterations := 2
	waitBuffer := 0
	args := appshell.BuildCommandArgs(appshell.CommandState{
		Workflow: appshell.WorkflowImprove,
		Runtime: appshell.CommandRuntime{
			Agent:         "gemini",
			Model:         "gemini-2.5-pro",
			StreamView:    "raw",
			WaitBufferSec: &waitBuffer,
		},
		Improve: appshell.ImproveCommandState{
			Mode:       "security",
			Iterations: &iterations,
			Loop:       true,
			Strategy:   "pr-per-pass",
			Scope:      "backend/",
		},
	})

	if len(args) == 0 || args[0] != "improve" {
		t.Fatalf("expected improve subcommand prefix, got %v", args)
	}

	opts, err := parseImproveArgs(args[1:])
	if err != nil {
		t.Fatalf("parseImproveArgs returned unexpected error: %v", err)
	}
	if opts.Mode != "security" {
		t.Fatalf("mode mismatch: got %q want %q", opts.Mode, "security")
	}
	if opts.Iterations != 2 {
		t.Fatalf("iterations mismatch: got %d want %d", opts.Iterations, 2)
	}
	if !opts.Loop {
		t.Fatalf("expected loop flag to be set")
	}
	if opts.Strategy != "pr-per-pass" {
		t.Fatalf("strategy mismatch: got %q want %q", opts.Strategy, "pr-per-pass")
	}
	if opts.Scope != "backend/" {
		t.Fatalf("scope mismatch: got %q want %q", opts.Scope, "backend/")
	}
	if opts.Agent != "gemini" {
		t.Fatalf("agent mismatch: got %q want %q", opts.Agent, "gemini")
	}
	if opts.Model != "gemini-2.5-pro" {
		t.Fatalf("model mismatch: got %q want %q", opts.Model, "gemini-2.5-pro")
	}
	if opts.StreamView != "raw" {
		t.Fatalf("stream view mismatch: got %q want %q", opts.StreamView, "raw")
	}
	if opts.WaitBufferSec != 0 {
		t.Fatalf("wait buffer mismatch: got %d want %d", opts.WaitBufferSec, 0)
	}
}

func TestTUIBuildCommandArgsParseFilesWorkflowRuntimeParity(t *testing.T) {
	t.Parallel()

	waitBuffer := 45
	args := appshell.BuildCommandArgs(appshell.CommandState{
		Workflow: appshell.WorkflowFiles,
		Runtime: appshell.CommandRuntime{
			Agent:         "codex",
			Model:         "gpt-5",
			StreamView:    "raw",
			WaitBufferSec: &waitBuffer,
			NoColor:       true,
			CodexBin:      "/usr/local/bin/codex",
		},
		Files: appshell.FileCommandState{
			Source:          appshell.FileSourceAllFiles,
			AllFiles:        "tasks",
			DryRun:          true,
			ContinueOnError: true,
			PromptTemplate:  "templates/review.tmpl",
			LogDir:          ".ticket-runs/custom",
			DoneFile:        ".ticket-runs/custom/.completed",
		},
	})

	opts, err := parseArgs(args)
	if err != nil {
		t.Fatalf("parseArgs returned unexpected error: %v", err)
	}
	if opts.AllFiles != "tasks" {
		t.Fatalf("all-files mismatch: got %q want %q", opts.AllFiles, "tasks")
	}
	if !opts.DryRun {
		t.Fatalf("expected dry-run flag to be set")
	}
	if !opts.ContinueOnError {
		t.Fatalf("expected continue-on-error flag to be set")
	}
	if opts.PromptTemplate != "templates/review.tmpl" {
		t.Fatalf("prompt template mismatch: got %q want %q", opts.PromptTemplate, "templates/review.tmpl")
	}
	if opts.LogDir != ".ticket-runs/custom" {
		t.Fatalf("log dir mismatch: got %q want %q", opts.LogDir, ".ticket-runs/custom")
	}
	if opts.DoneFile != ".ticket-runs/custom/.completed" {
		t.Fatalf("done file mismatch: got %q want %q", opts.DoneFile, ".ticket-runs/custom/.completed")
	}
	if opts.Agent != "codex" || opts.Model != "gpt-5" || opts.StreamView != "raw" || opts.WaitBufferSec != 45 || !opts.NoColor {
		t.Fatalf("runtime mismatch: got agent=%q model=%q stream=%q wait=%d no_color=%v", opts.Agent, opts.Model, opts.StreamView, opts.WaitBufferSec, opts.NoColor)
	}
	if opts.CodexBin != "/usr/local/bin/codex" {
		t.Fatalf("codex bin mismatch: got %q want %q", opts.CodexBin, "/usr/local/bin/codex")
	}
}

func TestTUIBuildCommandArgsParsePiRuntimeParity(t *testing.T) {
	t.Parallel()

	args := appshell.BuildCommandArgs(appshell.CommandState{
		Workflow: appshell.WorkflowFiles,
		Runtime: appshell.CommandRuntime{
			Agent: "pi",
			Model: "github-copilot/gpt-5.4:high",
			PiBin: "/usr/local/bin/pi",
			GHBin: "/usr/local/bin/gh",
		},
		Files: appshell.FileCommandState{
			Source:   appshell.FileSourceAllFiles,
			AllFiles: "tasks",
		},
	})

	opts, err := parseArgs(args)
	if err != nil {
		t.Fatalf("parseArgs returned unexpected error: %v", err)
	}
	if opts.Agent != "pi" {
		t.Fatalf("agent mismatch: got %q want %q", opts.Agent, "pi")
	}
	if opts.Model != "github-copilot/gpt-5.4:high" {
		t.Fatalf("model mismatch: got %q want %q", opts.Model, "github-copilot/gpt-5.4:high")
	}
	if opts.PiBin != "/usr/local/bin/pi" {
		t.Fatalf("pi bin mismatch: got %q want %q", opts.PiBin, "/usr/local/bin/pi")
	}
	if opts.GHBin != "/usr/local/bin/gh" {
		t.Fatalf("gh bin mismatch: got %q want %q", opts.GHBin, "/usr/local/bin/gh")
	}
}

func TestTUIBuildCommandArgsParseStagedIssueSubsetDropsAllOpenSemantics(t *testing.T) {
	t.Parallel()

	args := appshell.BuildCommandArgs(appshell.CommandState{
		Workflow: appshell.WorkflowIssues,
		Runtime: appshell.CommandRuntime{
			Agent: "codex",
		},
		Issues: appshell.IssueCommandState{
			Source:        appshell.IssueSourceAllOpen,
			Label:         "ghir",
			Loop:          true,
			ResolvedQueue: []string{"13", "19", "20"},
			StagedQueue:   []string{"20", "19"},
		},
	})

	opts, err := parseArgs(args)
	if err != nil {
		t.Fatalf("parseArgs returned unexpected error: %v", err)
	}
	if opts.IssuesCSV != "20,19" {
		t.Fatalf("issues csv mismatch: got %q want %q", opts.IssuesCSV, "20,19")
	}
	if opts.AllOpen {
		t.Fatalf("expected staged queue to map away from --all-open")
	}
	if opts.Label != "" {
		t.Fatalf("expected label to be omitted after staged rewrite, got %q", opts.Label)
	}
	if opts.Loop {
		t.Fatalf("expected loop to be omitted after staged rewrite")
	}
}

func TestTUIBuildCommandArgsParseImproveDefaultsParity(t *testing.T) {
	t.Parallel()

	args := appshell.BuildCommandArgs(appshell.CommandState{
		Workflow: appshell.WorkflowImprove,
		Runtime: appshell.CommandRuntime{
			Agent: "claude",
		},
		Improve: appshell.ImproveCommandState{},
	})

	if len(args) == 0 || args[0] != "improve" {
		t.Fatalf("expected improve subcommand prefix, got %v", args)
	}

	opts, err := parseImproveArgs(args[1:])
	if err != nil {
		t.Fatalf("parseImproveArgs returned unexpected error: %v", err)
	}
	if opts.Mode != "cleanup" {
		t.Fatalf("mode mismatch: got %q want %q", opts.Mode, "cleanup")
	}
	if opts.Iterations != 1 {
		t.Fatalf("iterations mismatch: got %d want %d", opts.Iterations, 1)
	}
	if opts.Strategy != "direct" {
		t.Fatalf("strategy mismatch: got %q want %q", opts.Strategy, "direct")
	}
	if opts.Agent != "claude" {
		t.Fatalf("agent mismatch: got %q want %q", opts.Agent, "claude")
	}
}

func TestTUIBuildCommandArgsParseImproveInlinePromptWorkflow(t *testing.T) {
	t.Parallel()

	args := appshell.BuildCommandArgs(appshell.CommandState{
		Workflow: appshell.WorkflowImprove,
		Improve: appshell.ImproveCommandState{
			PromptSource: appshell.ImprovePromptSourceInline,
			Prompt:       "custom improve prompt",
		},
	})

	opts, err := parseImproveArgs(args[1:])
	if err != nil {
		t.Fatalf("parseImproveArgs returned unexpected error: %v", err)
	}
	if opts.Prompt != "custom improve prompt" {
		t.Fatalf("prompt mismatch: got %q want %q", opts.Prompt, "custom improve prompt")
	}
}

func TestTUIBuildCommandArgsParseImprovePromptFileWorkflow(t *testing.T) {
	t.Parallel()

	args := appshell.BuildCommandArgs(appshell.CommandState{
		Workflow: appshell.WorkflowImprove,
		Improve: appshell.ImproveCommandState{
			PromptSource: appshell.ImprovePromptSourceFile,
			PromptFile:   "prompts/improve.txt",
		},
	})

	opts, err := parseImproveArgs(args[1:])
	if err != nil {
		t.Fatalf("parseImproveArgs returned unexpected error: %v", err)
	}
	if opts.PromptFile != "prompts/improve.txt" {
		t.Fatalf("prompt file mismatch: got %q want %q", opts.PromptFile, "prompts/improve.txt")
	}
}
