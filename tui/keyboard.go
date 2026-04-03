package tui

import (
	"fmt"
	"strings"
	"unicode"

	tea "github.com/charmbracelet/bubbletea"
)

type helpState struct {
	visible bool
}

type searchScope string

const (
	searchScopeConfigureQueue searchScope = "configure_queue"
	searchScopeRunQueue       searchScope = "run_queue"
	searchScopeSummaryFailed  searchScope = "summary_failed"
)

type searchState struct {
	active bool
	scope  searchScope
	query  string
	status string
}

func keyIs(msg tea.KeyMsg, values ...string) bool {
	current := msg.String()
	for _, value := range values {
		if current == value {
			return true
		}
	}
	return false
}

func keyIsRune(msg tea.KeyMsg, values ...rune) bool {
	if msg.Type != tea.KeyRunes || len(msg.Runes) != 1 {
		return false
	}

	current := unicode.ToLower(msg.Runes[0])
	for _, value := range values {
		if current == unicode.ToLower(value) {
			return true
		}
	}
	return false
}

func keyIsHelp(msg tea.KeyMsg) bool {
	return keyIs(msg, "?", "shift+/") || keyIsRune(msg, '?')
}

func keyIsSlash(msg tea.KeyMsg) bool {
	return keyIs(msg, "/") || (msg.Type == tea.KeyRunes && len(msg.Runes) == 1 && msg.Runes[0] == '/')
}

func searchScopeLabel(scope searchScope) string {
	switch scope {
	case searchScopeRunQueue:
		return "Run queue"
	case searchScopeSummaryFailed:
		return "Failed items"
	default:
		return "Configure queue"
	}
}

func searchPlaceholder(scope searchScope) string {
	switch scope {
	case searchScopeSummaryFailed:
		return "Type to jump to a failed item."
	default:
		return "Type to jump to a queue item."
	}
}

func describeSearchResult(query string, index, total int) string {
	return fmt.Sprintf("Match %d / %d for %q.", index+1, total, strings.TrimSpace(query))
}
