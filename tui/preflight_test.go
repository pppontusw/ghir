package tui

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestRunPreflightChecks(t *testing.T) {
	t.Parallel()

	gitBin, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("git must be installed for preflight tests: %v", err)
	}

	t.Run("repo required", func(t *testing.T) {
		report := RunPreflightChecks(CommandState{
			Workflow: WorkflowFiles,
			Runtime: CommandRuntime{
				Agent:     "claude",
				ClaudeBin: "go",
			},
			Files: FileCommandState{
				Source:   FileSourceAllFiles,
				AllFiles: "tasks",
			},
		}, PreflightOptions{
			RepoRoot: t.TempDir(),
			LookPath: allowBins(gitBin, "go"),
		})

		if !reportContains(report, "must run inside a git repository") {
			t.Fatalf("expected git repo error, got %#v", report.Items())
		}
	})

	t.Run("missing agent binary", func(t *testing.T) {
		repo := initGitRepo(t)
		report := RunPreflightChecks(CommandState{
			Workflow: WorkflowFiles,
			Runtime: CommandRuntime{
				Agent:     "claude",
				ClaudeBin: "missing-claude",
			},
			Files: FileCommandState{
				Source:   FileSourceAllFiles,
				AllFiles: "tasks",
			},
		}, PreflightOptions{
			RepoRoot: repo,
			LookPath: allowBins(gitBin),
		})

		if !reportContains(report, "missing required binary 'claude': missing-claude not found in PATH") {
			t.Fatalf("expected missing binary error, got %#v", report.Items())
		}
	})

	t.Run("missing pi binary", func(t *testing.T) {
		repo := initGitRepo(t)
		report := RunPreflightChecks(CommandState{
			Workflow: WorkflowFiles,
			Runtime: CommandRuntime{
				Agent: "pi",
				PiBin: "missing-pi",
			},
			Files: FileCommandState{
				Source:   FileSourceAllFiles,
				AllFiles: "tasks",
			},
		}, PreflightOptions{
			RepoRoot: repo,
			LookPath: allowBins(gitBin),
		})

		if !reportContains(report, "missing required binary 'pi': missing-pi not found in PATH") {
			t.Fatalf("expected missing pi binary error, got %#v", report.Items())
		}
	})

	t.Run("skips gh for files workflow", func(t *testing.T) {
		repo := initGitRepo(t)
		report := RunPreflightChecks(CommandState{
			Workflow: WorkflowFiles,
			Runtime: CommandRuntime{
				Agent:     "claude",
				ClaudeBin: "go",
			},
			Files: FileCommandState{
				Source:   FileSourceAllFiles,
				AllFiles: "tasks",
			},
		}, PreflightOptions{
			RepoRoot: repo,
			LookPath: allowBins(gitBin, "go"),
		})

		if reportContains(report, "missing required binary 'gh'") {
			t.Fatalf("did not expect gh requirement for file workflow: %#v", report.Items())
		}
		if !reportContains(report, "GitHub CLI is not required for file workflow runs.") {
			t.Fatalf("expected skip info, got %#v", report.Items())
		}
	})

	t.Run("requires gh for file pr strategy", func(t *testing.T) {
		repo := initGitRepo(t)
		report := RunPreflightChecks(CommandState{
			Workflow: WorkflowFiles,
			Runtime: CommandRuntime{
				Agent:     "claude",
				ClaudeBin: "go",
			},
			Files: FileCommandState{
				Source:   FileSourceAllFiles,
				AllFiles: "tasks",
				Strategy: "pr-per-pass",
			},
		}, PreflightOptions{
			RepoRoot: repo,
			LookPath: allowBins(gitBin, "go"),
		})

		if !reportContains(report, "missing required binary 'gh': gh not found in PATH") {
			t.Fatalf("expected gh requirement for PR strategy, got %#v", report.Items())
		}
	})

	t.Run("dirty working tree has actionable hint", func(t *testing.T) {
		repo := initGitRepo(t)
		if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("dirty\n"), 0o644); err != nil {
			t.Fatalf("write dirty file: %v", err)
		}

		report := RunPreflightChecks(CommandState{
			Workflow: WorkflowIssues,
			Runtime: CommandRuntime{
				Agent:     "claude",
				ClaudeBin: "go",
				GHBin:     "go",
			},
			Issues: IssueCommandState{
				Source: IssueSourceAllOpen,
			},
		}, PreflightOptions{
			RepoRoot: repo,
			LookPath: allowBins(gitBin, "go"),
		})

		if !reportContains(report, "uncommitted changes detected. Commit or stash before running.") {
			t.Fatalf("expected dirty tree error, got %#v", report.Items())
		}
		if !reportContains(report, "Review `git status` and commit or stash pending changes.") {
			t.Fatalf("expected dirty tree hint, got %#v", report.Items())
		}
	})
}

func initGitRepo(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	runGit(t, dir, "init")
	runGit(t, dir, "config", "user.email", "test@example.com")
	runGit(t, dir, "config", "user.name", "Test User")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("init\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, dir, "add", "README.md")
	runGit(t, dir, "commit", "-m", "init")
	return dir
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()

	cmd := exec.Command("git", args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v failed: %v\n%s", args, err, string(output))
	}
}

func allowBins(gitPath string, extra ...string) func(string) (string, error) {
	allowed := map[string]string{
		"git": gitPath,
	}
	for _, name := range extra {
		allowed[name] = "/usr/bin/" + name
	}

	return func(name string) (string, error) {
		if path, ok := allowed[name]; ok {
			return path, nil
		}
		return "", exec.ErrNotFound
	}
}
