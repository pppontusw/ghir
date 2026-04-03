package tui

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
)

type logAccessMsg struct {
	Path     string
	Fallback bool
	Err      error
}

func (m Model) openCurrentLog() tea.Cmd {
	logPath, err := resolveLogPath(m.options.RepoRoot, m.run.selectedLogPath())
	if err != nil {
		return func() tea.Msg {
			return logAccessMsg{Err: err}
		}
	}

	pagerArgs, ok := resolvePagerCommand(m.options)
	if !ok {
		return func() tea.Msg {
			return logAccessMsg{Path: logPath, Fallback: true}
		}
	}

	command := exec.Command(pagerArgs[0], append(pagerArgs[1:], logPath)...)
	command.Dir = m.options.RepoRoot

	execProcess := m.options.ExecProcess
	if execProcess == nil {
		execProcess = tea.ExecProcess
	}

	return execProcess(command, func(err error) tea.Msg {
		return logAccessMsg{Path: logPath, Err: err}
	})
}

func resolveLogPath(repoRoot, currentLogPath string) (string, error) {
	currentLogPath = strings.TrimSpace(currentLogPath)
	if currentLogPath == "" {
		return "", fmt.Errorf("no log path is available for the current item yet")
	}

	resolved := currentLogPath
	if !filepath.IsAbs(resolved) {
		resolved = filepath.Join(repoRoot, resolved)
	}
	if _, err := os.Stat(resolved); err != nil {
		return "", err
	}
	return resolved, nil
}

func resolvePagerCommand(opts Options) ([]string, bool) {
	getenv := opts.Getenv
	if getenv == nil {
		getenv = os.Getenv
	}
	lookPath := opts.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}

	if configured := strings.Fields(strings.TrimSpace(getenv("PAGER"))); len(configured) > 0 {
		if resolved, err := lookPath(configured[0]); err == nil {
			configured[0] = resolved
			return configured, true
		}
	}

	for _, candidate := range [][]string{{"less", "-R"}, {"more"}} {
		resolved, err := lookPath(candidate[0])
		if err == nil {
			candidate[0] = resolved
			return candidate, true
		}
	}

	return nil, false
}
