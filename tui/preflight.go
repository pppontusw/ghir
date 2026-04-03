package tui

import (
	"fmt"
	"os/exec"
	"strings"
)

type PreflightOptions struct {
	RepoRoot      string
	LookPath      func(string) (string, error)
	CommandOutput func(string, ...string) (string, error)
}

func RunPreflightChecks(state CommandState, opts PreflightOptions) DiagnosticReport {
	var report DiagnosticReport

	lookPath := opts.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	commandOutput := opts.CommandOutput
	if commandOutput == nil {
		commandOutput = defaultCommandOutput
	}

	gitAvailable := checkBinaryAvailability("git", "git", lookPath, &report)
	if gitAvailable {
		if !insideGitRepo(opts.RepoRoot, commandOutput) {
			report.add(SeverityError, "must run inside a git repository", "Open the TUI from a repository root or a directory inside a git repo.")
		} else {
			report.add(SeverityInfo, "Git repository detected.", "")
		}
	}

	if requiresGH(state) {
		checkBinaryAvailability("gh", effectiveGHBin(state.Runtime), lookPath, &report)
	} else {
		report.add(SeverityInfo, "GitHub CLI is not required for file workflow runs.", "")
	}

	checkBinaryAvailability(normalizeAgent(state.Runtime.Agent), effectiveAgentBin(state.Runtime), lookPath, &report)

	if !gitAvailable || !insideGitRepo(opts.RepoRoot, commandOutput) {
		return report
	}

	if skipDirtyCheck(state) {
		report.add(SeverityInfo, "Working tree cleanliness check is skipped for --dry-run.", "")
		return report
	}

	dirty, err := workingTreeDirty(opts.RepoRoot, commandOutput)
	if err != nil {
		report.add(SeverityError, fmt.Sprintf("check git status: %v", err), "Run `git status` to inspect the repository state.")
		return report
	}
	if dirty {
		switch normalizeCommandWorkflow(state.Workflow) {
		case WorkflowImprove:
			report.add(SeverityError, "uncommitted changes detected; commit or stash before running improve", "Review `git status` and commit or stash pending changes.")
		default:
			report.add(SeverityError, "uncommitted changes detected. Commit or stash before running.", "Review `git status` and commit or stash pending changes.")
		}
		return report
	}

	report.add(SeverityInfo, "Working tree is clean.", "")
	return report
}

func checkBinaryAvailability(name, binPath string, lookPath func(string) (string, error), report *DiagnosticReport) bool {
	if _, err := lookPath(binPath); err != nil {
		report.add(SeverityError, fmt.Sprintf("missing required binary '%s': %s not found in PATH", name, binPath), fmt.Sprintf("Install %s or update the configured binary path.", name))
		return false
	}
	report.add(SeverityInfo, fmt.Sprintf("Required binary available: %s (%s).", name, binPath), "")
	return true
}

func insideGitRepo(repoRoot string, commandOutput func(string, ...string) (string, error)) bool {
	if strings.TrimSpace(repoRoot) == "" {
		return false
	}
	_, err := commandOutput("git", "-C", repoRoot, "rev-parse", "--show-toplevel")
	return err == nil
}

func workingTreeDirty(repoRoot string, commandOutput func(string, ...string) (string, error)) (bool, error) {
	out, err := commandOutput("git", "-C", repoRoot, "status", "--porcelain")
	if err != nil {
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

func requiresGH(state CommandState) bool {
	if normalizeCommandWorkflow(state.Workflow) != WorkflowFiles {
		return true
	}
	return strings.TrimSpace(state.Files.Strategy) != "" && strings.TrimSpace(state.Files.Strategy) != DefaultStrategy
}

func skipDirtyCheck(state CommandState) bool {
	switch normalizeCommandWorkflow(state.Workflow) {
	case WorkflowFiles:
		return state.Files.DryRun
	case WorkflowImprove:
		return false
	default:
		return state.Issues.DryRun
	}
}

func effectiveAgentBin(runtime CommandRuntime) string {
	switch normalizeAgent(runtime.Agent) {
	case "codex":
		if strings.TrimSpace(runtime.CodexBin) != "" {
			return runtime.CodexBin
		}
		return defaultCodexBin
	case "gemini":
		if strings.TrimSpace(runtime.GeminiBin) != "" {
			return runtime.GeminiBin
		}
		return defaultGeminiBin
	case "cursor-agent":
		if strings.TrimSpace(runtime.CursorBin) != "" {
			return runtime.CursorBin
		}
		return defaultCursorBin
	case "pi":
		if strings.TrimSpace(runtime.PiBin) != "" {
			return runtime.PiBin
		}
		return defaultPiBin
	default:
		if strings.TrimSpace(runtime.ClaudeBin) != "" {
			return runtime.ClaudeBin
		}
		return defaultClaudeBin
	}
}

func effectiveGHBin(runtime CommandRuntime) string {
	if strings.TrimSpace(runtime.GHBin) != "" {
		return runtime.GHBin
	}
	return defaultGHBin
}

func defaultCommandOutput(name string, args ...string) (string, error) {
	cmd := exec.Command(name, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), err
	}
	return string(output), nil
}
