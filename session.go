package main

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const (
	defaultFallbackWaitSec = 1800
	geminiCapacity429Wait  = 900 // 15 minutes for "no capacity" / 429 from Gemini
)

var (
	claudeSessionLimitPattern    = regexp.MustCompile(`(?is)(out of\s+(extra\s+)?usage|hit your\s+(usage\s+)?limit|exceeded.*(usage|limit)|usage\s+limit|rate\s+limit).*resets?`)
	timeOfDayResetPattern        = regexp.MustCompile(`(?i)(?:resets?|try\s+again)\s+(?:at\s+)?[A-Za-z]*\s*(\d{1,2})(?::(\d{2}))?\s*(am|pm)?\s*\(?(UTC)?\)?`)
	codexResetTsPattern          = regexp.MustCompile(`(?i)resets_at\\?"?[:\s]+(\d+)`)
	codexResetInSecPattern       = regexp.MustCompile(`(?i)resets_in_seconds\\?"?[:\s]+(\d+)`)
	geminiSessionLimitPattern    = regexp.MustCompile(`(?is)(terminalquotaerror|quota\s+exceeded|rate\s+limit|no\s+capacity\s+available|retryablequotaerror)`)
	geminiResetDurationRegex     = regexp.MustCompile(`(?i)resets?\s+(?:after\s+)?(\d+h)?(\d+m)?(\d+s)?`)
	geminiDurationPartRegex      = regexp.MustCompile(`(?i)(\d+)([hms])`)
	internalServerErrorPattern   = regexp.MustCompile(`(?i)(internal\s+server\s+error|500\s+internal|502\s+bad\s+gateway|503\s+service\s+unavailable|504\s+gateway\s+timeout|overloaded)`)
	expiredAuthTokenErrorPattern = regexp.MustCompile(`(?is)(\bide\s+token\s+expired\b|\bunauthorized\b.*\btoken\s+expired\b|\btoken\s+expired\b.*\bunauthorized\b|\b401\b.*\btoken\s+expired\b)`)
)

func waitDuration(logOutput string, now time.Time, bufferSec int, agent string) (int, time.Time) {
	if agent == "codex" {
		return waitDurationCodex(logOutput, now, bufferSec)
	}
	if agent == "gemini" {
		return waitDurationGemini(logOutput, now, bufferSec)
	}
	return waitDurationClaude(logOutput, now, bufferSec)
}

func parseTimeOfDayMatch(match []string, now time.Time, bufferSec int) (int, time.Time, bool) {
	hour, err := strconv.Atoi(match[1])
	if err != nil {
		return 0, time.Time{}, false
	}

	minute := 0
	if match[2] != "" {
		minute, err = strconv.Atoi(match[2])
		if err != nil || minute < 0 || minute > 59 {
			return 0, time.Time{}, false
		}
	}

	ampm := strings.ToLower(strings.TrimSpace(match[3]))
	switch ampm {
	case "am":
		if hour == 12 {
			hour = 0
		}
	case "pm":
		if hour != 12 {
			hour += 12
		}
	case "":
		if hour < 0 || hour > 23 {
			return 0, time.Time{}, false
		}
	default:
		return 0, time.Time{}, false
	}

	if hour < 0 || hour > 23 {
		return 0, time.Time{}, false
	}

	reset := time.Date(now.Year(), now.Month(), now.Day(), hour, minute, 0, 0, time.UTC)
	if !reset.After(now) {
		reset = reset.Add(24 * time.Hour)
	}

	withBuffer := reset.Add(time.Duration(bufferSec) * time.Second)
	wait := int(withBuffer.Sub(now).Seconds())
	if wait <= 0 {
		return 0, time.Time{}, false
	}
	return wait, withBuffer, true
}

func waitDurationClaude(logOutput string, now time.Time, bufferSec int) (int, time.Time) {
	match := timeOfDayResetPattern.FindStringSubmatch(logOutput)
	if len(match) > 0 {
		wait, withBuffer, ok := parseTimeOfDayMatch(match, now, bufferSec)
		if ok {
			return wait, withBuffer
		}
	}
	wait := defaultFallbackWaitSec
	return wait, now.Add(time.Duration(wait) * time.Second)
}

func waitDurationCodex(logOutput string, now time.Time, bufferSec int) (int, time.Time) {
	match := codexResetTsPattern.FindStringSubmatch(logOutput)
	if len(match) >= 2 {
		seconds, err := strconv.ParseInt(match[1], 10, 64)
		if err == nil && seconds > 0 {
			reset := time.Unix(seconds, 0).UTC()
			withBuffer := reset.Add(time.Duration(bufferSec) * time.Second)
			wait := int(withBuffer.Sub(now).Seconds())
			if wait > 0 {
				return wait, withBuffer
			}
		}
	}

	secondsMatch := codexResetInSecPattern.FindStringSubmatch(logOutput)
	if len(secondsMatch) >= 2 {
		waitSeconds, err := strconv.Atoi(secondsMatch[1])
		if err == nil && waitSeconds > 0 {
			wait := waitSeconds + bufferSec
			return wait, now.Add(time.Duration(wait) * time.Second)
		}
	}

	timeMatch := timeOfDayResetPattern.FindStringSubmatch(logOutput)
	if len(timeMatch) > 0 {
		wait, withBuffer, ok := parseTimeOfDayMatch(timeMatch, now, bufferSec)
		if ok {
			return wait, withBuffer
		}
	}

	wait := defaultFallbackWaitSec
	return wait, now.Add(time.Duration(wait) * time.Second)
}

func waitDurationGemini(logOutput string, now time.Time, bufferSec int) (int, time.Time) {
	match := geminiResetDurationRegex.FindStringSubmatch(logOutput)
	if len(match) >= 4 {
		durationText := strings.Join([]string{match[1], match[2], match[3]}, "")
		if durationText != "" {
			durationSeconds := parseGeminiDurationSeconds(durationText)
			if durationSeconds > 0 {
				wait := durationSeconds + bufferSec
				return wait, now.Add(time.Duration(wait) * time.Second)
			}
		}
	}

	// "No capacity available" / 429 from Gemini: retry after 15 minutes
	lower := strings.ToLower(logOutput)
	if strings.Contains(lower, "no capacity") || strings.Contains(lower, "retryablequotaerror") || strings.Contains(lower, "code: 429") {
		wait := geminiCapacity429Wait + bufferSec
		return wait, now.Add(time.Duration(wait) * time.Second)
	}

	wait := defaultFallbackWaitSec
	return wait, now.Add(time.Duration(wait) * time.Second)
}

// isGemini429CapacityLog returns true if the log is from Gemini failing with 429 / no capacity.
// The CLI prints this in multiple forms (JSON error body, stack trace, RetryableQuotaError).
func isGemini429CapacityLog(logOutput string) bool {
	lower := strings.ToLower(logOutput)
	// Phrases that appear in the actual Gemini CLI output when capacity is exhausted
	return strings.Contains(lower, "no capacity available") ||
		strings.Contains(lower, "retryablequotaerror") ||
		strings.Contains(lower, "model_capacity_exhausted") ||
		(strings.Contains(lower, "resource_exhausted") && strings.Contains(lower, "429")) ||
		strings.Contains(lower, "ratelimitexceeded")
}

func detectSessionLimit(logOutput, agent string, exitCode int) bool {
	if agent == "codex" {
		if detectCodexErrorEventLimit(logOutput) {
			return true
		}
		if exitCode == 0 {
			return false
		}
		lower := strings.ToLower(logOutput)
		if strings.Contains(lower, "usage_limit_reached") {
			return true
		}
		if strings.Contains(lower, "usage limit") {
			return strings.Contains(lower, "resets_at") ||
				strings.Contains(lower, "resets_in_seconds") ||
				strings.Contains(lower, "http 429") ||
				strings.Contains(lower, "too many requests") ||
				strings.Contains(lower, "hit your usage limit")
		}
		return false
	}
	if agent == "gemini" {
		if exitCode != 0 && isGemini429CapacityLog(logOutput) {
			return true
		}
		if detectGeminiErrorPayloadLimit(logOutput) {
			return true
		}
		if exitCode == 0 {
			return false
		}
		return geminiSessionLimitPattern.MatchString(logOutput)
	}
	if agent == "cursor-agent" || agent == "pi" {
		return false
	}
	return claudeSessionLimitPattern.MatchString(logOutput)
}

func detectInternalServerError(logOutput string) bool {
	return internalServerErrorPattern.MatchString(logOutput)
}

func detectExpiredAuthTokenError(logOutput string, exitCode int) bool {
	if exitCode == 0 {
		return false
	}
	return expiredAuthTokenErrorPattern.MatchString(logOutput)
}

func detectRetryableAgentError(logOutput, agent string, exitCode int) (string, bool) {
	if detectInternalServerError(logOutput) {
		return "internal server error", true
	}
	if detectExpiredAuthTokenError(logOutput, exitCode) {
		if agent == "pi" {
			return "expired IDE token", true
		}
		return "expired auth token", true
	}
	return "", false
}

// handleSessionLimitRetry checks for session limit. If detected, commits partial progress if dirty,
// waits for reset, and returns (true, nil). If commit fails, returns (false, err).
// If no session limit, returns (false, nil).
func (r *runner) handleSessionLimitRetry(logOutput string, exitCode int, context string, partialCommitMsg string) (shouldRetry bool, err error) {
	if !detectSessionLimit(logOutput, r.opts.Agent, exitCode) {
		return false, nil
	}
	if dirtyNow, dirtyErr := r.workingTreeDirty(); dirtyErr == nil && dirtyNow {
		r.printf(r.colors.Yellow, "Session limit hit %s. Committing partial progress...\n", context)
		if commitErr := r.commitAll(partialCommitMsg); commitErr != nil {
			return false, fmt.Errorf("could not commit partial progress: %w", commitErr)
		}
	}
	waitSeconds, resetTime := waitDuration(logOutput, time.Now().UTC(), r.opts.WaitBufferSec, r.opts.Agent)
	r.waitForSessionReset(waitSeconds, resetTime)
	return true, nil
}

// forEachJSONLine iterates over lines in logOutput that look like JSON objects.
// For each parseable line, it calls fn with the unmarshaled payload. If fn returns true,
// iteration stops and forEachJSONLine returns true. Otherwise it returns false.
func forEachJSONLine(logOutput string, fn func(payload map[string]any) bool) bool {
	for _, raw := range strings.Split(logOutput, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || !strings.HasPrefix(line, "{") {
			continue
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(line), &payload); err != nil {
			continue
		}
		if fn(payload) {
			return true
		}
	}
	return false
}

func detectCodexErrorEventLimit(logOutput string) bool {
	return forEachJSONLine(logOutput, func(payload map[string]any) bool {
		eventType, _ := payload["type"].(string)
		if eventType != "error" {
			return false
		}
		if code, ok := payload["code"].(string); ok {
			lowerCode := strings.ToLower(code)
			if strings.Contains(lowerCode, "usage_limit_reached") || strings.Contains(lowerCode, "usage limit") {
				return true
			}
		}
		if message, ok := payload["message"].(string); ok {
			lowerMessage := strings.ToLower(message)
			if strings.Contains(lowerMessage, "usage_limit_reached") || strings.Contains(lowerMessage, "usage limit") {
				return true
			}
		}
		if _, hasReset := payload["resets_at"]; hasReset {
			return true
		}
		return false
	})
}

func detectGeminiErrorPayloadLimit(logOutput string) bool {
	return forEachJSONLine(logOutput, func(payload map[string]any) bool {
		isError, ok := payload["is_error"].(bool)
		if !ok || !isError {
			return false
		}
		var messageParts []string
		if result, ok := payload["result"].(string); ok {
			messageParts = append(messageParts, result)
		}
		if message, ok := payload["message"].(string); ok {
			messageParts = append(messageParts, message)
		}
		combined := strings.Join(messageParts, " ")
		return geminiSessionLimitPattern.MatchString(combined)
	})
}

func parseGeminiDurationSeconds(durationText string) int {
	matches := geminiDurationPartRegex.FindAllStringSubmatch(strings.ToLower(durationText), -1)
	if len(matches) == 0 {
		return 0
	}

	total := 0
	for _, m := range matches {
		if len(m) < 3 {
			continue
		}
		value, err := strconv.Atoi(m[1])
		if err != nil || value < 0 {
			return 0
		}
		switch m[2] {
		case "h":
			total += value * 3600
		case "m":
			total += value * 60
		case "s":
			total += value
		}
	}

	return total
}
