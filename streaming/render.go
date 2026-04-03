package streaming

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

const (
	StreamViewPretty = "pretty"
	StreamViewRaw    = "raw"
)

type Renderer interface {
	ConsumeLine(line string) []string
	FinalLines() []string
}

type RawRenderer struct{}

func (r *RawRenderer) ConsumeLine(line string) []string {
	return []string{line}
}

func (r *RawRenderer) FinalLines() []string {
	return nil
}

type CodexPrettyRenderer struct{}

func (r *CodexPrettyRenderer) ConsumeLine(line string) []string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}
	if !strings.HasPrefix(trimmed, "{") {
		return []string{line}
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return []string{line}
	}

	eventType, _ := payload["type"].(string)
	switch eventType {
	case "item.started":
		item := asAnyMap(payload["item"])
		if item == nil || getStringField(item, "type") != "command_execution" {
			return nil
		}
		cmd := truncateForConsole(normalizeWhitespace(getStringField(item, "command")), 120)
		if cmd == "" {
			return []string{"[cmd] started"}
		}
		return []string{fmt.Sprintf("[cmd] %s", cmd)}
	case "item.completed":
		item := asAnyMap(payload["item"])
		if item == nil {
			return nil
		}

		switch getStringField(item, "type") {
		case "command_execution":
			exitCode, hasExitCode := getIntField(item, "exit_code")
			status := strings.ToLower(getStringField(item, "status"))
			if (hasExitCode && exitCode == 0 && (status == "" || status == "completed")) ||
				(!hasExitCode && status == "completed") {
				return nil
			}

			cmd := truncateForConsole(normalizeWhitespace(getStringField(item, "command")), 120)
			header := "[cmd failed]"
			if hasExitCode {
				header = fmt.Sprintf("[cmd failed exit=%d]", exitCode)
			}
			if status != "" {
				header += " status=" + status
			}

			var lines []string
			if cmd != "" {
				lines = append(lines, fmt.Sprintf("%s %s", header, cmd))
			} else {
				lines = append(lines, header)
			}

			aggregatedOutput := strings.TrimSpace(getStringField(item, "aggregated_output"))
			for _, outputLine := range compactMultiline(aggregatedOutput, 4, 360) {
				lines = append(lines, "  "+outputLine)
			}
			return lines
		case "agent_message":
			text := strings.TrimSpace(getStringField(item, "text"))
			if text == "" {
				return nil
			}
			return prefixMultiline("[assistant] ", "  ", text)
		default:
			return nil
		}
	case "error":
		code := getStringField(payload, "code")
		message := strings.TrimSpace(getStringField(payload, "message"))
		switch {
		case code != "" && message != "":
			return []string{fmt.Sprintf("[error] %s: %s", code, message)}
		case message != "":
			return []string{"[error] " + message}
		case code != "":
			return []string{"[error] " + code}
		default:
			return []string{"[error] received error event"}
		}
	case "turn.completed":
		return []string{"[done] turn completed"}
	default:
		return nil
	}
}

func (r *CodexPrettyRenderer) FinalLines() []string {
	return nil
}

type CursorAgentPrettyRenderer struct{}

func (r *CursorAgentPrettyRenderer) ConsumeLine(line string) []string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}
	if !strings.HasPrefix(trimmed, "{") {
		return []string{line}
	}

	var payload map[string]any
	if err := json.Unmarshal([]byte(trimmed), &payload); err != nil {
		return []string{line}
	}

	eventType, _ := payload["type"].(string)
	if eventType != "result" {
		return nil
	}

	subtype := getStringField(payload, "subtype")
	isError := payload["is_error"] == true
	durationMs, hasDuration := getIntField(payload, "duration_ms")
	result := strings.TrimSpace(getStringField(payload, "result"))

	var lines []string

	if isError {
		errMsg := subtype
		if errMsg == "" {
			errMsg = "failed"
		}
		lines = append(lines, "[error] "+errMsg)
	} else if hasDuration && durationMs > 0 {
		sec := float64(durationMs) / 1000
		lines = append(lines, fmt.Sprintf("[done] %.1fs", sec))
	} else {
		lines = append(lines, "[done]")
	}

	if result != "" {
		for _, l := range prefixMultiline("[assistant] ", "  ", result) {
			lines = append(lines, l)
		}
	}

	return lines
}

func (r *CursorAgentPrettyRenderer) FinalLines() []string {
	return nil
}

type GeminiPrettyRenderer struct {
	jsonBuf    []string
	braceCount int
}

func (r *GeminiPrettyRenderer) ConsumeLine(line string) []string {
	trimmed := strings.TrimSpace(line)
	if trimmed == "" {
		return nil
	}
	if strings.HasPrefix(trimmed, "YOLO mode is enabled") || trimmed == "Loaded cached credentials." {
		return nil
	}
	if strings.HasPrefix(trimmed, "Error executing tool ") {
		return []string{line}
	}
	if r.braceCount > 0 || strings.HasPrefix(trimmed, "{") {
		r.jsonBuf = append(r.jsonBuf, line)
		for _, c := range trimmed {
			if c == '{' {
				r.braceCount++
			} else if c == '}' {
				r.braceCount--
			}
		}
		if r.braceCount != 0 {
			return nil
		}
		block := strings.Join(r.jsonBuf, "\n")
		r.jsonBuf = nil
		r.braceCount = 0
		return r.formatGeminiResult(block)
	}
	return nil
}

func (r *GeminiPrettyRenderer) formatGeminiResult(block string) []string {
	var payload struct {
		Response string `json:"response"`
		Stats    struct {
			Models map[string]struct {
				API struct {
					TotalRequests  int `json:"totalRequests"`
					TotalErrors    int `json:"totalErrors"`
					TotalLatencyMs int `json:"totalLatencyMs"`
				} `json:"api"`
				Tokens struct {
					Input      int `json:"input"`
					Prompt     int `json:"prompt"`
					Candidates int `json:"candidates"`
					Total      int `json:"total"`
					Cached     int `json:"cached"`
					Thoughts   int `json:"thoughts"`
					Tool       int `json:"tool"`
				} `json:"tokens"`
			} `json:"models"`
			Tools struct {
				TotalCalls      int `json:"totalCalls"`
				TotalSuccess    int `json:"totalSuccess"`
				TotalFail       int `json:"totalFail"`
				TotalDurationMs int `json:"totalDurationMs"`
			} `json:"tools"`
			Files struct {
				TotalLinesAdded   int `json:"totalLinesAdded"`
				TotalLinesRemoved int `json:"totalLinesRemoved"`
			} `json:"files"`
		} `json:"stats"`
	}
	if err := json.Unmarshal([]byte(block), &payload); err != nil {
		return []string{block}
	}
	var lines []string
	if payload.Response != "" {
		for i, l := range compactMultiline(payload.Response, 0, 0) {
			if i == 0 {
				lines = append(lines, "[assistant] "+l)
				continue
			}
			lines = append(lines, "  "+l)
		}
	}
	for _, model := range payload.Stats.Models {
		lines = append(lines, fmt.Sprintf("  tokens: %d (cached %d) · requests: %d · latency: %.1fs",
			model.Tokens.Total, model.Tokens.Cached, model.API.TotalRequests, float64(model.API.TotalLatencyMs)/1000))
		break
	}
	tools := payload.Stats.Tools
	lines = append(lines, fmt.Sprintf("  tools: %d calls, %d ok, %d fail · %.1fs",
		tools.TotalCalls, tools.TotalSuccess, tools.TotalFail, float64(tools.TotalDurationMs)/1000))
	files := payload.Stats.Files
	if files.TotalLinesAdded != 0 || files.TotalLinesRemoved != 0 {
		lines = append(lines, fmt.Sprintf("  files: +%d −%d", files.TotalLinesAdded, files.TotalLinesRemoved))
	}
	return lines
}

func (r *GeminiPrettyRenderer) FinalLines() []string {
	if len(r.jsonBuf) == 0 {
		return nil
	}
	block := strings.Join(r.jsonBuf, "\n")
	r.jsonBuf = nil
	return r.formatGeminiResult(block)
}

func NewRenderer(agent, streamView string) (Renderer, string) {
	if streamView == StreamViewRaw {
		return &RawRenderer{}, ""
	}
	switch agent {
	case "codex":
		return &CodexPrettyRenderer{}, ""
	case "cursor-agent":
		return &CursorAgentPrettyRenderer{}, ""
	case "gemini":
		return &GeminiPrettyRenderer{}, ""
	default:
		return &RawRenderer{}, fmt.Sprintf(
			"Stream view %q is not implemented for %s yet; showing raw output.",
			streamView,
			agentDisplayName(agent),
		)
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

func asAnyMap(value any) map[string]any {
	m, ok := value.(map[string]any)
	if !ok {
		return nil
	}
	return m
}

func getStringField(fields map[string]any, key string) string {
	if fields == nil {
		return ""
	}
	value, ok := fields[key]
	if !ok || value == nil {
		return ""
	}
	text, ok := value.(string)
	if !ok {
		return ""
	}
	return text
}

func getIntField(fields map[string]any, key string) (int, bool) {
	if fields == nil {
		return 0, false
	}
	value, ok := fields[key]
	if !ok || value == nil {
		return 0, false
	}
	switch v := value.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	case json.Number:
		n, err := strconv.Atoi(v.String())
		if err != nil {
			return 0, false
		}
		return n, true
	default:
		return 0, false
	}
}

func normalizeWhitespace(value string) string {
	return strings.Join(strings.Fields(value), " ")
}

func truncateForConsole(value string, maxLen int) string {
	if maxLen <= 0 || len(value) <= maxLen {
		return value
	}
	if maxLen <= 3 {
		return value[:maxLen]
	}
	return value[:maxLen-3] + "..."
}

func compactMultiline(value string, maxLines int, maxChars int) []string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return nil
	}
	if maxChars > 0 && len(trimmed) > maxChars {
		trimmed = truncateForConsole(trimmed, maxChars)
	}
	lines := strings.Split(trimmed, "\n")
	if maxLines > 0 && len(lines) > maxLines {
		lines = append(lines[:maxLines], "...")
	}
	for i := range lines {
		lines[i] = strings.TrimSpace(lines[i])
	}
	return lines
}

func prefixMultiline(firstPrefix, nextPrefix, value string) []string {
	lines := strings.Split(strings.TrimSpace(value), "\n")
	if len(lines) == 0 {
		return nil
	}
	for i := range lines {
		lines[i] = strings.TrimRight(lines[i], "\r")
	}
	var formatted []string
	for idx, line := range lines {
		if idx == 0 {
			formatted = append(formatted, firstPrefix+line)
			continue
		}
		formatted = append(formatted, nextPrefix+line)
	}
	return formatted
}
