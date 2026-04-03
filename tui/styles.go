package tui

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type Styles struct {
	noColor      bool
	topBar       lipgloss.Style
	footer       lipgloss.Style
	panelFrame   lipgloss.Style
	panelTitle   lipgloss.Style
	panelBody    lipgloss.Style
	focus        lipgloss.Style
	dim          lipgloss.Style
	ok           lipgloss.Style
	warn         lipgloss.Style
	err          lipgloss.Style
	neutralBadge lipgloss.Style
}

func NewStyles(noColor bool) Styles {
	border := lipgloss.NormalBorder()
	styles := Styles{
		noColor:      noColor,
		topBar:       lipgloss.NewStyle().Padding(0, 1),
		footer:       lipgloss.NewStyle().Padding(0, 1),
		panelFrame:   lipgloss.NewStyle().Border(border).Padding(0, 1),
		panelTitle:   lipgloss.NewStyle(),
		panelBody:    lipgloss.NewStyle(),
		focus:        lipgloss.NewStyle(),
		dim:          lipgloss.NewStyle(),
		ok:           lipgloss.NewStyle(),
		warn:         lipgloss.NewStyle(),
		err:          lipgloss.NewStyle(),
		neutralBadge: lipgloss.NewStyle(),
	}

	if noColor {
		return styles
	}

	styles.topBar = styles.topBar.Background(lipgloss.Color("#12344A")).Foreground(lipgloss.Color("#ECF7FF"))
	styles.footer = styles.footer.Background(lipgloss.Color("#EAF1F4")).Foreground(lipgloss.Color("#13313F"))
	styles.panelFrame = styles.panelFrame.BorderForeground(lipgloss.Color("#89A2AF"))
	styles.panelTitle = styles.panelTitle.Foreground(lipgloss.Color("#58C4DD"))
	styles.focus = styles.focus.Foreground(lipgloss.Color("#58C4DD"))
	styles.dim = styles.dim.Foreground(lipgloss.Color("#8AA0A9"))
	styles.ok = styles.ok.Foreground(lipgloss.Color("#6BCB77"))
	styles.warn = styles.warn.Foreground(lipgloss.Color("#F0B45A"))
	styles.err = styles.err.Foreground(lipgloss.Color("#F06A6A"))
	styles.neutralBadge = styles.neutralBadge.Background(lipgloss.Color("#204B5E")).Foreground(lipgloss.Color("#ECF7FF")).Padding(0, 1)

	return styles
}

func (s Styles) panel(title, body string, width, height int) string {
	frame := s.panelFrame.Copy().Width(width).Height(height)
	innerWidth := max(1, width-frame.GetHorizontalFrameSize())
	innerHeight := max(1, height-frame.GetVerticalFrameSize())

	header := s.panelTitle.Copy().Width(innerWidth).Render(title)
	bodyHeight := max(1, innerHeight-lipgloss.Height(header))
	content := s.panelBody.Copy().Width(innerWidth).Height(bodyHeight).MaxHeight(bodyHeight).Render(body)

	return frame.Render(lipgloss.JoinVertical(lipgloss.Left, header, content))
}

func (s Styles) badge(label string) string {
	if s.noColor {
		return "[" + label + "]"
	}
	return s.neutralBadge.Render(label)
}

func (s Styles) focusText(label string) string {
	if s.noColor {
		return label
	}
	return s.focus.Render(label)
}

func (s Styles) dimText(label string) string {
	if s.noColor {
		return label
	}
	return s.dim.Render(label)
}

func (s Styles) okText(label string) string {
	if s.noColor {
		return label
	}
	return s.ok.Render(label)
}

func (s Styles) warnText(label string) string {
	if s.noColor {
		return label
	}
	return s.warn.Render(label)
}

func (s Styles) errText(label string) string {
	if s.noColor {
		return label
	}
	return s.err.Render(label)
}

func (s Styles) separator() string {
	if s.noColor {
		return " | "
	}
	return "  •  "
}

func (s Styles) keyHint(hint KeyHint) string {
	key := hint.Key
	if !s.noColor {
		key = s.focus.Render(hint.Key)
	}
	label := hint.Label
	if !s.noColor {
		label = s.dim.Render(hint.Label)
	}
	return strings.TrimSpace(key + " " + label)
}
