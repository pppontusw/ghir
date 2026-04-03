package tui

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestSaveAndLoadNamedPresetRoundTrip(t *testing.T) {
	repo := initGitRepo(t)
	t.Setenv("HOME", repo)
	waitBuffer := 45
	iterations := 3

	state := CommandState{
		Workflow: WorkflowIssues,
		Runtime: CommandRuntime{
			Agent:         "codex",
			Model:         "gpt-5-codex",
			StreamView:    "raw",
			WaitBufferSec: &waitBuffer,
			NoColor:       true,
			CodexBin:      "codex-custom",
			GHBin:         "gh-custom",
		},
		Issues: IssueCommandState{
			Source:          IssueSourceAllOpen,
			Strategy:        "pr-per-pass",
			Label:           "ghir",
			DryRun:          true,
			Force:           true,
			ContinueOnError: true,
			Loop:            true,
			PromptTemplate:  ".ticket-runner/custom.tmpl",
			LogDir:          ".runs",
			DoneFile:        ".runs/done.txt",
		},
		Improve: ImproveCommandState{
			Mode:       "docs",
			Strategy:   "pr-chain",
			Iterations: &iterations,
		},
	}

	if _, err := saveNamedPreset(repo, "daily", state); err != nil {
		t.Fatalf("saveNamedPreset returned unexpected error: %v", err)
	}

	catalog, err := loadPresetCatalog(repo)
	if err != nil {
		t.Fatalf("loadPresetCatalog returned unexpected error: %v", err)
	}
	if got, want := catalog.Path, filepath.Join(repo, presetRelativePath); got != want {
		t.Fatalf("preset path mismatch: got %q want %q", got, want)
	}
	if got, want := len(catalog.Presets), 1; got != want {
		t.Fatalf("preset count mismatch: got %d want %d", got, want)
	}

	preset := catalog.Presets[0]
	if got, want := preset.Name, "daily"; got != want {
		t.Fatalf("preset name mismatch: got %q want %q", got, want)
	}
	if got, want := preset.State.Workflow, WorkflowIssues; got != want {
		t.Fatalf("workflow mismatch: got %q want %q", got, want)
	}
	if got, want := preset.State.Issues.Source, IssueSourceAllOpen; got != want {
		t.Fatalf("issue source mismatch: got %q want %q", got, want)
	}
	if got, want := preset.State.Issues.Label, "ghir"; got != want {
		t.Fatalf("label mismatch: got %q want %q", got, want)
	}
	if got, want := preset.State.Runtime.Agent, "codex"; got != want {
		t.Fatalf("agent mismatch: got %q want %q", got, want)
	}
	if got, want := preset.State.Runtime.Model, "gpt-5-codex"; got != want {
		t.Fatalf("model mismatch: got %q want %q", got, want)
	}
	if got, want := preset.State.Runtime.StreamView, "raw"; got != want {
		t.Fatalf("stream view mismatch: got %q want %q", got, want)
	}
	if preset.State.Runtime.WaitBufferSec == nil || *preset.State.Runtime.WaitBufferSec != 45 {
		t.Fatalf("wait buffer mismatch: got %#v want 45", preset.State.Runtime.WaitBufferSec)
	}
	if !preset.State.Issues.ContinueOnError || !preset.State.Issues.Loop {
		t.Fatalf("expected issue boolean fields to round-trip, got %#v", preset.State.Issues)
	}
	if got, want := preset.State.Issues.Strategy, "pr-per-pass"; got != want {
		t.Fatalf("issue strategy mismatch: got %q want %q", got, want)
	}

	data, err := os.ReadFile(filepath.Join(repo, presetRelativePath))
	if err != nil {
		t.Fatalf("read preset file: %v", err)
	}
	for _, needle := range []string{
		`"version": 1`,
		`"name": "daily"`,
		`"workflow": "issues"`,
		`"source": "all-open"`,
		`"agent": "codex"`,
	} {
		if !strings.Contains(string(data), needle) {
			t.Fatalf("expected preset file to contain %q, got:\n%s", needle, string(data))
		}
	}
}

func TestLoadPresetCatalogRejectsInvalidSchema(t *testing.T) {
	repo := initGitRepo(t)
	t.Setenv("HOME", repo)
	path := filepath.Join(repo, presetRelativePath)
	writeRepoFile(t, path, `{"version":2,"presets":[]}`)

	_, err := loadPresetCatalog(repo)
	if err == nil {
		t.Fatal("expected invalid preset file error")
	}
	var invalid invalidPresetFileError
	if !errors.As(err, &invalid) {
		t.Fatalf("expected invalidPresetFileError, got %T (%v)", err, err)
	}
	if !strings.Contains(err.Error(), "unsupported version 2") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestSaveNamedPresetRecoversInvalidFileNonDestructively(t *testing.T) {
	repo := initGitRepo(t)
	t.Setenv("HOME", repo)
	path := filepath.Join(repo, presetRelativePath)
	writeRepoFile(t, path, `{"version":1,"presets":[`)

	state := defaultConfigureCommandState("files", "claude")
	state.Files.Source = FileSourceAllFiles
	state.Files.AllFiles = "tasks"

	result, err := saveNamedPreset(repo, "repair", state)
	if err != nil {
		t.Fatalf("saveNamedPreset returned unexpected error: %v", err)
	}
	if result.RecoveredPath == "" {
		t.Fatal("expected invalid preset file to be moved aside")
	}

	recovered, err := os.ReadFile(result.RecoveredPath)
	if err != nil {
		t.Fatalf("read recovered preset file: %v", err)
	}
	if string(recovered) != `{"version":1,"presets":[` {
		t.Fatalf("recovered preset file mismatch: got %q", string(recovered))
	}

	catalog, err := loadPresetCatalog(repo)
	if err != nil {
		t.Fatalf("loadPresetCatalog returned unexpected error after recovery: %v", err)
	}
	if got, want := len(catalog.Presets), 1; got != want {
		t.Fatalf("preset count mismatch: got %d want %d", got, want)
	}
	if got, want := catalog.Presets[0].Name, "repair"; got != want {
		t.Fatalf("preset name mismatch: got %q want %q", got, want)
	}
}

func TestLoadPresetCatalogFallsBackToRepoLocalPresetFile(t *testing.T) {
	repo := initGitRepo(t)
	home := t.TempDir()
	t.Setenv("HOME", home)
	writeRepoFile(t, filepath.Join(repo, presetRelativePath), `{
  "version": 1,
  "presets": [
    {
      "name": "legacy",
      "workflow": "files",
      "fields": {
        "source": "all-files",
        "all_files": "tasks"
      }
    }
  ]
}`)

	catalog, err := loadPresetCatalog(repo)
	if err != nil {
		t.Fatalf("loadPresetCatalog returned unexpected error: %v", err)
	}
	if got, want := catalog.Path, filepath.Join(repo, presetRelativePath); got != want {
		t.Fatalf("legacy preset path mismatch: got %q want %q", got, want)
	}
	if got, want := len(catalog.Presets), 1; got != want {
		t.Fatalf("preset count mismatch: got %d want %d", got, want)
	}
	if got, want := catalog.Presets[0].Name, "legacy"; got != want {
		t.Fatalf("preset name mismatch: got %q want %q", got, want)
	}
}

func TestSaveAndLoadImproveCustomPromptPresetRoundTrip(t *testing.T) {
	repo := initGitRepo(t)
	t.Setenv("HOME", repo)
	iterations := 2

	state := CommandState{
		Workflow: WorkflowImprove,
		Runtime: CommandRuntime{
			Agent: "claude",
		},
		Improve: ImproveCommandState{
			PromptSource: ImprovePromptSourceFile,
			PromptFile:   "prompts/improve.txt",
			Iterations:   &iterations,
			Strategy:     "pr-per-pass",
			Scope:        "backend/",
		},
	}

	if _, err := saveNamedPreset(repo, "improve-custom", state); err != nil {
		t.Fatalf("saveNamedPreset returned unexpected error: %v", err)
	}

	catalog, err := loadPresetCatalog(repo)
	if err != nil {
		t.Fatalf("loadPresetCatalog returned unexpected error: %v", err)
	}
	if got, want := len(catalog.Presets), 1; got != want {
		t.Fatalf("preset count mismatch: got %d want %d", got, want)
	}

	preset := catalog.Presets[0]
	if got, want := preset.State.Improve.PromptSource, ImprovePromptSourceFile; got != want {
		t.Fatalf("prompt source mismatch: got %q want %q", got, want)
	}
	if got, want := preset.State.Improve.PromptFile, "prompts/improve.txt"; got != want {
		t.Fatalf("prompt file mismatch: got %q want %q", got, want)
	}
	if got, want := preset.State.Improve.Strategy, "pr-per-pass"; got != want {
		t.Fatalf("strategy mismatch: got %q want %q", got, want)
	}
}
