package main

import (
	"slices"
	"testing"
)

func TestBuildAgentCommandUsesBuiltInDefaultModelWhenUnset(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		agent    string
		wantArgv []string
	}{
		{
			name:     "claude default model",
			agent:    "claude",
			wantArgv: []string{"claude", "--print", "--verbose", "--output-format", "text", "--dangerously-skip-permissions", "--model", "opus"},
		},
		{
			name:     "codex default model",
			agent:    "codex",
			wantArgv: []string{"codex", "exec", "--json", "--dangerously-bypass-approvals-and-sandbox", "--model", "gpt-5.4"},
		},
		{
			name:     "gemini default model",
			agent:    "gemini",
			wantArgv: []string{"gemini", "--output-format", "json", "--yolo", "-m", "gemini-3.1-pro-preview"},
		},
		{
			name:     "cursor-agent default model",
			agent:    "cursor-agent",
			wantArgv: []string{"cursor-agent", "--print", "--output-format", "json", "--force", "--model", "auto"},
		},
		{
			name:     "pi default model",
			agent:    "pi",
			wantArgv: []string{"pi", "-p", "--model", "github-copilot/gpt-5.4:high"},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			cfg := defaultAgentConfig()
			cfg.Agent = tt.agent
			r := &runner{opts: options{agentConfig: cfg}}

			cmd, err := r.buildAgentCommand("prompt")
			if err != nil {
				t.Fatalf("buildAgentCommand returned unexpected error: %v", err)
			}

			if got, want := cmd.Args[:len(tt.wantArgv)], tt.wantArgv; !slices.Equal(got, want) {
				t.Fatalf("argv prefix mismatch:\n got: %v\nwant: %v", got, want)
			}
		})
	}
}

func TestBuildAgentCommandKeepsExplicitModelOverride(t *testing.T) {
	t.Parallel()

	cfg := defaultAgentConfig()
	cfg.Agent = "codex"
	cfg.Model = "gpt-5.3-codex"
	r := &runner{opts: options{agentConfig: cfg}}

	cmd, err := r.buildAgentCommand("prompt")
	if err != nil {
		t.Fatalf("buildAgentCommand returned unexpected error: %v", err)
	}

	if got, want := cmd.Args[5], "gpt-5.3-codex"; got != want {
		t.Fatalf("explicit model mismatch: got %q want %q", got, want)
	}
}
