package tui

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
)

func TestPhaseTransitionsCycle(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 7, 10, 0, 0, 0, time.UTC)
	model := NewModel(Options{
		RepoRoot: "/tmp/ghir",
		Branch:   "feature/tui-shell",
		Workflow: "files",
		NoColor:  true,
		Now: func() time.Time {
			return now
		},
	})

	updated, _ := model.Update(TransitionMsg{Phase: PhaseRun})
	runModel := updated.(Model)
	if runModel.Phase() != PhaseRun {
		t.Fatalf("phase mismatch after run transition: got %q want %q", runModel.Phase(), PhaseRun)
	}

	updated, _ = runModel.Update(TransitionMsg{Phase: PhaseSummary})
	summaryModel := updated.(Model)
	if summaryModel.Phase() != PhaseSummary {
		t.Fatalf("phase mismatch after summary transition: got %q want %q", summaryModel.Phase(), PhaseSummary)
	}

	updated, _ = summaryModel.Update(TransitionMsg{Phase: PhaseConfigure})
	configureModel := updated.(Model)
	if configureModel.Phase() != PhaseConfigure {
		t.Fatalf("phase mismatch after configure transition: got %q want %q", configureModel.Phase(), PhaseConfigure)
	}
}

func TestRunKeyBlockedByValidationErrors(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	model := NewModel(Options{
		RepoRoot: repo,
		Branch:   "main",
		Workflow: "issues",
		NoColor:  true,
	})

	updated, _ := model.Update(ConfigureStateMsg{
		State: CommandState{
			Workflow: WorkflowIssues,
			Runtime: CommandRuntime{
				Agent:     "claude",
				ClaudeBin: "go",
				GHBin:     "go",
			},
			Issues: IssueCommandState{
				Source:      IssueSourceSingle,
				SingleIssue: "abc",
			},
		},
	})
	blocked := updated.(Model)
	if !blocked.validation.HasErrors() {
		t.Fatalf("expected validation error before run")
	}

	updated, _ = blocked.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	blocked = updated.(Model)
	if blocked.Phase() != PhaseConfigure {
		t.Fatalf("expected configure phase to remain active, got %q", blocked.Phase())
	}
	if blocked.runBlocked == "" {
		t.Fatalf("expected blocked run message to be set")
	}
	if !reportContains(blocked.validation, `--issue must be numeric: "abc"`) {
		t.Fatalf("expected validation error in report, got %#v", blocked.validation.Items())
	}
}

func TestRunKeyTransitionsWhenValidationAndPreflightPass(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	writeRepoFile(t, filepath.Join(repo, "tasks", "1.md"), "one\n")
	model := NewModel(Options{
		RepoRoot: repo,
		Branch:   "main",
		Workflow: "files",
		NoColor:  true,
	})

	updated, _ := model.Update(ConfigureStateMsg{
		State: CommandState{
			Workflow: WorkflowFiles,
			Runtime: CommandRuntime{
				Agent:     "claude",
				ClaudeBin: "go",
			},
			Files: FileCommandState{
				Source:   FileSourceAllFiles,
				AllFiles: "tasks",
				DryRun:   true,
			},
		},
	})
	ready := updated.(Model)
	if !ready.canRun() {
		t.Fatalf("expected valid configure state, got validation=%#v preflight=%#v", ready.validation.Items(), ready.preflight.Items())
	}

	updated, cmd := ready.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	running := updated.(Model)
	if cmd != nil {
		updated, _ = running.Update(cmd())
		running = updated.(Model)
	}
	if running.Phase() != PhaseRun {
		t.Fatalf("expected run phase after valid run request, got %q", running.Phase())
	}
}

func TestConfigureStatePreservedAfterReturningFromRun(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	model := NewModel(Options{
		RepoRoot: repo,
		Branch:   "main",
		Workflow: "files",
		NoColor:  true,
	})

	state := CommandState{
		Workflow: WorkflowFiles,
		Runtime: CommandRuntime{
			Agent:     "claude",
			ClaudeBin: "go",
		},
		Files: FileCommandState{
			Source:   FileSourceAllFiles,
			AllFiles: "tasks/custom",
		},
	}

	updated, _ := model.Update(ConfigureStateMsg{State: state})
	configured := updated.(Model)
	updated, cmd := configured.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	running := updated.(Model)
	if cmd != nil {
		updated, _ = running.Update(cmd())
		running = updated.(Model)
	}
	updated, cmd = running.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'b'}})
	configure := updated.(Model)
	if cmd != nil {
		updated, _ = configure.Update(cmd())
		configure = updated.(Model)
	}

	if configure.Phase() != PhaseConfigure {
		t.Fatalf("expected configure phase after back navigation, got %q", configure.Phase())
	}
	if configure.command.Files.AllFiles != "tasks/custom" {
		t.Fatalf("expected configure state to be preserved, got %#v", configure.command.Files)
	}
	if !strings.Contains(configure.View(), "tasks/custom") {
		t.Fatalf("expected preserved state in configure view, got:\n%s", configure.View())
	}
}

func TestShellViewIncludesPersistentRegions(t *testing.T) {
	t.Parallel()

	view := Snapshot(Options{
		RepoRoot: "/work/ghir",
		Branch:   "TUI",
		Workflow: "files",
		Preset:   "daily",
		Agent:    "codex",
		NoColor:  true,
	}, 120, 36)

	for _, needle := range []string{
		"[GHIR TUI]",
		"Phase Configure",
		"Repo ghir",
		"Branch TUI",
		"Run Setup",
		"Preset:",
		"Q Quit",
	} {
		if !strings.Contains(view, needle) {
			t.Fatalf("expected snapshot to contain %q, got:\n%s", needle, view)
		}
	}
}

func TestBootstrapPresetLoadsStateAtStartup(t *testing.T) {
	repo := initGitRepo(t)
	t.Setenv("HOME", repo)
	writeRepoFile(t, filepath.Join(repo, "tasks", "daily", "1.md"), "daily\n")
	runGit(t, repo, "add", "tasks/daily/1.md")
	runGit(t, repo, "commit", "-m", "add daily queue")
	writeRepoFile(t, filepath.Join(repo, presetRelativePath), `{
  "version": 1,
  "presets": [
    {
      "name": "daily",
      "workflow": "files",
      "fields": {
        "source": "all-files",
        "all_files": "tasks/daily",
        "agent": "codex",
        "stream_view": "raw",
        "wait_buffer_sec": 33,
        "no_color": true
      }
    }
  ]
}`)
	runGit(t, repo, "add", ".ticket-runner/tui-presets.json")
	runGit(t, repo, "commit", "-m", "add preset")

	model := NewModel(Options{
		RepoRoot: repo,
		Branch:   "main",
		Workflow: "issues",
		Preset:   "daily",
		NoColor:  true,
	})

	if got, want := model.command.Workflow, WorkflowFiles; got != want {
		t.Fatalf("workflow mismatch: got %q want %q", got, want)
	}
	if got, want := model.command.Files.Source, FileSourceAllFiles; got != want {
		t.Fatalf("file source mismatch: got %q want %q", got, want)
	}
	if got, want := model.command.Files.AllFiles, "tasks/daily"; got != want {
		t.Fatalf("all-files mismatch: got %q want %q", got, want)
	}
	if got, want := model.command.Runtime.Agent, "codex"; got != want {
		t.Fatalf("agent mismatch: got %q want %q", got, want)
	}
	if model.command.Runtime.WaitBufferSec == nil || *model.command.Runtime.WaitBufferSec != 33 {
		t.Fatalf("wait buffer mismatch: got %#v want 33", model.command.Runtime.WaitBufferSec)
	}
	if got, want := model.presets.active, "daily"; got != want {
		t.Fatalf("active preset mismatch: got %q want %q", got, want)
	}
	view := model.View()
	for _, needle := range []string{
		"Preset: daily",
		"tasks/daily",
		"Preset status: Preset \"daily\"",
		"loaded from",
	} {
		if !strings.Contains(view, needle) {
			t.Fatalf("expected startup view to contain %q, got:\n%s", needle, view)
		}
	}
}

func TestConfigurePresetSaveAndLoadAcrossSessions(t *testing.T) {
	repo := initGitRepo(t)
	t.Setenv("HOME", repo)
	writeRepoFile(t, filepath.Join(repo, "tasks", "1.md"), "one\n")

	model := NewModel(Options{
		RepoRoot: repo,
		Branch:   "main",
		Workflow: "files",
		NoColor:  true,
	})

	model = focusConfigureField(t, model, fieldRuntimeAgent)
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRight})
	if got, want := model.command.Runtime.Agent, "codex"; got != want {
		t.Fatalf("agent mismatch after edit: got %q want %q", got, want)
	}

	model = updateModel(t, model, keyRunes("s"))
	if got, want := model.configure.overlay, configureOverlaySavePreset; got != want {
		t.Fatalf("expected save overlay after S, got %q", got)
	}
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyDelete})
	model = updateModel(t, model, keyRunes("daily"))
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if got, want := model.presets.active, "daily"; got != want {
		t.Fatalf("active preset mismatch after save: got %q want %q", got, want)
	}
	if !strings.Contains(model.View(), `Preset status: Preset "daily" saved`) {
		t.Fatalf("expected save status in view, got:\n%s", model.View())
	}

	reloaded := NewModel(Options{
		RepoRoot: repo,
		Branch:   "main",
		Workflow: "issues",
		NoColor:  true,
	})
	if got, want := reloaded.presets.selection, "daily"; got != want {
		t.Fatalf("selection mismatch after reload: got %q want %q", got, want)
	}

	reloaded = updateModel(t, reloaded, keyRunes("L"))
	if got, want := reloaded.configure.overlay, configureOverlayLoadPreset; got != want {
		t.Fatalf("expected load overlay after L, got %q", got)
	}
	reloaded = updateModel(t, reloaded, tea.KeyMsg{Type: tea.KeyEnter})

	if got, want := reloaded.command.Workflow, WorkflowFiles; got != want {
		t.Fatalf("workflow mismatch after preset load: got %q want %q", got, want)
	}
	if got, want := reloaded.command.Runtime.Agent, "codex"; got != want {
		t.Fatalf("agent mismatch after preset load: got %q want %q", got, want)
	}
	if got, want := reloaded.presets.active, "daily"; got != want {
		t.Fatalf("active preset mismatch after load: got %q want %q", got, want)
	}
}

func TestInvalidPresetFileRemainsVisibleAndNonBlocking(t *testing.T) {
	repo := initGitRepo(t)
	t.Setenv("HOME", repo)
	writeRepoFile(t, filepath.Join(repo, presetRelativePath), `{"version":99,"presets":[]}`)
	writeRepoFile(t, filepath.Join(repo, "tasks", "1.md"), "one\n")

	model := NewModel(Options{
		RepoRoot: repo,
		Branch:   "main",
		Workflow: "files",
		NoColor:  true,
	})

	updated, _ := model.Update(ConfigureStateMsg{
		State: CommandState{
			Workflow: WorkflowFiles,
			Runtime: CommandRuntime{
				Agent:     "claude",
				ClaudeBin: "go",
			},
			Files: FileCommandState{
				Source:   FileSourceAllFiles,
				AllFiles: "tasks",
				DryRun:   true,
			},
		},
	})
	model = updated.(Model)

	if !model.canRun() {
		t.Fatalf("expected invalid preset file to remain non-blocking, got validation=%#v preflight=%#v", model.validation.Items(), model.preflight.Items())
	}
	view := model.View()
	if !strings.Contains(view, "Preset file skipped:") || !strings.Contains(view, "unsupported") || !strings.Contains(view, "version 99") {
		t.Fatalf("expected invalid preset warning in view, got:\n%s", view)
	}

	model = updateModel(t, model, keyRunes("s"))
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyDelete})
	model = updateModel(t, model, keyRunes("recovered"))
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if !strings.Contains(model.View(), "Invalid preset file moved to") {
		t.Fatalf("expected recovery status after save, got:\n%s", model.View())
	}
}

func TestLayoutThresholds(t *testing.T) {
	t.Parallel()

	model := NewModel(Options{
		RepoRoot: "/work/ghir",
		Branch:   "main",
		NoColor:  true,
	})

	updated, _ := model.Update(tea.WindowSizeMsg{Width: 120, Height: 36})
	wide := updated.(Model)
	if wide.Layout().Mode != LayoutWide {
		t.Fatalf("wide layout mismatch: got %q want %q", wide.Layout().Mode, LayoutWide)
	}

	updated, _ = model.Update(tea.WindowSizeMsg{Width: 90, Height: 36})
	stacked := updated.(Model)
	if stacked.Layout().Mode != LayoutStacked {
		t.Fatalf("stacked layout mismatch: got %q want %q", stacked.Layout().Mode, LayoutStacked)
	}
	if !strings.Contains(stacked.View(), "Run Setup") {
		t.Fatalf("expected stacked layout to keep the run setup visible, got:\n%s", stacked.View())
	}

	updated, _ = model.Update(tea.WindowSizeMsg{Width: 70, Height: 36})
	compact := updated.(Model)
	if compact.Layout().Mode != LayoutCompact {
		t.Fatalf("compact layout mismatch: got %q want %q", compact.Layout().Mode, LayoutCompact)
	}
	if !strings.Contains(compact.View(), "Compact Mode") {
		t.Fatalf("expected compact snapshot to mention compact mode, got:\n%s", compact.View())
	}
	if !strings.Contains(compact.View(), "Terminal width is under 80 columns.") {
		t.Fatalf("expected compact warning in snapshot, got:\n%s", compact.View())
	}
}

func TestNoColorSnapshotHasNoANSISequences(t *testing.T) {
	t.Parallel()

	view := Snapshot(Options{
		RepoRoot: "/work/ghir",
		Branch:   "main",
		Workflow: "issues",
		NoColor:  true,
	}, 120, 36)

	ansiPattern := regexp.MustCompile(`\x1b\[[0-9;]*m`)
	if ansiPattern.MatchString(view) {
		t.Fatalf("expected no ANSI sequences in no-color snapshot, got:\n%s", view)
	}
	if !strings.Contains(view, "> Workflow:") {
		t.Fatalf("expected no-color snapshot to retain textual focus markers, got:\n%s", view)
	}
}

func TestHelpOverlayTogglesAndCloses(t *testing.T) {
	t.Parallel()

	model := NewModel(Options{
		RepoRoot: "/work/ghir",
		Branch:   "main",
		Workflow: "files",
		NoColor:  true,
	})

	model = updateModel(t, model, keyRunes("?"))
	view := model.View()
	for _, needle := range []string{"Keyboard help", "Configure phase", "Close: Esc, Enter, ?, or Q"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("expected help overlay to contain %q, got:\n%s", needle, view)
		}
	}

	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEsc})
	if strings.Contains(model.View(), "Keyboard help") {
		t.Fatalf("expected help overlay to close, got:\n%s", model.View())
	}
}

func TestConfigureSearchIsDisabled(t *testing.T) {
	t.Parallel()

	model := NewModel(Options{
		RepoRoot: "/work/ghir",
		Branch:   "main",
		Workflow: "files",
		NoColor:  true,
	})

	model = updateModel(t, model, keyRunes("/"))
	if model.search.active {
		t.Fatalf("expected configure search to stay disabled")
	}
}

func TestRunSearchUpdatesSelectedQueueItem(t *testing.T) {
	t.Parallel()

	state := CommandState{
		Workflow: WorkflowFiles,
		Runtime: CommandRuntime{
			Agent: "codex",
		},
		Files: FileCommandState{
			Source:        FileSourceAllFiles,
			AllFiles:      "tasks",
			ResolvedQueue: []string{"tasks/1.md", "tasks/2.md", "tasks/10.md"},
			StagedQueue:   []string{"tasks/1.md", "tasks/2.md", "tasks/10.md"},
		},
	}

	model := NewModel(Options{
		RepoRoot: "/work/ghir",
		Branch:   "main",
		Workflow: "files",
		NoColor:  true,
	})
	model.phase = PhaseRun
	model.lastRunCommand = cloneCommandState(state)
	model.run = newRunPhase(state)

	model = updateModel(t, model, keyRunes("/"))
	model = updateModel(t, model, keyRunes("2"))

	if got, want := model.run.selectedIndex, 1; got != want {
		t.Fatalf("selected run queue item mismatch: got %d want %d", got, want)
	}
	if !strings.Contains(model.View(), "Selected item: tasks/2.md") {
		t.Fatalf("expected run view to show the searched selection, got:\n%s", model.View())
	}
}

func TestSummaryEnterAppliesFocusedAction(t *testing.T) {
	t.Parallel()

	model := NewModel(Options{
		RepoRoot: "/work/ghir",
		Branch:   "main",
		Workflow: "files",
		NoColor:  true,
	})
	model.phase = PhaseSummary
	model.summary = summaryPhase{
		workflow:    "files",
		focusArea:   summaryFocusActions,
		actionFocus: 3,
	}

	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if got, want := model.Phase(), PhaseConfigure; got != want {
		t.Fatalf("phase mismatch after summary Enter action: got %q want %q", got, want)
	}
}

func TestConfigureKeyboardEditsUpdateModel(t *testing.T) {
	t.Parallel()

	model := NewModel(Options{
		RepoRoot: "/work/ghir",
		Branch:   "main",
		Workflow: "files",
		NoColor:  true,
	})

	model = focusConfigureField(t, model, fieldRuntimeAgent)
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRight})
	if got, want := model.command.Runtime.Agent, "codex"; got != want {
		t.Fatalf("agent mismatch: got %q want %q", got, want)
	}
	if got, want := model.command.Runtime.Model, "gpt-5.4"; got != want {
		t.Fatalf("default model mismatch after agent switch: got %q want %q", got, want)
	}

	model = focusConfigureField(t, model, fieldRuntimeModel)
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRight})
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRight})
	if got, want := selectedModelChoice(model.command.Runtime.Agent, model.command.Runtime.Model, model.command.Runtime.ModelCustom), customModelOptionValue; got != want {
		t.Fatalf("expected custom model selection, got %q", got)
	}
	model = focusConfigureField(t, model, fieldRuntimeModelCustom)
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyDelete})
	model = updateModel(t, model, keyRunes("gpt-5-codex"))
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if got, want := model.command.Runtime.Model, "gpt-5-codex"; got != want {
		t.Fatalf("model mismatch: got %q want %q", got, want)
	}
	view := model.View()
	if !strings.Contains(view, "gpt-5-codex") || !strings.Contains(view, "--model gpt-5-codex") {
		t.Fatalf("expected configure view to render the updated model, got:\n%s", model.View())
	}
}

func TestConfigureChoiceFieldEnterOpensSelectionMenu(t *testing.T) {
	t.Parallel()

	model := NewModel(Options{
		RepoRoot: "/work/ghir",
		Branch:   "main",
		Workflow: "issues",
		NoColor:  true,
	})

	model = focusConfigureField(t, model, fieldRuntimeAgent)
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	if got, want := model.configure.overlay, configureOverlayChoice; got != want {
		t.Fatalf("expected choice overlay after Enter, got %q", got)
	}
	if !strings.Contains(model.View(), "Select agent") {
		t.Fatalf("expected choice overlay to render in view, got:\n%s", model.View())
	}

	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyDown})
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if got, want := model.command.Runtime.Agent, "codex"; got != want {
		t.Fatalf("agent mismatch after choice selection: got %q want %q", got, want)
	}
	if got := model.configure.overlay; got != configureOverlayNone {
		t.Fatalf("expected choice overlay to close, got %q", got)
	}
}

func TestConfigureKeyboardSupportsWorkflowAndSourceSwitching(t *testing.T) {
	t.Parallel()

	model := NewModel(Options{
		RepoRoot: "/work/ghir",
		Branch:   "main",
		Workflow: "issues",
		NoColor:  true,
	})

	model = focusConfigureField(t, model, fieldWorkflow)
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRight})
	if model.command.Workflow != WorkflowFiles {
		t.Fatalf("workflow mismatch after right navigation: got %q want %q", model.command.Workflow, WorkflowFiles)
	}

	model = focusConfigureField(t, model, fieldFileSource)
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyLeft})
	if model.command.Files.Source != FileSourceExplicit {
		t.Fatalf("file source mismatch after left navigation: got %q want %q", model.command.Files.Source, FileSourceExplicit)
	}

	model = focusConfigureField(t, model, fieldWorkflow)
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRight})
	if model.command.Workflow != WorkflowImprove {
		t.Fatalf("workflow mismatch after second right navigation: got %q want %q", model.command.Workflow, WorkflowImprove)
	}
	if !strings.Contains(model.View(), "Mode:") || !strings.Contains(model.View(), "Strategy:") {
		t.Fatalf("expected improve workflow fields in configure view, got:\n%s", model.View())
	}
}

func TestConfigureKeyboardValidationRendersInline(t *testing.T) {
	t.Parallel()

	model := NewModel(Options{
		RepoRoot: "/work/ghir",
		Branch:   "main",
		Workflow: "issues",
		NoColor:  true,
	})

	model = focusConfigureField(t, model, fieldIssueSource)
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyLeft})
	if model.command.Issues.Source != IssueSourceCSV {
		t.Fatalf("issue source mismatch after left navigation: got %q want %q", model.command.Issues.Source, IssueSourceCSV)
	}
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyLeft})
	if model.command.Issues.Source != IssueSourceSingle {
		t.Fatalf("issue source mismatch after second left navigation: got %q want %q", model.command.Issues.Source, IssueSourceSingle)
	}

	model = focusConfigureField(t, model, fieldIssueSingle)
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})
	model = updateModel(t, model, keyRunes("abc"))
	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyEnter})

	if !model.validation.HasErrors() {
		t.Fatalf("expected validation errors after invalid issue entry")
	}
	view := model.View()
	if !strings.Contains(view, "--issue must be numeric:") || !strings.Contains(view, "Value:") {
		t.Fatalf("expected inline issue validation message in configure view, got:\n%s", view)
	}
	if !strings.Contains(buildCommandPreview(model.command), "--issue abc") {
		t.Fatalf("expected command preview to track the active single-issue source, got %q", buildCommandPreview(model.command))
	}
	if !strings.Contains(view, "Status") {
		t.Fatalf("expected compact status section in configure view, got:\n%s", view)
	}
}

func TestConfigureViewKeepsFocusedSourceVisibleInStackedLayout(t *testing.T) {
	t.Parallel()

	model := NewModel(Options{
		RepoRoot: "/work/ghir",
		Branch:   "main",
		Workflow: "issues",
		NoColor:  true,
	})

	model = focusConfigureField(t, model, fieldIssueSource)
	view := model.Snapshot(90, 18)

	if !strings.Contains(view, "> Source:") {
		t.Fatalf("expected focused source field to remain visible in stacked layout, got:\n%s", view)
	}
	if got := len(strings.Split(view, "\n")); got > 22 {
		t.Fatalf("expected stacked snapshot to remain clipped, got %d lines:\n%s", got, view)
	}
}

func TestConfigureRefreshIgnoresInactiveWorkflowConflicts(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	model := NewModel(Options{
		RepoRoot: repo,
		Branch:   "main",
		Workflow: "issues",
		NoColor:  true,
	})

	updated, _ := model.Update(ConfigureStateMsg{
		State: CommandState{
			Workflow: WorkflowIssues,
			Runtime: CommandRuntime{
				Agent:     "claude",
				ClaudeBin: "go",
				GHBin:     "go",
			},
			Issues: IssueCommandState{
				Source: IssueSourceAllOpen,
			},
			Files: FileCommandState{
				Source:   FileSourceAllFiles,
				AllFiles: "tasks",
			},
		},
	})
	configured := updated.(Model)
	if reportContains(configured.validation, "--files/--all-files cannot be combined with --issue/--all-open/--issues/--issues-file") {
		t.Fatalf("expected inactive workflow conflict to be ignored in configure validation, got %#v", configured.validation.Items())
	}
}

func TestConfigureQueuePreviewLeavesFilesQueueUntouched(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	writeRepoFile(t, filepath.Join(repo, "tasks", "1.md"), "one\n")
	writeRepoFile(t, filepath.Join(repo, "tasks", "2.md"), "two\n")
	runGit(t, repo, "add", "tasks/1.md", "tasks/2.md")
	runGit(t, repo, "commit", "-m", "add tasks")
	writeRepoFile(t, filepath.Join(repo, "tasks", "10.md"), "ten\n")

	model := NewModel(Options{
		RepoRoot: repo,
		Branch:   "main",
		Workflow: "files",
		NoColor:  true,
	})

	if got, want := model.command.Files.ResolvedQueue, []string{"tasks/1.md", "tasks/2.md", "tasks/10.md"}; !slices.Equal(got, want) {
		t.Fatalf("resolved queue mismatch:\n got: %v\nwant: %v", got, want)
	}
	if got, want := model.command.Files.StagedQueue, []string{"tasks/1.md", "tasks/2.md", "tasks/10.md"}; !slices.Equal(got, want) {
		t.Fatalf("staged queue mismatch:\n got: %v\nwant: %v", got, want)
	}

	model = updateModel(t, model, keyRunes("]"))
	model = updateModel(t, model, keyRunes("u"))
	model = updateModel(t, model, keyRunes("]"))
	model = updateModel(t, model, keyRunes("]"))
	model = updateModel(t, model, keyRunes("x"))

	if got, want := model.command.Files.StagedQueue, []string{"tasks/1.md", "tasks/2.md", "tasks/10.md"}; !slices.Equal(got, want) {
		t.Fatalf("expected configure queue preview to remain read-only:\n got: %v\nwant: %v", got, want)
	}

	command := buildCommandPreview(model.command)
	if !strings.Contains(command, "--all-files tasks") {
		t.Fatalf("expected files source command preview, got %q", command)
	}
}

func TestConfigureQueuePreviewDoesNotMutateIssuesFile(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	issuesPath := filepath.Join(repo, ".ticket-runner", "issues.txt")
	writeRepoFile(t, issuesPath, "# backlog\n13\n19 note\n20\n")
	before, err := os.ReadFile(issuesPath)
	if err != nil {
		t.Fatalf("read issues file: %v", err)
	}

	model := NewModel(Options{
		RepoRoot: repo,
		Branch:   "main",
		Workflow: "issues",
		NoColor:  true,
	})

	updated, _ := model.Update(ConfigureStateMsg{
		State: CommandState{
			Workflow: WorkflowIssues,
			Runtime: CommandRuntime{
				Agent:     "claude",
				ClaudeBin: "go",
				GHBin:     "go",
			},
			Issues: IssueCommandState{
				Source: IssueSourceFile,
			},
		},
	})
	model = updated.(Model)

	if got, want := model.command.Issues.ResolvedQueue, []string{"13", "19", "20"}; !slices.Equal(got, want) {
		t.Fatalf("resolved queue mismatch:\n got: %v\nwant: %v", got, want)
	}

	model = updateModel(t, model, keyRunes("]"))
	model = updateModel(t, model, keyRunes("u"))
	model = updateModel(t, model, keyRunes("]"))
	model = updateModel(t, model, keyRunes("x"))

	if got, want := model.command.Issues.StagedQueue, []string{"13", "19", "20"}; !slices.Equal(got, want) {
		t.Fatalf("expected issue queue preview to remain read-only:\n got: %v\nwant: %v", got, want)
	}

	command := buildCommandPreview(model.command)
	if strings.Contains(command, "--issues 19,20") {
		t.Fatalf("expected issues queue to stay sourced from defaults, got %q", command)
	}

	after, err := os.ReadFile(issuesPath)
	if err != nil {
		t.Fatalf("read issues file after staging: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("expected staging actions to avoid file mutation:\n before: %q\n after: %q", string(before), string(after))
	}

}

func TestConfigureLegacyQueueKeysDoNotBlockRun(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	writeRepoFile(t, filepath.Join(repo, "tasks", "1.md"), "one\n")
	writeRepoFile(t, filepath.Join(repo, "tasks", "2.md"), "two\n")
	writeRepoFile(t, filepath.Join(repo, "tasks", "10.md"), "ten\n")

	model := NewModel(Options{
		RepoRoot: repo,
		Branch:   "main",
		Workflow: "files",
		NoColor:  true,
	})

	updated, _ := model.Update(ConfigureStateMsg{
		State: CommandState{
			Workflow: WorkflowFiles,
			Runtime: CommandRuntime{
				Agent:     "claude",
				ClaudeBin: "go",
			},
			Files: FileCommandState{
				Source:   FileSourceAllFiles,
				AllFiles: "tasks",
				DryRun:   true,
			},
		},
	})
	model = updated.(Model)

	model = updateModel(t, model, keyRunes("x"))
	model = updateModel(t, model, keyRunes("x"))
	model = updateModel(t, model, keyRunes("x"))

	if got, want := model.command.Files.StagedQueue, []string{"tasks/1.md", "tasks/2.md", "tasks/10.md"}; !slices.Equal(got, want) {
		t.Fatalf("expected queue preview to remain unchanged, got %v want %v", got, want)
	}

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	running := updated.(Model)
	if cmd != nil {
		updated, _ = running.Update(cmd())
		running = updated.(Model)
	}
	if running.Phase() != PhaseRun {
		t.Fatalf("expected run phase after no-op legacy queue keys, got %q", running.Phase())
	}
}

func TestRunPhaseUsesStagedQueueOrdering(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	writeRepoFile(t, filepath.Join(repo, "tasks", "1.md"), "one\n")
	writeRepoFile(t, filepath.Join(repo, "tasks", "2.md"), "two\n")
	writeRepoFile(t, filepath.Join(repo, "tasks", "10.md"), "ten\n")

	model := NewModel(Options{
		RepoRoot: repo,
		Branch:   "main",
		Workflow: "files",
		NoColor:  true,
	})

	updated, _ := model.Update(ConfigureStateMsg{
		State: CommandState{
			Workflow: WorkflowFiles,
			Runtime: CommandRuntime{
				Agent:     "claude",
				ClaudeBin: "go",
			},
			Files: FileCommandState{
				Source:        FileSourceAllFiles,
				AllFiles:      "tasks",
				DryRun:        true,
				ResolvedQueue: []string{"tasks/1.md", "tasks/2.md", "tasks/10.md"},
				StagedQueue:   []string{"tasks/10.md", "tasks/1.md"},
			},
		},
	})
	model = updated.(Model)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	running := updated.(Model)
	if cmd != nil {
		updated, _ = running.Update(cmd())
		running = updated.(Model)
	}

	if running.Phase() != PhaseRun {
		t.Fatalf("expected run phase, got %q", running.Phase())
	}
	if got, want := len(running.run.queue), 2; got != want {
		t.Fatalf("run queue length mismatch: got %d want %d", got, want)
	}
	if got, want := running.run.queue[0].Label, "tasks/10.md"; got != want {
		t.Fatalf("run queue first item mismatch: got %q want %q", got, want)
	}
	if got, want := running.run.queue[1].Label, "tasks/1.md"; got != want {
		t.Fatalf("run queue second item mismatch: got %q want %q", got, want)
	}
	if got, want := running.run.queue[0].Status, statusRunning; got != want {
		t.Fatalf("run queue first status mismatch: got %q want %q", got, want)
	}
	if got, want := running.run.queue[1].Status, statusQueued; got != want {
		t.Fatalf("run queue second status mismatch: got %q want %q", got, want)
	}
}

func TestConfigureCommandRailCopyActionUsesFullInvocation(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	writeRepoFile(t, filepath.Join(repo, "tasks", "1.md"), "one\n")

	var copied string
	model := NewModel(Options{
		RepoRoot: repo,
		Branch:   "main",
		Workflow: "files",
		NoColor:  true,
		CopyCommand: func(command string) error {
			copied = command
			return nil
		},
	})

	updated, _ := model.Update(ConfigureStateMsg{
		State: CommandState{
			Workflow: WorkflowFiles,
			Runtime: CommandRuntime{
				Agent:    "codex",
				CodexBin: "go",
			},
			Files: FileCommandState{
				Source:   FileSourceAllFiles,
				AllFiles: "tasks",
				DryRun:   true,
			},
		},
	})
	model = updated.(Model)

	model = updateModel(t, model, keyRunes("c"))

	if got, want := copied, BuildCommandString(model.command); got != want {
		t.Fatalf("copied command mismatch:\n got: %q\nwant: %q", got, want)
	}
	rail := strings.Join(renderCommandRail(&model), "\n")
	if !strings.Contains(rail, "Copy: full invocation copied to the terminal clipboard.") {
		t.Fatalf("expected successful copy status in command rail, got:\n%s", rail)
	}
}

func TestConfigureCommandRailExplainHighlightsStagedDivergence(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	writeRepoFile(t, filepath.Join(repo, "tasks", "1.md"), "one\n")
	writeRepoFile(t, filepath.Join(repo, "tasks", "2.md"), "two\n")
	writeRepoFile(t, filepath.Join(repo, "tasks", "10.md"), "ten\n")

	model := NewModel(Options{
		RepoRoot: repo,
		Branch:   "main",
		Workflow: "files",
		NoColor:  true,
	})

	updated, _ := model.Update(ConfigureStateMsg{
		State: CommandState{
			Workflow: WorkflowFiles,
			Runtime: CommandRuntime{
				Agent:     "claude",
				ClaudeBin: "go",
			},
			Files: FileCommandState{
				Source:        FileSourceAllFiles,
				AllFiles:      "tasks",
				Loop:          true,
				ResolvedQueue: []string{"tasks/1.md", "tasks/2.md", "tasks/10.md"},
				StagedQueue:   []string{"tasks/10.md", "tasks/1.md"},
			},
		},
	})
	model = updated.(Model)

	model = updateModel(t, model, keyRunes("e"))
	rail := strings.Join(renderCommandRail(&model), "\n")
	for _, needle := range []string{
		"Queue staging rewrote the file source for this run.",
		"Source field: --all-files tasks",
		"Executed as: --files tasks/10.md,tasks/1.md",
		"Staged queue -> --files tasks/10.md,tasks/1.md",
		"Loop -> (omitted) (staged files no longer preserve --all-files semantics)",
	} {
		if !strings.Contains(rail, needle) {
			t.Fatalf("expected command rail explanation to contain %q, got:\n%s", needle, rail)
		}
	}
}

func TestRunPhaseCommandRailRemainsVisible(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	writeRepoFile(t, filepath.Join(repo, "tasks", "1.md"), "one\n")

	model := NewModel(Options{
		RepoRoot: repo,
		Branch:   "main",
		Workflow: "files",
		NoColor:  true,
	})

	updated, _ := model.Update(ConfigureStateMsg{
		State: CommandState{
			Workflow: WorkflowFiles,
			Runtime: CommandRuntime{
				Agent:    "codex",
				CodexBin: "go",
			},
			Files: FileCommandState{
				Source:   FileSourceAllFiles,
				AllFiles: "tasks",
				DryRun:   true,
			},
		},
	})
	model = updated.(Model)
	model = updateModel(t, model, keyRunes("r"))
	model = updateModel(t, model, keyRunes("e"))

	rail := strings.Join(renderCommandRail(&model), "\n")
	for _, needle := range []string{
		"Invocation",
		"ghir --all-files tasks --dry-run --agent codex --model gpt-5.4 --codex-bin go",
		"Explain",
		"Source selection -> --all-files tasks",
	} {
		if !strings.Contains(rail, needle) {
			t.Fatalf("expected run command rail to contain %q, got:\n%s", needle, rail)
		}
	}

	view := model.View()
	if !strings.Contains(view, "Current Item / Command Rail") {
		t.Fatalf("expected run pane title to mention command rail, got:\n%s", view)
	}
}

func TestRunPhaseStreamsProcessEventsAndUpdatesQueue(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	writeRepoFile(t, filepath.Join(repo, "tasks", "1.md"), "one\n")
	writeRepoFile(t, filepath.Join(repo, "tasks", "2.md"), "two\n")

	events := make(chan tea.Msg, 8)
	var request RunRequest

	model := NewModel(Options{
		RepoRoot:      repo,
		Branch:        "main",
		Workflow:      "files",
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
		Workflow: WorkflowFiles,
		Runtime: CommandRuntime{
			Agent:      "codex",
			CodexBin:   "go",
			StreamView: "raw",
		},
		Files: FileCommandState{
			Source:        FileSourceAllFiles,
			AllFiles:      "tasks",
			DryRun:        true,
			ResolvedQueue: []string{"tasks/1.md", "tasks/2.md"},
			StagedQueue:   []string{"tasks/1.md", "tasks/2.md"},
		},
	}

	updated, _ := model.Update(ConfigureStateMsg{State: state})
	model = updated.(Model)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model = updated.(Model)
	if cmd == nil {
		t.Fatalf("expected run listener command")
	}

	if got, want := request.Executable, "/tmp/ghir-test"; got != want {
		t.Fatalf("run executable mismatch: got %q want %q", got, want)
	}
	if got, want := request.Args, BuildCommandArgs(model.command); !slices.Equal(got, want) {
		t.Fatalf("run args mismatch:\n got: %v\nwant: %v", got, want)
	}

	model, cmd = stepRunEvent(t, model, cmd, events, runProcessLineMsg{Line: "[1/2] tasks/1.md: First task"})
	model, cmd = stepRunEvent(t, model, cmd, events, runProcessLineMsg{Line: "Log: .ticket-runs/tasks__1.md.log"})
	model, cmd = stepRunEvent(t, model, cmd, events, runProcessLineMsg{Line: "[DRY RUN] Would process tasks/1.md"})
	model, cmd = stepRunEvent(t, model, cmd, events, runProcessLineMsg{Line: "[2/2] tasks/2.md: Second task"})
	model, cmd = stepRunEvent(t, model, cmd, events, runProcessLineMsg{Line: "Log: .ticket-runs/tasks__2.md.log"})
	model, cmd = stepRunEvent(t, model, cmd, events, runProcessLineMsg{Line: "FAILED: codex exited with code 1 for tasks/2.md"})
	model, _ = stepRunEvent(t, model, cmd, events, runProcessExitMsg{ExitCode: 1, Err: errors.New("exit status 1")})

	if got, want := model.Phase(), PhaseSummary; got != want {
		t.Fatalf("phase mismatch after run completion: got %q want %q", got, want)
	}

	if got, want := model.run.queue[0].Status, statusDone; got != want {
		t.Fatalf("first queue status mismatch: got %q want %q", got, want)
	}
	if got, want := model.run.queue[1].Status, statusFailed; got != want {
		t.Fatalf("second queue status mismatch: got %q want %q", got, want)
	}
	if got, want := model.run.currentLogPath, ".ticket-runs/tasks__2.md.log"; got != want {
		t.Fatalf("current log path mismatch: got %q want %q", got, want)
	}
	if got, want := model.run.state, runStateFailed; got != want {
		t.Fatalf("run state mismatch: got %q want %q", got, want)
	}
	if got, want := model.run.failureContext, "FAILED: codex exited with code 1 for tasks/2.md"; got != want {
		t.Fatalf("failure context mismatch: got %q want %q", got, want)
	}
	if got, want := model.summary.succeeded, 1; got != want {
		t.Fatalf("summary succeeded mismatch: got %d want %d", got, want)
	}
	if got, want := model.summary.failed, 1; got != want {
		t.Fatalf("summary failed mismatch: got %d want %d", got, want)
	}
	if got, want := model.summary.failedItems, []string{"tasks/2.md"}; !slices.Equal(got, want) {
		t.Fatalf("summary failed items mismatch:\n got: %v\nwant: %v", got, want)
	}
	if got, want := model.summary.lastLogs, []string{".ticket-runs/tasks__1.md.log", ".ticket-runs/tasks__2.md.log"}; !slices.Equal(got, want) {
		t.Fatalf("summary logs mismatch:\n got: %v\nwant: %v", got, want)
	}
	view := model.View()
	for _, needle := range []string{"Succeeded: 1", "Failed: 1", "tasks/2.md", ".ticket-runs/tasks__2.md.log", "Stream", "FAILED: codex exited with code 1", "for tasks/2.md"} {
		if !strings.Contains(view, needle) {
			t.Fatalf("expected summary view to contain %q, got:\n%s", needle, view)
		}
	}
	if !strings.Contains(strings.Join(model.run.stream, "\n"), "FAILED: codex exited with code 1 for tasks/2.md") {
		t.Fatalf("expected stream output to contain failure line, got %v", model.run.stream)
	}
	if !strings.Contains(strings.Join(model.summary.stream, "\n"), "FAILED: codex exited with code 1 for tasks/2.md") {
		t.Fatalf("expected summary to retain stream output, got %v", model.summary.stream)
	}
}

func TestSummaryRerunFailedSubsetReturnsToRun(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	writeRepoFile(t, filepath.Join(repo, "tasks", "1.md"), "one\n")
	writeRepoFile(t, filepath.Join(repo, "tasks", "2.md"), "two\n")

	events := make(chan tea.Msg, 1)
	var requests []RunRequest

	model := NewModel(Options{
		RepoRoot:      repo,
		Branch:        "main",
		Workflow:      "files",
		NoColor:       true,
		RunExecutable: "/tmp/ghir-test",
		StartRun: func(req RunRequest) (runHandle, error) {
			requests = append(requests, req)
			return runHandle{
				Events: events,
				Stop: func() error {
					return nil
				},
			}, nil
		},
	})

	state := CommandState{
		Workflow: WorkflowFiles,
		Runtime: CommandRuntime{
			Agent:    "codex",
			CodexBin: "go",
		},
		Files: FileCommandState{
			Source:        FileSourceAllFiles,
			AllFiles:      "tasks",
			DryRun:        true,
			ResolvedQueue: []string{"tasks/1.md", "tasks/2.md"},
			StagedQueue:   []string{"tasks/1.md", "tasks/2.md"},
		},
	}

	model.command = cloneCommandState(state)
	model.lastRunCommand = cloneCommandState(state)
	model.lastRunDefaults = resolveConfigureDefaults(repo, effectiveCommandState(state))
	model.run = newRunPhase(state)
	model.run.queue = []runItem{
		{Label: "tasks/1.md", Status: statusDone},
		{Label: "tasks/2.md", Status: statusFailed},
	}
	model.run.lastLogs = []string{".ticket-runs/tasks__2.md.log"}
	model.run.failureContext = "FAILED: codex exited with code 1 for tasks/2.md"
	model.run.state = runStateFailed
	model.phase = PhaseRun

	updated, _ := model.Update(TransitionMsg{Phase: PhaseSummary})
	model = updated.(Model)

	updated, cmd := model.Update(keyRunes("r"))
	model = updated.(Model)
	if got, want := model.Phase(), PhaseRun; got != want {
		t.Fatalf("phase mismatch after rerun action: got %q want %q", got, want)
	}
	if cmd == nil {
		t.Fatalf("expected rerun listener command")
	}

	expectedState := cloneCommandState(state)
	replaceActiveStagedQueue(&expectedState, []string{"tasks/2.md"})
	if got, want := requests[len(requests)-1].Args, BuildCommandArgs(expectedState); !slices.Equal(got, want) {
		t.Fatalf("rerun args mismatch:\n got: %v\nwant: %v", got, want)
	}
	if got, want := model.command.Files.StagedQueue, []string{"tasks/1.md", "tasks/2.md"}; !slices.Equal(got, want) {
		t.Fatalf("configure state was mutated by rerun:\n got: %v\nwant: %v", got, want)
	}
}

func TestSummaryResetActionUsesCLIResetCommand(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	var requests []RunRequest

	state := CommandState{
		Workflow: WorkflowFiles,
		Runtime: CommandRuntime{
			Agent:    "codex",
			CodexBin: "go",
			NoColor:  true,
		},
		Files: FileCommandState{
			Source:        FileSourceAllFiles,
			AllFiles:      "tasks",
			ResolvedQueue: []string{"tasks/1.md", "tasks/2.md"},
			StagedQueue:   []string{"tasks/1.md", "tasks/2.md"},
			DoneFile:      ".ticket-runs/custom.completed",
		},
	}

	model := NewModel(Options{
		RepoRoot:      repo,
		Branch:        "main",
		Workflow:      "files",
		NoColor:       true,
		RunExecutable: "/tmp/ghir-test",
		RunAction: func(req RunRequest) (string, error) {
			requests = append(requests, req)
			return "Reset completion for tasks/2.md", nil
		},
	})
	model.phase = PhaseSummary
	model.summary = summaryPhase{
		workflow:     "files",
		command:      cloneCommandState(state),
		defaults:     resolveConfigureDefaults(repo, effectiveCommandState(state)),
		failed:       1,
		failedItems:  []string{"tasks/2.md"},
		failureFocus: 0,
	}

	updated, cmd := model.Update(keyRunes("x"))
	model = updated.(Model)
	if cmd == nil {
		t.Fatalf("expected reset command")
	}

	updated, _ = model.Update(cmd())
	model = updated.(Model)

	scope, expectedArgs := buildSummaryResetArgs(state, resolveConfigureDefaults(repo, effectiveCommandState(state)), "tasks/2.md")
	if got, want := requests, []RunRequest{{
		Executable: "/tmp/ghir-test",
		Args:       expectedArgs,
		Dir:        repo,
		Env:        nil,
	}}; len(got) != len(want) || got[0].Executable != want[0].Executable || got[0].Dir != want[0].Dir || !slices.Equal(got[0].Args, want[0].Args) {
		t.Fatalf("reset request mismatch:\n got: %#v\nwant: %#v", got, want)
	}
	if !strings.Contains(model.summary.actionStatus, scope) || !strings.Contains(model.summary.actionStatus, "Reset completion for tasks/2.md") {
		t.Fatalf("expected reset status in summary, got %q", model.summary.actionStatus)
	}
}

func TestRunPhasePauseBuffersStreamWithoutStoppingExecution(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	writeRepoFile(t, filepath.Join(repo, "tasks", "1.md"), "one\n")
	runGit(t, repo, "add", "tasks/1.md")
	runGit(t, repo, "commit", "-m", "add task")
	events := make(chan tea.Msg, 4)

	model := NewModel(Options{
		RepoRoot:      repo,
		Branch:        "main",
		Workflow:      "files",
		NoColor:       true,
		RunExecutable: "/tmp/ghir-test",
		StartRun: func(req RunRequest) (runHandle, error) {
			return runHandle{
				Events: events,
				Stop: func() error {
					return nil
				},
			}, nil
		},
	})

	updated, _ := model.Update(ConfigureStateMsg{
		State: CommandState{
			Workflow: WorkflowFiles,
			Runtime: CommandRuntime{
				Agent:    "codex",
				CodexBin: "go",
			},
			Files: FileCommandState{
				Source:        FileSourceExplicit,
				Files:         []string{"tasks/1.md"},
				ResolvedQueue: []string{"tasks/1.md"},
				StagedQueue:   []string{"tasks/1.md"},
				DryRun:        true,
			},
		},
	})
	model = updated.(Model)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model = updated.(Model)
	if cmd == nil {
		t.Fatalf("expected run listener command")
	}

	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	model, cmd = stepRunEvent(t, model, cmd, events, runProcessLineMsg{Line: "buffer me"})

	if strings.Contains(strings.Join(model.run.stream, "\n"), "buffer me") {
		t.Fatalf("expected paused stream to hide buffered output, got %v", model.run.stream)
	}
	if got, want := len(model.run.pendingStream), 1; got != want {
		t.Fatalf("pending stream length mismatch: got %d want %d", got, want)
	}

	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'p'}})
	if !strings.Contains(strings.Join(model.run.stream, "\n"), "buffer me") {
		t.Fatalf("expected resumed stream to flush buffered output, got %v", model.run.stream)
	}
}

func TestRunPhaseQuitStopsGracefullyAndQuitsAfterExit(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	writeRepoFile(t, filepath.Join(repo, "tasks", "1.md"), "one\n")
	runGit(t, repo, "add", "tasks/1.md")
	runGit(t, repo, "commit", "-m", "add task")
	events := make(chan tea.Msg, 4)
	stopCalls := 0

	model := NewModel(Options{
		RepoRoot:      repo,
		Branch:        "main",
		Workflow:      "files",
		NoColor:       true,
		RunExecutable: "/tmp/ghir-test",
		StartRun: func(req RunRequest) (runHandle, error) {
			return runHandle{
				Events: events,
				Stop: func() error {
					stopCalls++
					return nil
				},
			}, nil
		},
	})

	updated, _ := model.Update(ConfigureStateMsg{
		State: CommandState{
			Workflow: WorkflowFiles,
			Runtime: CommandRuntime{
				Agent:    "codex",
				CodexBin: "go",
			},
			Files: FileCommandState{
				Source:        FileSourceExplicit,
				Files:         []string{"tasks/1.md"},
				ResolvedQueue: []string{"tasks/1.md"},
				StagedQueue:   []string{"tasks/1.md"},
				DryRun:        true,
			},
		},
	})
	model = updated.(Model)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model = updated.(Model)
	if cmd == nil {
		t.Fatalf("expected run listener command")
	}

	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	model = updated.(Model)
	if got, want := stopCalls, 1; got != want {
		t.Fatalf("stop calls mismatch: got %d want %d", got, want)
	}

	events <- runProcessExitMsg{ExitCode: 130, Err: errors.New("interrupt")}
	updated, quitCmd := model.Update(cmd())
	model = updated.(Model)
	if got, want := model.run.state, runStateStopped; got != want {
		t.Fatalf("run state mismatch: got %q want %q", got, want)
	}
	if quitCmd == nil {
		t.Fatalf("expected quit command after graceful stop")
	}
	if _, ok := quitCmd().(tea.QuitMsg); !ok {
		t.Fatalf("expected tea.QuitMsg after graceful stop, got %T", quitCmd())
	}
}

func TestRunPhaseCtrlCStopsGracefullyWithoutQuitting(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	writeRepoFile(t, filepath.Join(repo, "tasks", "1.md"), "one\n")
	runGit(t, repo, "add", "tasks/1.md")
	runGit(t, repo, "commit", "-m", "add task")
	events := make(chan tea.Msg, 4)
	stopCalls := 0

	model := NewModel(Options{
		RepoRoot:      repo,
		Branch:        "main",
		Workflow:      "files",
		NoColor:       true,
		RunExecutable: "/tmp/ghir-test",
		StartRun: func(req RunRequest) (runHandle, error) {
			return runHandle{
				Events: events,
				Stop: func() error {
					stopCalls++
					return nil
				},
			}, nil
		},
	})

	updated, _ := model.Update(ConfigureStateMsg{
		State: CommandState{
			Workflow: WorkflowFiles,
			Runtime: CommandRuntime{
				Agent:    "codex",
				CodexBin: "go",
			},
			Files: FileCommandState{
				Source:        FileSourceExplicit,
				Files:         []string{"tasks/1.md"},
				ResolvedQueue: []string{"tasks/1.md"},
				StagedQueue:   []string{"tasks/1.md"},
				DryRun:        true,
			},
		},
	})
	model = updated.(Model)

	updated, cmd := model.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	model = updated.(Model)
	if cmd == nil {
		t.Fatalf("expected run listener command")
	}

	updated, cmd = model.Update(tea.KeyMsg{Type: tea.KeyCtrlC})
	model = updated.(Model)
	if got, want := stopCalls, 1; got != want {
		t.Fatalf("stop calls mismatch: got %d want %d", got, want)
	}
	view := model.View()
	if !strings.Contains(view, "Graceful stop requested.") || !strings.Contains(view, "Waiting for the subprocess to exit") || !strings.Contains(view, "cleanly.") {
		t.Fatalf("expected graceful stop warning in view, got:\n%s", model.View())
	}

	events <- runProcessExitMsg{ExitCode: 130, Err: errors.New("interrupt")}
	updated, quitCmd := model.Update(cmd())
	model = updated.(Model)
	if got, want := model.run.state, runStateStopped; got != want {
		t.Fatalf("run state mismatch: got %q want %q", got, want)
	}
	if got, want := model.Phase(), PhaseSummary; got != want {
		t.Fatalf("phase mismatch after graceful stop: got %q want %q", got, want)
	}
	if quitCmd != nil {
		t.Fatalf("expected Ctrl+C graceful stop to stay in TUI, got quit command %v", quitCmd)
	}
}

func TestRunPhaseOpenLogUsesPagerWhenAvailable(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	logPath := filepath.Join(repo, ".ticket-runs", "task.log")
	writeRepoFile(t, logPath, "log output\n")

	var executed *exec.Cmd
	model := NewModel(Options{
		RepoRoot: repo,
		Branch:   "main",
		Workflow: "files",
		NoColor:  true,
		Getenv: func(name string) string {
			if name == "PAGER" {
				return "test-pager -R"
			}
			return ""
		},
		LookPath: func(name string) (string, error) {
			if name == "test-pager" {
				return "/usr/bin/test-pager", nil
			}
			return "", exec.ErrNotFound
		},
		ExecProcess: func(cmd *exec.Cmd, fn tea.ExecCallback) tea.Cmd {
			executed = cmd
			return func() tea.Msg {
				return fn(nil)
			}
		},
	})

	updated, _ := model.Update(TransitionMsg{Phase: PhaseRun})
	model = updated.(Model)
	model.run.currentLogPath = ".ticket-runs/task.log"

	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})

	if executed == nil {
		t.Fatalf("expected pager command to execute")
	}
	if got, want := executed.Args, []string{"/usr/bin/test-pager", "-R", logPath}; !slices.Equal(got, want) {
		t.Fatalf("pager args mismatch:\n got: %v\nwant: %v", got, want)
	}
	if got, want := executed.Dir, repo; got != want {
		t.Fatalf("pager dir mismatch: got %q want %q", got, want)
	}
	if !strings.Contains(model.run.logAccessStatus, "Opened log with pager") {
		t.Fatalf("expected successful log access status, got %q", model.run.logAccessStatus)
	}
}

func TestRunPhaseOpenLogFallsBackToPathWhenPagerUnavailable(t *testing.T) {
	t.Parallel()

	repo := initGitRepo(t)
	logPath := filepath.Join(repo, ".ticket-runs", "task.log")
	writeRepoFile(t, logPath, "log output\n")

	model := NewModel(Options{
		RepoRoot: repo,
		Branch:   "main",
		Workflow: "files",
		NoColor:  true,
		Getenv: func(name string) string {
			if name == "PAGER" {
				return "missing-pager"
			}
			return ""
		},
		LookPath: func(name string) (string, error) {
			return "", exec.ErrNotFound
		},
	})

	updated, _ := model.Update(TransitionMsg{Phase: PhaseRun})
	model = updated.(Model)
	model.run.currentLogPath = ".ticket-runs/task.log"

	model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'o'}})

	if got := model.run.logAccessSeverity; got != SeverityWarning {
		t.Fatalf("log access severity mismatch: got %q want %q", got, SeverityWarning)
	}
	if !strings.Contains(model.run.logAccessStatus, logPath) {
		t.Fatalf("expected fallback path in log access status, got %q", model.run.logAccessStatus)
	}
}

func TestRunPhaseTracksRetryAndSessionWaitCountdown(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)
	model := NewModel(Options{
		RepoRoot: "/work/ghir",
		Branch:   "main",
		Workflow: "files",
		NoColor:  true,
		Now: func() time.Time {
			return now
		},
	})

	updated, _ := model.Update(TransitionMsg{Phase: PhaseRun})
	model = updated.(Model)
	model.run.consumeLine(now, "Retrying due to internal server error (attempt 2/3)...")
	model.run.consumeLine(now, "SESSION LIMIT HIT - waiting until 2026-03-07 12:02 UTC (120s)")
	model.run.tick(now.Add(30 * time.Second))

	if got, want := model.run.retryStatus, "2 / 3"; got != want {
		t.Fatalf("retry status mismatch: got %q want %q", got, want)
	}
	if got, want := model.run.renderSessionStatus(), "waiting (01:30 remaining until 2026-03-07 12:02 UTC)"; got != want {
		t.Fatalf("session status mismatch: got %q want %q", got, want)
	}
	view := model.View()
	if !strings.Contains(view, "Retry count: 2 / 3") {
		t.Fatalf("expected retry count in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Session wait: waiting (01:30") || !strings.Contains(view, "remaining until 2026-03-07 12:02") {
		t.Fatalf("expected session wait countdown in view, got:\n%s", view)
	}
}

func TestRunPhaseTracksRetryReasonForExpiredToken(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)
	model := NewModel(Options{
		RepoRoot: "/work/ghir",
		Branch:   "main",
		Workflow: "files",
		NoColor:  true,
		Now: func() time.Time {
			return now
		},
	})

	updated, _ := model.Update(TransitionMsg{Phase: PhaseRun})
	model = updated.(Model)
	model.run.consumeLine(now, "Retrying due to expired IDE token (attempt 1/3)...")

	if got, want := model.run.retryStatus, "1 / 3"; got != want {
		t.Fatalf("retry status mismatch: got %q want %q", got, want)
	}
	view := model.View()
	if !strings.Contains(view, "warn  Agent retry in progress (1/3)") || !strings.Contains(view, "after expired IDE token.") {
		t.Fatalf("expected token retry warning banner in view, got:\n%s", view)
	}
}

func TestRunPhaseViewIncludesWarningBannerAndFailureContext(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, 3, 7, 12, 0, 0, 0, time.UTC)
	model := NewModel(Options{
		RepoRoot: "/work/ghir",
		Branch:   "main",
		Workflow: "files",
		NoColor:  true,
		Now: func() time.Time {
			return now
		},
	})

	updated, _ := model.Update(TransitionMsg{Phase: PhaseRun})
	model = updated.(Model)
	model.run.currentLogPath = ".ticket-runs/tasks__2.md.log"
	model.run.consumeLine(now, "Retrying due to internal server error (attempt 1/3)...")
	model.run.consumeLine(now, "FAILED: codex exited with code 1 for tasks/2.md")

	view := model.View()
	if !strings.Contains(view, "warn  Agent retry in progress (1/3)") || !strings.Contains(view, "after internal server error.") {
		t.Fatalf("expected warning banner in view, got:\n%s", view)
	}
	if !strings.Contains(view, "Failure context") {
		t.Fatalf("expected failure context section in view, got:\n%s", view)
	}
	if !strings.Contains(view, "FAILED: codex exited with code 1") || !strings.Contains(view, "for tasks/2.md") {
		t.Fatalf("expected failure context text in view, got:\n%s", view)
	}
}

func TestRunViewClipsLongStreamToViewport(t *testing.T) {
	t.Parallel()

	model := NewModel(Options{
		RepoRoot: "/work/ghir",
		Branch:   "main",
		Workflow: "files",
		NoColor:  true,
	})
	model.phase = PhaseRun
	model.run = newRunPhase(CommandState{
		Workflow: WorkflowFiles,
		Runtime:  CommandRuntime{Agent: "codex"},
		Files: FileCommandState{
			Source:        FileSourceAllFiles,
			AllFiles:      "tasks",
			ResolvedQueue: []string{"tasks/1.md"},
			StagedQueue:   []string{"tasks/1.md"},
		},
	})
	for i := 0; i < 120; i++ {
		model.run.appendVisibleLine(fmt.Sprintf("stream line %03d", i))
	}

	view := model.Snapshot(120, 20)
	lines := strings.Split(view, "\n")
	if got := len(lines); got > 24 {
		t.Fatalf("expected clipped viewport, got %d lines:\n%s", got, view)
	}
	if !strings.Contains(view, "stream line 119") {
		t.Fatalf("expected latest stream line to remain visible, got:\n%s", view)
	}
}

func TestDefaultRunStarterStreamsProcessOutputAndExitCode(t *testing.T) {
	t.Parallel()

	handle, err := defaultRunStarter(RunRequest{
		Executable: os.Args[0],
		Args:       []string{"-test.run=TestRunStarterHelperProcess"},
		Env:        append(os.Environ(), "GHIR_TUI_RUN_HELPER=1"),
	})
	if err != nil {
		t.Fatalf("defaultRunStarter returned error: %v", err)
	}

	var lines []string
	var exit runProcessExitMsg
	for msg := range handle.Events {
		switch typed := msg.(type) {
		case runProcessLineMsg:
			lines = append(lines, typed.Line)
		case runProcessExitMsg:
			exit = typed
		}
	}

	if got, want := lines, []string{"helper stdout", "helper stderr"}; !slices.Equal(got, want) {
		t.Fatalf("stream lines mismatch:\n got: %v\nwant: %v", got, want)
	}
	if got, want := exit.ExitCode, 7; got != want {
		t.Fatalf("exit code mismatch: got %d want %d", got, want)
	}
}

func TestRunStarterHelperProcess(t *testing.T) {
	if os.Getenv("GHIR_TUI_RUN_HELPER") != "1" {
		return
	}

	fmt.Fprintln(os.Stdout, "helper stdout")
	fmt.Fprintln(os.Stderr, "helper stderr")
	os.Exit(7)
}

func updateModel(t *testing.T, model Model, msg tea.Msg) Model {
	t.Helper()

	updated, cmd := model.Update(msg)
	next := updated.(Model)
	if cmd == nil {
		return next
	}

	updated, _ = next.Update(cmd())
	return updated.(Model)
}

func focusConfigureField(t *testing.T, model Model, id string) Model {
	t.Helper()

	for i := 0; i < 128; i++ {
		if model.configure.focusedField(&model).ID == id {
			return model
		}
		model = updateModel(t, model, tea.KeyMsg{Type: tea.KeyDown})
	}

	t.Fatalf("field %q not reachable; current focus=%q", id, model.configure.focusedField(&model).ID)
	return model
}

func keyRunes(value string) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)}
}

func stepRunEvent(t *testing.T, model Model, cmd tea.Cmd, events chan tea.Msg, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()

	if cmd == nil {
		t.Fatalf("expected run listener command")
	}
	events <- msg
	updated, next := model.Update(cmd())
	return updated.(Model), next
}

func writeRepoFile(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write file %s: %v", path, err)
	}
}
