package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	appshell "ghir/tui"
)

func runTUI(opts tuiOptions, repoRoot string) error {
	branch, err := currentBranch(repoRoot)
	if err != nil {
		return err
	}
	executable, err := os.Executable()
	if err != nil {
		return err
	}

	app := appshell.Options{
		RepoRoot:      repoRoot,
		Branch:        branch,
		Workflow:      opts.Mode,
		Preset:        opts.LoadPreset,
		Agent:         defaultAgentConfig().Agent,
		NoColor:       opts.NoColor || os.Getenv("NO_COLOR") != "",
		RunExecutable: executable,
		RunEnv:        filterTUIRunEnv(os.Environ()),
		RunTickEvery:  time.Second,
	}

	if os.Getenv("GHIR_TUI_TEST") != "" {
		width := readTUIDimension("GHIR_TUI_WIDTH", 120)
		height := readTUIDimension("GHIR_TUI_HEIGHT", 36)
		fmt.Println(appshell.Snapshot(app, width, height))
		return nil
	}

	_, err = tea.NewProgram(appshell.NewModel(app)).Run()
	return err
}

func filterTUIRunEnv(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		if strings.HasPrefix(entry, "GHIR_TUI_TEST=") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

func readTUIDimension(name string, fallback int) int {
	value := os.Getenv(name)
	if value == "" {
		return fallback
	}

	dimension, err := strconv.Atoi(value)
	if err != nil || dimension <= 0 {
		return fallback
	}

	return dimension
}
