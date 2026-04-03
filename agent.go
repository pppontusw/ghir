package main

import (
	"fmt"
	"os/exec"
	"slices"
	"strings"

	"ghir/tui"
)

const (
	streamViewPretty = "pretty"
	streamViewRaw    = "raw"
)

// agentConfig holds agent CLI and streaming options shared by issue mode and improve mode.
type agentConfig struct {
	Agent         string
	Model         string
	ClaudeBin     string
	CodexBin      string
	GeminiBin     string
	CursorBin     string
	PiBin         string
	GHBin         string
	StreamView    string
	NoColor       bool
	WaitBufferSec int
}

func defaultAgentConfig() agentConfig {
	return agentConfig{
		Agent:         "claude",
		ClaudeBin:     "claude",
		CodexBin:      "codex",
		GeminiBin:     "gemini",
		CursorBin:     "cursor-agent",
		PiBin:         "pi",
		GHBin:         "gh",
		StreamView:    streamViewPretty,
		WaitBufferSec: defaultSessionBufferSec,
	}
}

func getAgentBin(cfg agentConfig) string {
	switch cfg.Agent {
	case "claude":
		return cfg.ClaudeBin
	case "codex":
		return cfg.CodexBin
	case "gemini":
		return cfg.GeminiBin
	case "cursor-agent":
		return cfg.CursorBin
	case "pi":
		return cfg.PiBin
	default:
		return cfg.ClaudeBin
	}
}

func agentDisplayName(agent string) string {
	switch agent {
	case "codex":
		return "Codex"
	case "gemini":
		return "Gemini"
	case "cursor-agent":
		return "Cursor Agent"
	case "pi":
		return "pi"
	default:
		return "Claude"
	}
}

func defaultModelForAgent(agent string) string {
	switch agent {
	case "codex":
		return "gpt-5.4"
	case "gemini":
		return "gemini-3.1-pro-preview"
	case "cursor-agent":
		return "auto"
	case "pi":
		return "github-copilot/gpt-5.4:high"
	default:
		return "opus"
	}
}

func effectiveModelForAgent(agent, configured string) string {
	if model := strings.TrimSpace(configured); model != "" {
		return model
	}
	return defaultModelForAgent(agent)
}

// appendModelIfSet appends (flag, model) to args when model is non-empty.
func appendModelIfSet(args []string, model, flag string) []string {
	if model != "" {
		return append(args, flag, model)
	}
	return args
}

func (r *runner) buildAgentCommand(prompt string) (*exec.Cmd, error) {
	agentBin := getAgentBin(r.opts.agentConfig)
	model := effectiveModelForAgent(r.opts.Agent, r.opts.Model)
	switch r.opts.Agent {
	case "claude":
		args := appendModelIfSet([]string{
			"--print",
			"--verbose",
			"--output-format", "text",
			"--dangerously-skip-permissions",
		}, model, "--model")
		cmd := exec.Command(agentBin, args...)
		cmd.Stdin = strings.NewReader(prompt)
		return cmd, nil
	case "codex":
		args := appendModelIfSet([]string{
			"exec",
			"--json",
			"--dangerously-bypass-approvals-and-sandbox",
		}, model, "--model")
		args = append(args, prompt)
		cmd := exec.Command(agentBin, args...)
		return cmd, nil
	case "gemini":
		args := appendModelIfSet([]string{
			"--output-format",
			"json",
			"--yolo",
		}, model, "-m")
		args = append(args, "-p", prompt)
		cmd := exec.Command(agentBin, args...)
		return cmd, nil
	case "cursor-agent":
		args := appendModelIfSet([]string{
			"--print",
			"--output-format",
			"json",
			"--force",
		}, model, "--model")
		args = append(args, prompt)
		cmd := exec.Command(agentBin, args...)
		return cmd, nil
	case "pi":
		args := appendModelIfSet([]string{"-p"}, model, "--model")
		args = append(args, prompt)
		cmd := exec.Command(agentBin, args...)
		return cmd, nil
	default:
		return nil, fmt.Errorf("unsupported agent: %s", r.opts.Agent)
	}
}

func validateAgentConfig(cfg agentConfig) error {
	if !slices.Contains(tui.ValidAgents, cfg.Agent) {
		return fmt.Errorf("--agent must be one of: %s", strings.Join(tui.ValidAgents, ", "))
	}
	if !slices.Contains(tui.ValidStreamViews, cfg.StreamView) {
		return fmt.Errorf("--stream-view must be one of: %s", strings.Join(tui.ValidStreamViews, ", "))
	}
	return nil
}
