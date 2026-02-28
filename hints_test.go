package main

import (
	"strings"
	"testing"
)

func TestReadIssuesFileHint(t *testing.T) {
	t.Parallel()

	_, err := readIssuesFile("nonexistent_issues.txt")
	if err == nil {
		t.Fatal("expected error, got nil")
	}

	expectedHint := "Hint: create the file, pass --issues <list>, or use --all-open"
	if !strings.Contains(err.Error(), expectedHint) {
		t.Errorf("error %q should contain hint %q", err.Error(), expectedHint)
	}
}

func TestGHAuthFailureHint(t *testing.T) {
	t.Parallel()

	r := &runner{
		opts: options{
			GHBin: "bash",
		},
		repoRoot: ".",
	}

	// Fake gh auth failure
	script := `echo 'gh: To authenticate, please run ` + "`" + `gh auth login` + "`" + `.' >&2; exit 1`
	_, err := r.commandOutput("bash", "-c", script)
	if err == nil {
		t.Fatal("expected error")
	}

	expectedHint := "Hint: run `gh auth login` to authenticate or check repository permissions."
	if !strings.Contains(err.Error(), expectedHint) {
		t.Errorf("error %q should contain hint %q", err.Error(), expectedHint)
	}
}

func TestGHAuthFailureHint_NoAuthError(t *testing.T) {
	t.Parallel()

	r := &runner{
		opts: options{
			GHBin: "bash",
		},
		repoRoot: ".",
	}

	// Fake non-auth gh failure
	script := `echo "gh: exit 1." >&2; exit 1`
	_, err := r.commandOutput("bash", "-c", script)
	if err == nil {
		t.Fatal("expected error")
	}

	unexpectedHint := "Hint: run `gh auth login`"
	if strings.Contains(err.Error(), unexpectedHint) {
		t.Errorf("error %q should not contain hint %q", err.Error(), unexpectedHint)
	}
}
