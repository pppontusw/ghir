package main

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"strings"

	"ghir/tui"
)

// issueDetails holds title and body for an issue or file-based task (e.g. for prompt building).
type issueDetails struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func splitCSVTrimmed(value string) []string {
	parts := strings.Split(value, ",")
	items := make([]string, 0, len(parts))
	for _, part := range parts {
		item := strings.TrimSpace(part)
		if item == "" {
			continue
		}
		items = append(items, item)
	}
	return items
}

// dedupeStringsPreservingOrder returns a new slice with duplicate strings removed (first occurrence kept).
func dedupeStringsPreservingOrder(slice []string) []string {
	seen := make(map[string]struct{})
	out := make([]string, 0, len(slice))
	for _, s := range slice {
		if _, ok := seen[s]; ok {
			continue
		}
		seen[s] = struct{}{}
		out = append(out, s)
	}
	return out
}

func parseCSVIssues(value string) ([]string, error) {
	var candidates []string
	for _, id := range splitCSVTrimmed(value) {
		if !tui.MatchIssueNumber(id) {
			return nil, fmt.Errorf("invalid issue in --issues: %q", id)
		}
		candidates = append(candidates, id)
	}
	issues := dedupeStringsPreservingOrder(candidates)
	if len(issues) == 0 {
		return nil, fmt.Errorf("no issues found in --issues")
	}
	return issues, nil
}

func readIssuesFile(path string) ([]string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("issue file not found: %s\nHint: create the file, pass --issues <list>, or use --all-open", path)
		}
		return nil, fmt.Errorf("read issues file: %w", err)
	}

	var candidates []string
	for i, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		id := fields[0]
		if !tui.MatchIssueNumber(id) {
			return nil, fmt.Errorf("invalid issue id at %s:%d: %q", path, i+1, id)
		}
		candidates = append(candidates, id)
	}
	issues := dedupeStringsPreservingOrder(candidates)
	if len(issues) == 0 {
		return nil, fmt.Errorf("no issue ids found in %s", path)
	}
	return issues, nil
}

func sortStringsNumeric(values []string) {
	sort.Slice(values, func(i, j int) bool {
		return tui.LessNumericIssueOrPath(values[i], values[j])
	})
}
