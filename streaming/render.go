package streaming

import (
	"encoding/json"
	"fmt"
	"sort"
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

const (
	piStreamSoftFlushChars = 72
	piStreamHardFlushChars = 120
)

type PiPrettyRenderer struct {
	textBlocks     map[int]*piStreamBlock
	thinkingBlocks map[int]*piStreamBlock
	tools          map[string]*piToolStream
}

type piStreamBlock struct {
	firstPrefix string
	nextPrefix  string
	pending     string
	emitted     bool
}

func newPiStreamBlock(firstPrefix, nextPrefix string) *piStreamBlock {
	return &piStreamBlock{firstPrefix: firstPrefix, nextPrefix: nextPrefix}
}

func (b *piStreamBlock) ConsumeText(text string) []string {
	if text == "" {
		return nil
	}
	b.pending += text
	return b.consume(false)
}

func (b *piStreamBlock) Flush() []string {
	return b.consume(true)
}

func (b *piStreamBlock) hasData() bool {
	return b.emitted || b.pending != ""
}

func (b *piStreamBlock) consume(force bool) []string {
	var lines []string
	for {
		newlineIndex := strings.IndexByte(b.pending, '\n')
		if newlineIndex >= 0 {
			line := strings.TrimRight(b.pending[:newlineIndex], "\r")
			if line != "" {
				lines = append(lines, b.format(line))
			}
			b.pending = b.pending[newlineIndex+1:]
			continue
		}

		if !force {
			cut := piStreamChunkIndex(b.pending)
			if cut <= 0 {
				break
			}
			chunk := b.pending[:cut]
			rest := b.pending[cut:]
			if strings.HasSuffix(chunk, " ") || strings.HasSuffix(chunk, "\t") {
				chunk = strings.TrimRight(chunk, " \t")
				rest = strings.TrimLeft(rest, " \t")
			}
			chunk = strings.TrimRight(chunk, "\r")
			if chunk != "" {
				lines = append(lines, b.format(chunk))
			}
			b.pending = rest
			continue
		}

		remaining := strings.TrimRight(b.pending, "\r")
		if remaining != "" {
			lines = append(lines, b.format(remaining))
		}
		b.pending = ""
		break
	}
	return lines
}

func (b *piStreamBlock) format(line string) string {
	prefix := b.firstPrefix
	if b.emitted {
		prefix = b.nextPrefix
	}
	b.emitted = true
	return prefix + line
}

type piToolStream struct {
	name        string
	args        map[string]any
	headerShown bool
	output      *piStreamBlock
	lastText    string
}

func newPiToolStream(name string, args map[string]any) *piToolStream {
	return &piToolStream{
		name:   name,
		args:   args,
		output: newPiStreamBlock("  ", "  "),
	}
}

func (t *piToolStream) updateArgs(args map[string]any) {
	if args != nil {
		t.args = args
	}
}

func (t *piToolStream) ConsumeSnapshot(text string) []string {
	if text == t.lastText {
		return nil
	}
	if strings.HasPrefix(text, t.lastText) {
		delta := text[len(t.lastText):]
		t.lastText = text
		return t.output.ConsumeText(delta)
	}

	t.lastText = text
	t.output = newPiStreamBlock("  ", "  ")
	var lines []string
	if text != "" {
		lines = append(lines, "  [output refreshed]")
		lines = append(lines, t.output.ConsumeText(text)...)
	}
	return lines
}

func (t *piToolStream) Flush() []string {
	return t.output.Flush()
}

func (r *PiPrettyRenderer) ConsumeLine(line string) []string {
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

	switch getStringField(payload, "type") {
	case "session", "agent_start", "agent_end", "turn_start", "turn_end", "message_start", "queue_update":
		return nil
	case "message_update":
		return r.consumeMessageUpdate(payload)
	case "message_end":
		return r.consumeMessageEnd(payload)
	case "tool_execution_start":
		return r.consumeToolExecutionStart(payload)
	case "tool_execution_update":
		return r.consumeToolExecutionUpdate(payload)
	case "tool_execution_end":
		return r.consumeToolExecutionEnd(payload)
	case "auto_retry_start":
		attempt, _ := getIntField(payload, "attempt")
		maxAttempts, _ := getIntField(payload, "maxAttempts")
		message := truncateForConsole(normalizeWhitespace(getStringField(payload, "errorMessage")), 160)
		if attempt > 0 && maxAttempts > 0 {
			if message != "" {
				return []string{fmt.Sprintf("[retry] %d/%d after %s", attempt, maxAttempts, message)}
			}
			return []string{fmt.Sprintf("[retry] %d/%d", attempt, maxAttempts)}
		}
		if message != "" {
			return []string{"[retry] " + message}
		}
		return []string{"[retry] transient agent error"}
	case "auto_retry_end":
		if payload["success"] == true {
			return []string{"[retry] recovered"}
		}
		message := strings.TrimSpace(getStringField(payload, "finalError"))
		if message == "" {
			return []string{"[retry] failed"}
		}
		return []string{"[retry] failed: " + truncateForConsole(normalizeWhitespace(message), 160)}
	case "compaction_start":
		reason := getStringField(payload, "reason")
		if reason == "" {
			return []string{"[compacting]"}
		}
		return []string{"[compacting] " + reason}
	case "compaction_end":
		if payload["aborted"] == true {
			return []string{"[compacting] aborted"}
		}
		if errMsg := strings.TrimSpace(getStringField(payload, "errorMessage")); errMsg != "" {
			return []string{"[compacting] failed: " + truncateForConsole(normalizeWhitespace(errMsg), 160)}
		}
		if payload["willRetry"] == true {
			return []string{"[compacting] done, retrying"}
		}
		return []string{"[compacting] done"}
	case "extension_error":
		errMsg := strings.TrimSpace(getStringField(payload, "error"))
		if errMsg == "" {
			return []string{"[extension error]"}
		}
		return []string{"[extension error] " + truncateForConsole(normalizeWhitespace(errMsg), 160)}
	default:
		return nil
	}
}

func (r *PiPrettyRenderer) FinalLines() []string {
	lines := r.flushMessageBlocks()
	if len(r.tools) == 0 {
		return lines
	}
	ids := make([]string, 0, len(r.tools))
	for id := range r.tools {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	for _, id := range ids {
		lines = append(lines, r.tools[id].Flush()...)
	}
	return lines
}

func (r *PiPrettyRenderer) consumeMessageUpdate(payload map[string]any) []string {
	event := asAnyMap(payload["assistantMessageEvent"])
	if event == nil {
		return nil
	}

	eventType := getStringField(event, "type")
	contentIndex, _ := getIntField(event, "contentIndex")
	switch eventType {
	case "text_start":
		r.ensureTextBlock(contentIndex)
		return nil
	case "text_delta":
		return r.ensureTextBlock(contentIndex).ConsumeText(getStringField(event, "delta"))
	case "text_end":
		block := r.ensureTextBlock(contentIndex)
		if content := getStringField(event, "content"); content != "" && !block.hasData() {
			return r.finishTextBlock(contentIndex, content)
		}
		return r.finishTextBlock(contentIndex, "")
	case "thinking_start":
		r.ensureThinkingBlock(contentIndex)
		return nil
	case "thinking_delta":
		return r.ensureThinkingBlock(contentIndex).ConsumeText(getStringField(event, "delta"))
	case "thinking_end":
		block := r.ensureThinkingBlock(contentIndex)
		if content := getStringField(event, "content"); content != "" && !block.hasData() {
			return r.finishThinkingBlock(contentIndex, content)
		}
		return r.finishThinkingBlock(contentIndex, "")
	case "done", "error":
		return r.flushMessageBlocks()
	default:
		return nil
	}
}

func (r *PiPrettyRenderer) consumeMessageEnd(payload map[string]any) []string {
	message := asAnyMap(payload["message"])
	if getStringField(message, "role") != "assistant" {
		return nil
	}

	lines := r.flushMessageBlocks()
	stopReason := getStringField(message, "stopReason")
	if stopReason == "error" || stopReason == "aborted" {
		errMsg := strings.TrimSpace(getStringField(message, "errorMessage"))
		if errMsg == "" {
			errMsg = "assistant " + stopReason
		}
		lines = append(lines, "[error] "+truncateForConsole(normalizeWhitespace(errMsg), 160))
	}
	return lines
}

func (r *PiPrettyRenderer) consumeToolExecutionStart(payload map[string]any) []string {
	id := getStringField(payload, "toolCallId")
	toolName := getStringField(payload, "toolName")
	if id == "" || toolName == "" {
		return nil
	}
	tool := r.ensureTool(id, toolName, asAnyMap(payload["args"]))
	if tool.headerShown {
		return nil
	}
	tool.headerShown = true
	return []string{formatPiToolStart(toolName, tool.args)}
}

func (r *PiPrettyRenderer) consumeToolExecutionUpdate(payload map[string]any) []string {
	id := getStringField(payload, "toolCallId")
	toolName := getStringField(payload, "toolName")
	if id == "" || toolName == "" {
		return nil
	}
	tool := r.ensureTool(id, toolName, asAnyMap(payload["args"]))
	var lines []string
	if !tool.headerShown {
		tool.headerShown = true
		lines = append(lines, formatPiToolStart(toolName, tool.args))
	}
	if toolName == "bash" {
		lines = append(lines, tool.ConsumeSnapshot(extractToolText(asAnyMap(payload["partialResult"])))...)
	}
	return lines
}

func (r *PiPrettyRenderer) consumeToolExecutionEnd(payload map[string]any) []string {
	id := getStringField(payload, "toolCallId")
	toolName := getStringField(payload, "toolName")
	if id == "" || toolName == "" {
		return nil
	}
	tool := r.ensureTool(id, toolName, nil)
	var lines []string
	if !tool.headerShown {
		tool.headerShown = true
		lines = append(lines, formatPiToolStart(toolName, tool.args))
	}

	result := asAnyMap(payload["result"])
	resultText := extractToolText(result)
	if toolName == "bash" {
		lines = append(lines, tool.ConsumeSnapshot(resultText)...)
		lines = append(lines, tool.Flush()...)
		if payload["isError"] == true && strings.TrimSpace(resultText) == "" {
			lines = append(lines, "  [error] command failed")
		}
		delete(r.tools, id)
		return lines
	}

	if payload["isError"] == true {
		if resultText == "" {
			lines = append(lines, "  [error] tool failed")
		} else {
			for _, line := range compactMultiline(resultText, 4, 360) {
				lines = append(lines, "  "+line)
			}
		}
	}
	delete(r.tools, id)
	return lines
}

func (r *PiPrettyRenderer) ensureTextBlock(index int) *piStreamBlock {
	if r.textBlocks == nil {
		r.textBlocks = make(map[int]*piStreamBlock)
	}
	block, ok := r.textBlocks[index]
	if !ok {
		block = newPiStreamBlock("[assistant] ", "  ")
		r.textBlocks[index] = block
	}
	return block
}

func (r *PiPrettyRenderer) ensureThinkingBlock(index int) *piStreamBlock {
	if r.thinkingBlocks == nil {
		r.thinkingBlocks = make(map[int]*piStreamBlock)
	}
	block, ok := r.thinkingBlocks[index]
	if !ok {
		block = newPiStreamBlock("[thinking] ", "  ")
		r.thinkingBlocks[index] = block
	}
	return block
}

func (r *PiPrettyRenderer) finishTextBlock(index int, content string) []string {
	block := r.ensureTextBlock(index)
	var lines []string
	if content != "" && !block.hasData() {
		lines = append(lines, block.ConsumeText(content)...)
	}
	lines = append(lines, block.Flush()...)
	delete(r.textBlocks, index)
	return lines
}

func (r *PiPrettyRenderer) finishThinkingBlock(index int, content string) []string {
	block := r.ensureThinkingBlock(index)
	var lines []string
	if content != "" && !block.hasData() {
		lines = append(lines, block.ConsumeText(content)...)
	}
	lines = append(lines, block.Flush()...)
	delete(r.thinkingBlocks, index)
	return lines
}

func (r *PiPrettyRenderer) flushMessageBlocks() []string {
	var lines []string
	if len(r.thinkingBlocks) > 0 {
		indices := make([]int, 0, len(r.thinkingBlocks))
		for index := range r.thinkingBlocks {
			indices = append(indices, index)
		}
		sort.Ints(indices)
		for _, index := range indices {
			lines = append(lines, r.thinkingBlocks[index].Flush()...)
			delete(r.thinkingBlocks, index)
		}
	}
	if len(r.textBlocks) > 0 {
		indices := make([]int, 0, len(r.textBlocks))
		for index := range r.textBlocks {
			indices = append(indices, index)
		}
		sort.Ints(indices)
		for _, index := range indices {
			lines = append(lines, r.textBlocks[index].Flush()...)
			delete(r.textBlocks, index)
		}
	}
	return lines
}

func (r *PiPrettyRenderer) ensureTool(id, name string, args map[string]any) *piToolStream {
	if r.tools == nil {
		r.tools = make(map[string]*piToolStream)
	}
	tool, ok := r.tools[id]
	if !ok {
		tool = newPiToolStream(name, args)
		r.tools[id] = tool
	} else {
		tool.name = name
		tool.updateArgs(args)
	}
	return tool
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
	case "pi":
		return &PiPrettyRenderer{}, ""
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

func piStreamChunkIndex(value string) int {
	if len(value) < piStreamSoftFlushChars {
		return 0
	}
	limit := len(value)
	if limit > piStreamHardFlushChars {
		limit = piStreamHardFlushChars
	}
	if cut := strings.LastIndexAny(value[:limit], " \t"); cut >= piStreamSoftFlushChars/2 {
		return cut + 1
	}
	if len(value) >= piStreamHardFlushChars {
		return piStreamHardFlushChars
	}
	return 0
}

func formatPiToolStart(toolName string, args map[string]any) string {
	switch toolName {
	case "bash":
		command := truncateForConsole(normalizeWhitespace(getStringField(args, "command")), 120)
		if command == "" {
			return "[bash]"
		}
		return "[bash] " + command
	case "read":
		path := getStringField(args, "path")
		offset, hasOffset := getIntField(args, "offset")
		limit, hasLimit := getIntField(args, "limit")
		rangeSuffix := ""
		if hasOffset || hasLimit {
			var parts []string
			if hasOffset {
				parts = append(parts, fmt.Sprintf("offset=%d", offset))
			}
			if hasLimit {
				parts = append(parts, fmt.Sprintf("limit=%d", limit))
			}
			rangeSuffix = " (" + strings.Join(parts, ", ") + ")"
		}
		if path == "" {
			return "[read]" + rangeSuffix
		}
		return "[read] " + truncateForConsole(path, 120) + rangeSuffix
	case "edit":
		path := getStringField(args, "path")
		if path == "" {
			return "[edit]"
		}
		return "[edit] " + truncateForConsole(path, 120)
	case "write":
		path := getStringField(args, "path")
		if path == "" {
			return "[write]"
		}
		return "[write] " + truncateForConsole(path, 120)
	case "grep":
		pattern := truncateForConsole(getStringField(args, "pattern"), 72)
		path := truncateForConsole(getStringField(args, "path"), 72)
		switch {
		case pattern != "" && path != "":
			return fmt.Sprintf("[grep] %s @ %s", pattern, path)
		case pattern != "":
			return "[grep] " + pattern
		case path != "":
			return "[grep] " + path
		default:
			return "[grep]"
		}
	case "find":
		pattern := truncateForConsole(getStringField(args, "pattern"), 72)
		path := truncateForConsole(getStringField(args, "path"), 72)
		switch {
		case pattern != "" && path != "":
			return fmt.Sprintf("[find] %s @ %s", pattern, path)
		case pattern != "":
			return "[find] " + pattern
		case path != "":
			return "[find] " + path
		default:
			return "[find]"
		}
	case "ls":
		path := getStringField(args, "path")
		if path == "" {
			return "[ls]"
		}
		return "[ls] " + truncateForConsole(path, 120)
	default:
		return "[tool] " + toolName
	}
}

func extractToolText(result map[string]any) string {
	if result == nil {
		return ""
	}
	items, ok := result["content"].([]any)
	if !ok {
		return ""
	}
	parts := make([]string, 0, len(items))
	for _, item := range items {
		fields := asAnyMap(item)
		if getStringField(fields, "type") != "text" {
			continue
		}
		text := getStringField(fields, "text")
		if text != "" {
			parts = append(parts, text)
		}
	}
	return strings.Join(parts, "\n")
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
