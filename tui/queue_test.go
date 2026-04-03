package tui

import (
	"slices"
	"testing"

	"ghir/defaults"
)

func TestRefreshActiveQueueResolvesAllOpenIssues(t *testing.T) {
	t.Parallel()

	state := CommandState{
		Workflow: WorkflowIssues,
		Runtime: CommandRuntime{
			GHBin: "gh",
		},
		Issues: IssueCommandState{
			Source: IssueSourceAllOpen,
			Label:  "ghir",
		},
	}

	status, report := refreshActiveQueue(&state, queueResolveOptions{
		CommandOutput: func(name string, args ...string) (string, error) {
			if name != "gh" {
				t.Fatalf("unexpected binary: %s", name)
			}
			return `[{"number":20},{"number":13},{"number":19}]`, nil
		},
	})

	if status != "" {
		t.Fatalf("expected empty queue status on successful all-open resolution, got %q", status)
	}
	if report.HasErrors() {
		t.Fatalf("expected successful queue report, got %#v", report.Items())
	}
	if got, want := state.Issues.ResolvedQueue, []string{"13", "19", "20"}; !slices.Equal(got, want) {
		t.Fatalf("resolved queue mismatch:\n got: %v\nwant: %v", got, want)
	}
	if got, want := state.Issues.StagedQueue, []string{"13", "19", "20"}; !slices.Equal(got, want) {
		t.Fatalf("staged queue mismatch:\n got: %v\nwant: %v", got, want)
	}
}

func TestReadIssueQueueFileBlankPathUsesDefaultLabel(t *testing.T) {
	t.Parallel()

	_, err := readIssueQueueFile("")
	if err == nil {
		t.Fatalf("expected missing file error")
	}
	if got := err.Error(); got != "issue file not found: "+defaults.IssuesFile {
		t.Fatalf("unexpected error: %q", got)
	}
}
