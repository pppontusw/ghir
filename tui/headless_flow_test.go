package tui

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
)

func TestHeadlessConfigureRunSummaryFlowIssues(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	events := make(chan tea.Msg, 8)
	var request RunRequest

	model := NewModel(Options{
		RepoRoot:      repo,
		Branch:        "main",
		Workflow:      "issues",
		NoColor:       true,
		RunExecutable: "/tmp/ghir-test",
		StartRun: func(req RunRequest) (runHandle, error) {
			request = req
			return runHandle{
				Events: events,
				Stop: func() error {
					return nil
				},
			}, nil
		},
	})

	state := CommandState{
		Workflow: WorkflowIssues,
		Runtime: CommandRuntime{
			Agent:      "claude",
			ClaudeBin:  "go",
			GHBin:      "go",
			StreamView: "raw",
		},
		Issues: IssueCommandState{
			Source:        IssueSourceCSV,
			Issues:        []string{"13", "19"},
			ResolvedQueue: []string{"13", "19"},
			StagedQueue:   []string{"13", "19"},
			DryRun:        true,
		},
	}

	updated, _ := model.Update(ConfigureStateMsg{State: state})
	model = updated.(Model)

	updated, cmd := model.Update(keyRunes("r"))
	model = updated.(Model)
	if cmd == nil {
		t.Fatalf("expected run listener command")
	}
	if got, want := request.Args, BuildCommandArgs(model.command); !slices.Equal(got, want) {
		t.Fatalf("run args mismatch:\n got: %v\nwant: %v", got, want)
	}

	model, cmd = stepRunEvent(t, model, cmd, events, runProcessLineMsg{Line: "[1/2] #13: First issue"})
	model, cmd = stepRunEvent(t, model, cmd, events, runProcessLineMsg{Line: "Log: .ticket-runs/13.log"})
	model, cmd = stepRunEvent(t, model, cmd, events, runProcessLineMsg{Line: "[DRY RUN] Would process #13"})
	model, cmd = stepRunEvent(t, model, cmd, events, runProcessLineMsg{Line: "[2/2] #19: Second issue"})
	model, cmd = stepRunEvent(t, model, cmd, events, runProcessLineMsg{Line: "Log: .ticket-runs/19.log"})
	model, cmd = stepRunEvent(t, model, cmd, events, runProcessLineMsg{Line: "[DRY RUN] Would process #19"})
	model, _ = stepRunEvent(t, model, cmd, events, runProcessExitMsg{ExitCode: 0})

	if got, want := model.Phase(), PhaseSummary; got != want {
		t.Fatalf("phase mismatch after run completion: got %q want %q", got, want)
	}
	if got, want := model.summary.succeeded, 2; got != want {
		t.Fatalf("summary succeeded mismatch: got %d want %d", got, want)
	}
	if got, want := model.summary.failed, 0; got != want {
		t.Fatalf("summary failed mismatch: got %d want %d", got, want)
	}
	if got, want := model.summary.lastLogs, []string{".ticket-runs/13.log", ".ticket-runs/19.log"}; !slices.Equal(got, want) {
		t.Fatalf("summary logs mismatch:\n got: %v\nwant: %v", got, want)
	}
	view := model.View()
	for _, needle := range []string{"Workflow: Issues", "Succeeded: 2", "Failed: 0", ".ticket-runs/19.log"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("expected summary view to contain %q, got:\n%s", needle, view)
		}
	}
}

func TestHeadlessConfigureRunSummaryFlowImprove(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	writeRepoFile(t, filepath.Join(repo, "README.md"), "fixture\n")
	runGit(t, repo, "add", "README.md")
	runGit(t, repo, "commit", "-m", "add fixture")

	events := make(chan tea.Msg, 8)
	var request RunRequest
	iterations := 2

	model := NewModel(Options{
		RepoRoot:      repo,
		Branch:        "main",
		Workflow:      "improve",
		NoColor:       true,
		RunExecutable: "/tmp/ghir-test",
		StartRun: func(req RunRequest) (runHandle, error) {
			request = req
			return runHandle{
				Events: events,
				Stop: func() error {
					return nil
				},
			}, nil
		},
	})

	state := CommandState{
		Workflow: WorkflowImprove,
		Runtime: CommandRuntime{
			Agent:      "codex",
			CodexBin:   "go",
			GHBin:      "go",
			NoColor:    true,
			StreamView: "raw",
		},
		Improve: ImproveCommandState{
			Mode:       "cleanup",
			Iterations: &iterations,
			Strategy:   "direct",
		},
	}

	updated, _ := model.Update(ConfigureStateMsg{State: state})
	model = updated.(Model)

	updated, cmd := model.Update(keyRunes("r"))
	model = updated.(Model)
	if cmd == nil {
		t.Fatalf("expected run listener command")
	}
	if got, want := request.Args, BuildCommandArgs(model.command); !slices.Equal(got, want) {
		t.Fatalf("run args mismatch:\n got: %v\nwant: %v", got, want)
	}

	model, cmd = stepRunEvent(t, model, cmd, events, runProcessLineMsg{Line: "Starting improve pass 1 (cleanup) with codex..."})
	model, cmd = stepRunEvent(t, model, cmd, events, runProcessLineMsg{Line: "Log: .ticket-runs/improve-pass-1.log"})
	model, cmd = stepRunEvent(t, model, cmd, events, runProcessLineMsg{Line: "Improvement pass 1 (cleanup) committed by runner"})
	model, cmd = stepRunEvent(t, model, cmd, events, runProcessLineMsg{Line: "Starting improve pass 2 (cleanup) with codex..."})
	model, cmd = stepRunEvent(t, model, cmd, events, runProcessLineMsg{Line: "Log: .ticket-runs/improve-pass-2.log"})
	model, cmd = stepRunEvent(t, model, cmd, events, runProcessLineMsg{Line: "FAILED: codex exited with code 1 for cleanup pass 2"})
	model, _ = stepRunEvent(t, model, cmd, events, runProcessExitMsg{ExitCode: 1, Err: errors.New("exit status 1")})

	if got, want := model.Phase(), PhaseSummary; got != want {
		t.Fatalf("phase mismatch after run completion: got %q want %q", got, want)
	}
	if got, want := model.summary.succeeded, 1; got != want {
		t.Fatalf("summary succeeded mismatch: got %d want %d", got, want)
	}
	if got, want := model.summary.failed, 1; got != want {
		t.Fatalf("summary failed mismatch: got %d want %d", got, want)
	}
	if got, want := model.summary.failedItems, []string{"cleanup pass 2"}; !slices.Equal(got, want) {
		t.Fatalf("summary failed items mismatch:\n got: %v\nwant: %v", got, want)
	}
	view := model.View()
	for _, needle := range []string{"Workflow: Improve", "Succeeded: 1", "Failed: 1", "cleanup pass 2", ".ticket-runs/improve-pass-2.log"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("expected summary view to contain %q, got:\n%s", needle, view)
		}
	}
	if handled, cmd := model.summary.handleKey(&model, keyRunes("r")); !handled || cmd != nil {
		t.Fatalf("expected improve rerun action to be handled without starting a run")
	}
	if !strings.Contains(model.summary.actionStatus, "Rerun failed subset is unavailable for Improve runs.") {
		t.Fatalf("expected improve rerun warning, got %q", model.summary.actionStatus)
	}
}
