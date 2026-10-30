package pipelines

import (
	"errors"
	"fmt"
	"regexp"
	"strings"

	"github.com/hari-incoming/hazdo/internal/provider"
	"github.com/hari-incoming/hazdo/internal/ui/components"
	"github.com/hari-incoming/hazdo/internal/ui/styles"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var writeClipboard = clipboard.WriteAll

// timestampRegex matches Azure DevOps log timestamps like "2024-02-06T10:00:00.000Z "
var timestampRegex = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d+Z\s*`)

// LogViewerModel represents a scrollable log viewer
type LogViewerModel struct {
	client        provider.Provider
	scope         string
	buildID       int
	logID         int
	title         string
	content       string
	viewport      viewport.Model
	loading       bool
	err           error
	width         int
	height        int
	ready         bool
	spinner       *components.LoadingIndicator
	styles        *styles.Styles
	searchInput   textinput.Model
	searching     bool
	searchQuery   string
	matches       []int
	matchIndex    int
	statusMessage string
}

// NewLogViewerModel creates a new log viewer model with default styles
func NewLogViewerModel(client provider.Provider, scope string, buildID, logID int, title string) *LogViewerModel {
	return NewLogViewerModelWithStyles(client, scope, buildID, logID, title, styles.DefaultStyles())
}

// NewLogViewerModelWithStyles creates a new log viewer model with custom styles
func NewLogViewerModelWithStyles(client provider.Provider, scope string, buildID, logID int, title string, s *styles.Styles) *LogViewerModel {
	spinner := components.NewLoadingIndicator(s)
	spinner.SetMessage(fmt.Sprintf("Loading log for %s...", title))
	searchInput := textinput.New()
	searchInput.Prompt = "/ "
	searchInput.CharLimit = 200

	return &LogViewerModel{
		client:      client,
		scope:       scope,
		buildID:     buildID,
		logID:       logID,
		title:       title,
		loading:     true,
		spinner:     spinner,
		styles:      s,
		searchInput: searchInput,
		matchIndex:  -1,
	}
}

// Init initializes the model and fetches log content
func (m *LogViewerModel) Init() tea.Cmd {
	m.spinner.SetVisible(true)
	return tea.Batch(m.fetchLogContent(), m.spinner.Init())
}

// SetSize sets the viewport size
func (m *LogViewerModel) SetSize(width, height int) {
	m.width = width
	m.height = height

	// Account for header lines rendered in View(): title (1) + separator (1) = 2
	headerLines := 2
	viewportHeight := height - headerLines
	if viewportHeight < 1 {
		viewportHeight = 1
	}
	if m.searching || m.searchQuery != "" {
		viewportHeight--
		if viewportHeight < 1 {
			viewportHeight = 1
		}
	}
	if m.statusMessage != "" {
		viewportHeight--
		if viewportHeight < 1 {
			viewportHeight = 1
		}
	}

	if !m.ready {
		m.viewport = viewport.New(width, viewportHeight)
		m.ready = true
	} else {
		m.viewport.Width = width
		m.viewport.Height = viewportHeight
	}
	m.searchInput.Width = max(width-4, 1)

	// Re-set content if we have it
	if m.content != "" {
		m.viewport.SetContent(m.formatContent())
	}
}

// SetContent sets the log content
func (m *LogViewerModel) SetContent(content string) {
	m.content = content
	m.loading = false
	if m.ready {
		m.viewport.SetContent(m.formatContent())
		m.viewport.GotoTop()
	}
	m.matchIndex = -1
	m.rebuildMatches()
	if m.ready && m.searchQuery != "" {
		m.jumpToNextMatch(0)
	}
}

// SetError sets an error state
func (m *LogViewerModel) SetError(message string) {
	m.err = errors.New(message)
	m.loading = false
}

// GetContent returns the raw log content
func (m *LogViewerModel) GetContent() string {
	return m.content
}

// GetTitle returns the log title
func (m *LogViewerModel) GetTitle() string {
	return m.title
}

// GetBuildID returns the build ID
func (m *LogViewerModel) GetBuildID() int {
	return m.buildID
}

// GetLogID returns the log ID
func (m *LogViewerModel) GetLogID() int {
	return m.logID
}

// IsLoading returns true if content is loading
func (m *LogViewerModel) IsLoading() bool {
	return m.loading
}

// GetError returns any error
func (m *LogViewerModel) GetError() error {
	return m.err
}

// Update handles messages
func (m *LogViewerModel) Update(msg tea.Msg) (*LogViewerModel, tea.Cmd) {
	var cmd tea.Cmd

	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)

	case spinner.TickMsg:
		// Forward spinner tick messages
		if m.loading {
			var spinnerCmd tea.Cmd
			m.spinner, spinnerCmd = m.spinner.Update(msg)
			return m, spinnerCmd
		}

	case copyLogContentMsg:
		if msg.err != nil {
			m.statusMessage = fmt.Sprintf("Could not copy log: %v", msg.err)
		} else {
			m.statusMessage = "Log copied to clipboard"
		}
		m.SetSize(m.width, m.height)
		return m, nil

	case tea.KeyMsg:
		if m.searching {
			switch msg.String() {
			case "esc":
				m.searching = false
				m.searchInput.Blur()
				m.SetSize(m.width, m.height)
				return m, nil
			case "enter":
				m.searching = false
				m.searchInput.Blur()
				m.searchQuery = m.searchInput.Value()
				m.matchIndex = -1
				m.rebuildMatches()
				m.SetSize(m.width, m.height)
				m.jumpToNextMatch(m.viewport.YOffset)
				return m, nil
			default:
				m.searchInput, cmd = m.searchInput.Update(msg)
				return m, cmd
			}
		}

		switch msg.String() {
		case "r":
			m.loading = true
			m.spinner.SetVisible(true)
			m.err = nil
			m.statusMessage = ""
			m.SetSize(m.width, m.height)
			return m, tea.Batch(m.fetchLogContent(), m.spinner.Tick())
		case "c":
			if m.loading || m.err != nil || m.content == "" {
				m.statusMessage = "No log content available to copy"
				m.SetSize(m.width, m.height)
				return m, nil
			}
			return m, m.copyLogContent()
		case "/":
			m.searching = true
			m.searchInput.SetValue("")
			m.SetSize(m.width, m.height)
			return m, m.searchInput.Focus()
		case "n":
			m.jumpToMatch(m.matchIndex + 1)
		case "N":
			m.jumpToMatch(m.matchIndex - 1)
		case "g":
			m.viewport.GotoTop()
		case "G":
			m.viewport.GotoBottom()
		}

	case logContentMsg:
		m.loading = false
		m.spinner.SetVisible(false)
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.SetContent(msg.content)
	}

	// Update viewport for scrolling
	if m.ready {
		m.viewport, cmd = m.viewport.Update(msg)
	}

	return m, cmd
}

// View renders the log viewer
func (m *LogViewerModel) View() string {
	// Helper to wrap content with consistent width
	wrapContent := func(content string) string {
		contentStyle := lipgloss.NewStyle().
			Width(m.width)
		return contentStyle.Render(content)
	}

	if m.err != nil {
		return wrapContent(fmt.Sprintf("Error loading log: %v\n\nPress r to retry, Esc to go back", m.err))
	}

	if m.loading {
		return wrapContent(m.spinner.View())
	}

	if m.content == "" {
		return wrapContent(fmt.Sprintf("Log: %s\n\nNo log content available.\n\nPress Esc to go back", m.title))
	}

	var sb strings.Builder

	// Header
	sb.WriteString(m.styles.Header.Render(fmt.Sprintf("Log: %s", m.title)))
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("─", min(m.width-2, 60)))
	sb.WriteString("\n")
	if m.searching {
		sb.WriteString(m.searchInput.View())
		sb.WriteString("\n")
	} else if m.searchQuery != "" {
		if len(m.matches) == 0 {
			sb.WriteString(fmt.Sprintf("/%s (no matches)", m.searchQuery))
		} else {
			sb.WriteString(fmt.Sprintf("/%s  [%d/%d]", m.searchQuery, m.matchIndex+1, len(m.matches)))
		}
		sb.WriteString("\n")
	}
	if m.statusMessage != "" {
		sb.WriteString(m.statusMessage)
		sb.WriteString("\n")
	}

	// Viewport content
	if m.ready {
		sb.WriteString(m.viewport.View())
	}

	return wrapContent(sb.String())
}

// formatContent formats the log content for display
func (m *LogViewerModel) formatContent() string {
	if m.content == "" {
		return ""
	}

	lines := formatLogLines(m.content)
	return strings.Join(lines, "\n")
}

// GetContextItems returns context bar items for this view
func (m *LogViewerModel) GetContextItems() []components.ContextItem {
	if m.searching {
		return []components.ContextItem{
			{Key: "enter", Description: "find matches"},
			{Key: "esc", Description: "cancel search"},
		}
	}
	return []components.ContextItem{
		{Key: "↑↓/pgup/pgdn", Description: "scroll"},
		{Key: "g/G", Description: "top/bottom"},
		{Key: "/", Description: "search log"},
		{Key: "n/N", Description: "next/previous match"},
		{Key: "c", Description: "copy full log"},
	}
}

// IsSearching reports whether the log viewer is capturing search input.
func (m *LogViewerModel) IsSearching() bool {
	return m.searching
}

func (m *LogViewerModel) rebuildMatches() {
	m.matches = nil
	if m.searchQuery == "" {
		return
	}
	query := strings.ToLower(m.searchQuery)
	for i, line := range formatLogLines(m.content) {
		if strings.Contains(strings.ToLower(line), query) {
			m.matches = append(m.matches, i)
		}
	}
	if m.matchIndex >= len(m.matches) {
		m.matchIndex = -1
	}
}

func (m *LogViewerModel) jumpToNextMatch(start int) {
	if len(m.matches) == 0 {
		m.matchIndex = -1
		return
	}
	for i, line := range m.matches {
		if line >= start {
			m.jumpToMatch(i)
			return
		}
	}
	m.jumpToMatch(0)
}

func (m *LogViewerModel) jumpToMatch(index int) {
	if len(m.matches) == 0 {
		m.matchIndex = -1
		return
	}
	index = (index%len(m.matches) + len(m.matches)) % len(m.matches)
	m.matchIndex = index
	line := m.matches[index]
	if !m.ready {
		return
	}
	if line < m.viewport.YOffset {
		m.viewport.SetYOffset(line)
	} else if line >= m.viewport.YOffset+m.viewport.Height {
		m.viewport.SetYOffset(line - m.viewport.Height + 1)
	}
}

// GetScrollPercent returns the current scroll percentage (0-100)
func (m *LogViewerModel) GetScrollPercent() float64 {
	if !m.ready {
		return 0
	}
	return m.viewport.ScrollPercent() * 100
}

// Messages

type logContentMsg struct {
	content string
	err     error
}

type copyLogContentMsg struct {
	err error
}

func (m *LogViewerModel) copyLogContent() tea.Cmd {
	content := m.content
	return func() tea.Msg {
		return copyLogContentMsg{err: writeClipboard(content)}
	}
}

func (m *LogViewerModel) fetchLogContent() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return logContentMsg{content: "", err: nil}
		}
		content, err := m.client.GetBuildLogContent(m.scope, m.buildID, m.logID)
		return logContentMsg{content: content, err: err}
	}
}

// Helper functions

// formatLogLines splits content into lines and optionally strips timestamps
func formatLogLines(content string) []string {
	if content == "" {
		return []string{}
	}

	// Split by newlines
	rawLines := strings.Split(content, "\n")

	// Filter out empty trailing lines
	var lines []string
	for _, line := range rawLines {
		// Strip Azure DevOps timestamps for cleaner display
		cleanLine := stripTimestamp(line)
		lines = append(lines, cleanLine)
	}

	// Remove trailing empty line if present
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}

	return lines
}

// stripTimestamp removes Azure DevOps timestamp prefix from a log line
func stripTimestamp(line string) string {
	return timestampRegex.ReplaceAllString(line, "")
}
