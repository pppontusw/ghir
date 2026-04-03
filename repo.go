package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"ghir/defaults"
)

// commitCoAuthorSuffix is appended to commit messages produced by the runner or improve flow.
const commitCoAuthorSuffix = "\n\nCo-Authored-By: Claude Opus 4.6 <noreply@anthropic.com>"

func findRepoRoot() (string, error) {
	cmd := exec.Command("git", "rev-parse", "--show-toplevel")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("must run inside a git repository")
	}
	return strings.TrimSpace(string(output)), nil
}

func currentBranch(repoRoot string) (string, error) {
	cmd := exec.Command("git", "-C", repoRoot, "rev-parse", "--abbrev-ref", "HEAD")
	output, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("determine current branch: %w", err)
	}

	branch := strings.TrimSpace(string(output))
	if branch == "" {
		return "", fmt.Errorf("determine current branch: empty result")
	}

	return branch, nil
}

func defaultOrResolvePath(repoRoot, current, defaultRel string) string {
	if current == "" {
		return filepath.Join(repoRoot, defaultRel)
	}
	return resolvePath(repoRoot, current)
}

func applyRepoDefaults(opts *options, repoRoot string) {
	opts.IssuesFile = defaultOrResolvePath(repoRoot, opts.IssuesFile, defaults.IssuesFile)
	opts.LogDir = defaultOrResolvePath(repoRoot, opts.LogDir, defaults.LogDir)
	if opts.DoneFile == "" {
		opts.DoneFile = filepath.Join(opts.LogDir, defaults.DoneFileName)
	} else {
		opts.DoneFile = resolvePath(repoRoot, opts.DoneFile)
	}

	if opts.PromptTemplate != "" {
		opts.PromptTemplate = resolvePath(repoRoot, opts.PromptTemplate)
		return
	}

	candidate := filepath.Join(repoRoot, defaults.PromptTemplate)
	if _, err := os.Stat(candidate); err == nil {
		opts.PromptTemplate = candidate
	}
}

func resolvePath(repoRoot, value string) string {
	if filepath.IsAbs(value) {
		return value
	}
	return filepath.Join(repoRoot, value)
}

func repoRelativePath(repoRoot, path string) string {
	if repoRoot == "" {
		return path
	}

	rel, err := filepath.Rel(repoRoot, path)
	if err != nil {
		return path
	}

	return rel
}

func ensureFile(path string) error {
	f, err := os.OpenFile(path, os.O_CREATE, 0o644)
	if err != nil {
		return err
	}
	return f.Close()
}

func loadDoneSet(path string) (map[string]struct{}, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read done file: %w", err)
	}
	done := make(map[string]struct{})
	for _, raw := range strings.Split(string(data), "\n") {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		done[id] = struct{}{}
	}
	return done, nil
}
