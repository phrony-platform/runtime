package main

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// Menu action identifiers. Selecting a menu item dispatches by id.
const (
	menuActionComplete = "complete"
	menuActionCancel   = "cancel"
	menuActionDetach   = "detach"
	menuActionQuit     = "quit"
)

var (
	tuiMenuBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("39")).
			Background(lipgloss.Color("236")).
			Padding(1, 2)
	tuiMenuTitleStyle = lipgloss.NewStyle().
				Bold(true).
				Foreground(lipgloss.Color("39")).
				Background(lipgloss.Color("236")).
				MarginBottom(1)
	tuiMenuItemStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("252")).
				Background(lipgloss.Color("236"))
	tuiMenuItemSelectedStyle = lipgloss.NewStyle().
					Bold(true).
					Foreground(lipgloss.Color("231")).
					Background(lipgloss.Color("24"))
	tuiMenuItemDisabledStyle = lipgloss.NewStyle().
					Foreground(lipgloss.Color("240")).
					Background(lipgloss.Color("236"))
	tuiMenuDescStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("245")).
				Background(lipgloss.Color("236"))
	tuiMenuHelpStyle = lipgloss.NewStyle().
				Foreground(lipgloss.Color("244")).
				Background(lipgloss.Color("236")).
				MarginTop(1)
)

// tuiMenuItem is a single selectable action in the TUI action menu.
type tuiMenuItem struct {
	id      string
	label   string
	desc    string
	enabled bool
}

// tuiMenu is a small, self-contained single-select list used as the TUI action
// menu. It is intentionally generic: callers populate items, drive navigation,
// and read back the highlighted item.
type tuiMenu struct {
	title    string
	items    []tuiMenuItem
	selected int
	open     bool
}

// reset opens the menu with the supplied items and selects the first enabled one.
func (mn *tuiMenu) reset(title string, items []tuiMenuItem) {
	mn.title = title
	mn.items = items
	mn.open = true
	mn.selected = 0
	mn.selectFirstEnabled()
}

func (mn *tuiMenu) close() {
	mn.open = false
}

func (mn *tuiMenu) selectFirstEnabled() {
	for i, it := range mn.items {
		if it.enabled {
			mn.selected = i
			return
		}
	}
}

// moveBy advances the selection by delta, skipping disabled items and wrapping
// around. It is a no-op when no item is enabled.
func (mn *tuiMenu) moveBy(delta int) {
	n := len(mn.items)
	if n == 0 || !mn.hasEnabled() {
		return
	}
	idx := mn.selected
	for i := 0; i < n; i++ {
		idx = (idx + delta + n) % n
		if mn.items[idx].enabled {
			mn.selected = idx
			return
		}
	}
}

func (mn *tuiMenu) hasEnabled() bool {
	for _, it := range mn.items {
		if it.enabled {
			return true
		}
	}
	return false
}

// current returns the highlighted item when it is selectable.
func (mn *tuiMenu) current() (tuiMenuItem, bool) {
	if mn.selected < 0 || mn.selected >= len(mn.items) {
		return tuiMenuItem{}, false
	}
	it := mn.items[mn.selected]
	if !it.enabled {
		return tuiMenuItem{}, false
	}
	return it, true
}

// view renders the modal at the given total width (including border + padding).
func (mn *tuiMenu) view(width int) string {
	innerWidth := width - tuiMenuBoxStyle.GetHorizontalFrameSize()
	if innerWidth < 10 {
		innerWidth = 10
	}
	rows := make([]string, 0, len(mn.items)+2)
	rows = append(rows, tuiMenuTitleStyle.Render(mn.title))
	for i, it := range mn.items {
		rows = append(rows, mn.itemView(i, it, innerWidth))
	}
	rows = append(rows, tuiMenuHelpStyle.Render("↑↓ select  ·  Enter confirm  ·  Esc close"))
	inner := lipgloss.JoinVertical(lipgloss.Left, rows...)
	return tuiMenuBoxStyle.Width(width).Render(inner)
}

func (mn *tuiMenu) itemView(idx int, it tuiMenuItem, width int) string {
	rowStyle := tuiMenuItemStyle
	switch {
	case !it.enabled:
		rowStyle = tuiMenuItemDisabledStyle
	case idx == mn.selected:
		rowStyle = tuiMenuItemSelectedStyle
	}
	cursor := "  "
	if it.enabled && idx == mn.selected {
		cursor = "> "
	}
	text := cursor + it.label
	if it.desc != "" {
		text += "  — " + it.desc
	}
	return rowStyle.Width(width).Render(text)
}

// buildSessionMenuItems returns the action menu items for the current session
// state. Terminal sessions only expose a way to leave the viewer.
func (m *runTUI) buildSessionMenuItems() []tuiMenuItem {
	if m.sessionTerminal() {
		return []tuiMenuItem{
			{id: menuActionQuit, label: "Close viewer", desc: "Exit and return to the shell", enabled: true},
		}
	}
	return []tuiMenuItem{
		{id: menuActionComplete, label: "Complete session", desc: "Stop sending input and let the agent finish", enabled: true},
		{id: menuActionCancel, label: "Cancel session", desc: "Abort the run immediately", enabled: m.canCancelSession()},
		{id: menuActionDetach, label: "Detach", desc: "Leave the session running in the background", enabled: true},
	}
}

// sessionTerminal reports whether the session has reached a state where no
// further actions can be taken on the run itself.
func (m *runTUI) sessionTerminal() bool {
	if m.readOnly {
		return true
	}
	switch m.status {
	case "done", "failed", "cancelled", "error":
		return true
	}
	return false
}

func (m *runTUI) canCancelSession() bool {
	return m.cancel != nil && strings.TrimSpace(m.sessionID) != ""
}

func (m *runTUI) openMenu() {
	if m.menu.open {
		return
	}
	m.menu.reset("Session actions", m.buildSessionMenuItems())
	m.input.Blur()
	m.layout()
}

func (m *runTUI) closeMenu() {
	if !m.menu.open {
		return
	}
	m.menu.close()
	if m.awaitingInput && !m.inputBlocked() {
		m.input.Focus()
	}
	m.layout()
}
