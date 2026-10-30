package pullrequests

import (
	"fmt"
	"strings"

	"github.com/hari-incoming/hazdo/internal/azdevops"
	"github.com/hari-incoming/hazdo/internal/browser"
	"github.com/hari-incoming/hazdo/internal/diff"
	"github.com/hari-incoming/hazdo/internal/provider"
	"github.com/hari-incoming/hazdo/internal/ui/components"
	"github.com/hari-incoming/hazdo/internal/ui/display"
	"github.com/hari-incoming/hazdo/internal/ui/styles"
	"github.com/atotto/clipboard"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// openURL is a package-level seam so tests can intercept browser launches.
var openURL = browser.Open

var writeClipboard = clipboard.WriteAll

// openURLResultMsg is sent when an attempt to open a URL in the browser completes.
type openURLResultMsg struct {
	err error
}

type copyPRURLResultMsg struct {
	err error
}

type copyPRCommitsResultMsg struct {
	err error
}

// DetailModel represents the PR detail view showing description, reviewers, and changed files
type DetailModel struct {
	client                provider.Provider
	pr                    provider.PullRequest
	threads               []provider.Thread
	changedFiles          []provider.IterationChange
	commentCounts         map[string]int // filePath -> comment count
	fileIndex             int
	loading               bool
	threadsLoaded         bool
	filesLoaded           bool
	err                   error
	width                 int
	height                int
	viewport              viewport.Model
	ready                 bool
	statusMessage         string
	spinner               *components.LoadingIndicator
	styles                *styles.Styles
	votePicker            components.VotePicker
	completionPicker      components.ListPicker
	confirmingCompletion  bool
	confirmingAbandonment bool
	pendingMergeStrategy  provider.PRMergeStrategy
	descriptionEditor     textarea.Model
	summaryEditor         textinput.Model
	editingSummary        bool
	savingSummary         bool
	editingDescription    bool
	savingDescription     bool
	vimNormalMode         bool
	vimCommand            string
}

// NewDetailModel creates a new PR detail model with default styles
func NewDetailModel(client provider.Provider, pr provider.PullRequest) *DetailModel {
	return NewDetailModelWithStyles(client, pr, styles.DefaultStyles())
}

// NewDetailModelWithStyles creates a new PR detail model with custom styles
func NewDetailModelWithStyles(client provider.Provider, pr provider.PullRequest, s *styles.Styles) *DetailModel {
	spinner := components.NewLoadingIndicator(s)
	spinner.SetMessage(fmt.Sprintf("Loading PR #%d...", prNumericID(pr)))
	descriptionEditor := textarea.New()
	descriptionEditor.Placeholder = "Pull request description..."
	descriptionEditor.ShowLineNumbers = false
	descriptionEditor.CharLimit = 0
	descriptionEditor.SetHeight(8)
	summaryEditor := textinput.New()
	summaryEditor.Prompt = ""
	summaryEditor.Placeholder = "Pull request summary..."
	summaryEditor.CharLimit = 0

	return &DetailModel{
		client:            client,
		pr:                pr,
		threads:           []provider.Thread{},
		commentCounts:     make(map[string]int),
		fileIndex:         0,
		spinner:           spinner,
		styles:            s,
		votePicker:        components.NewVotePickerForKind(s, pr.Identity.Kind),
		completionPicker:  components.NewListPicker(s),
		descriptionEditor: descriptionEditor,
		summaryEditor:     summaryEditor,
	}
}

// Init initializes the detail model
func (m *DetailModel) Init() tea.Cmd {
	m.loading = true
	m.threadsLoaded = false
	m.filesLoaded = false
	m.spinner.SetVisible(true)
	return tea.Batch(m.fetchThreads(), m.fetchChangedFiles(), m.spinner.Init())
}

// Update handles messages for the detail view
func (m *DetailModel) Update(msg tea.Msg) (*DetailModel, tea.Cmd) {
	if result, ok := msg.(prDescriptionUpdateFinishedMsg); ok {
		m.savingDescription = false
		if result.err != nil {
			m.statusMessage = fmt.Sprintf("Failed to update PR description: %v", result.err)
			return m, nil
		}
		m.pr.Description = result.description
		m.editingDescription = false
		m.descriptionEditor.Blur()
		m.statusMessage = "PR description updated"
		if m.ready {
			m.updateViewportContent()
		}
		return m, nil
	}
	if result, ok := msg.(prTitleUpdateFinishedMsg); ok {
		m.savingSummary = false
		if result.err != nil {
			m.statusMessage = fmt.Sprintf("Failed to update PR summary: %v", result.err)
			return m, nil
		}
		m.pr.Title = result.title
		m.editingSummary = false
		m.summaryEditor.Blur()
		m.statusMessage = "PR summary updated"
		return m, nil
	}
	if result, ok := msg.(copyPRURLResultMsg); ok {
		if result.err != nil {
			m.statusMessage = fmt.Sprintf("Failed to copy PR URL: %v", result.err)
		} else {
			m.statusMessage = "PR URL copied to clipboard"
		}
		return m, nil
	}
	if result, ok := msg.(copyPRCommitsResultMsg); ok {
		if result.err != nil {
			m.statusMessage = fmt.Sprintf("Failed to copy PR commit IDs: %v", result.err)
		} else {
			m.statusMessage = "PR commit IDs copied to clipboard"
		}
		return m, nil
	}

	if m.editingDescription {
		if wmsg, ok := msg.(tea.WindowSizeMsg); ok {
			m.width = wmsg.Width
			m.height = wmsg.Height
			m.descriptionEditor.SetWidth(max(10, wmsg.Width-4))
		}
		if key, ok := msg.(tea.KeyMsg); ok {
			if m.vimNormalMode {
				return m, m.handleVimNormalMode(key)
			}
			switch key.Type {
			case tea.KeyEsc:
				m.vimNormalMode = true
				m.vimCommand = ""
				m.descriptionEditor.Blur()
				return m, nil
			case tea.KeyCtrlJ:
				if m.savingDescription {
					return m, nil
				}
				return m, m.saveDescription()
			}
		}
		if m.savingDescription {
			return m, nil
		}
		var cmd tea.Cmd
		m.descriptionEditor, cmd = m.descriptionEditor.Update(msg)
		return m, cmd
	}
	if m.editingSummary {
		if wmsg, ok := msg.(tea.WindowSizeMsg); ok {
			m.width = wmsg.Width
			m.height = wmsg.Height
			m.summaryEditor.Width = max(10, wmsg.Width-4)
		}
		if key, ok := msg.(tea.KeyMsg); ok {
			if m.vimNormalMode {
				return m, m.handleVimNormalMode(key)
			}
			switch key.Type {
			case tea.KeyEsc:
				m.vimNormalMode = true
				m.vimCommand = ""
				m.summaryEditor.Blur()
				return m, nil
			case tea.KeyEnter, tea.KeyCtrlJ:
				if m.savingSummary {
					return m, nil
				}
				return m, m.saveSummary()
			}
		}
		if m.savingSummary {
			return m, nil
		}
		var cmd tea.Cmd
		m.summaryEditor, cmd = m.summaryEditor.Update(msg)
		return m, cmd
	}

	// Route input to vote picker when visible
	if m.votePicker.IsVisible() {
		var cmd tea.Cmd
		m.votePicker, cmd = m.votePicker.Update(msg)
		return m, cmd
	}
	if m.completionPicker.IsVisible() {
		var cmd tea.Cmd
		m.completionPicker, cmd = m.completionPicker.Update(msg)
		return m, cmd
	}
	if m.confirmingCompletion {
		key, ok := msg.(tea.KeyMsg)
		if !ok {
			return m, nil
		}
		switch key.String() {
		case "y", "enter":
			m.confirmingCompletion = false
			m.loading = true
			m.spinner.SetVisible(true)
			return m, tea.Batch(m.approveAndCompletePullRequest(m.pendingMergeStrategy), m.spinner.Tick())
		case "n", "esc":
			m.confirmingCompletion = false
			m.statusMessage = "PR completion cancelled"
		}
		return m, nil
	}
	if m.confirmingAbandonment {
		key, ok := msg.(tea.KeyMsg)
		if !ok {
			return m, nil
		}
		switch key.String() {
		case "y", "enter":
			m.confirmingAbandonment = false
			m.loading = true
			m.spinner.SetVisible(true)
			return m, tea.Batch(m.abandonPullRequest(), m.spinner.Tick())
		case "n", "esc":
			m.confirmingAbandonment = false
			m.statusMessage = "PR abandonment cancelled"
		}
		return m, nil
	}

	switch msg := msg.(type) {
	case components.VoteSelectedMsg:
		m.loading = true
		m.spinner.SetVisible(true)
		return m, tea.Batch(m.votePR(msg.Vote), m.spinner.Tick())

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.completionPicker.SetSize(msg.Width, msg.Height)

	case spinner.TickMsg:
		if m.loading {
			var spinnerCmd tea.Cmd
			m.spinner, spinnerCmd = m.spinner.Update(msg)
			return m, spinnerCmd
		}

	case tea.KeyMsg:
		switch msg.String() {
		case "up", "k":
			m.MoveUp()
		case "down", "j":
			m.MoveDown()
		case "pgup":
			m.PageUp()
		case "pgdown":
			m.PageDown()
		case "enter":
			if m.isGeneralCommentsSelected() {
				return m, func() tea.Msg {
					return openGeneralCommentsMsg{}
				}
			}
			fi := m.fileIndex - m.generalCommentsOffset()
			if fi >= 0 && fi < len(m.changedFiles) {
				return m, func() tea.Msg {
					return openFileDiffMsg{
						file: m.changedFiles[fi],
					}
				}
			}
		case "v":
			m.votePicker.SetSize(m.width, m.height)
			m.votePicker.Show()
			return m, nil
		case "e":
			if m.CanEditPR() {
				m.descriptionEditor.SetValue(m.pr.Description)
				m.descriptionEditor.SetWidth(max(10, m.width-4))
				m.editingDescription = true
				m.vimNormalMode = false
				m.vimCommand = ""
				m.statusMessage = ""
				return m, m.descriptionEditor.Focus()
			}
		case "s":
			if m.CanEditPR() {
				m.summaryEditor.SetValue(m.pr.Title)
				m.summaryEditor.Width = max(10, m.width-4)
				m.editingSummary = true
				m.vimNormalMode = false
				m.vimCommand = ""
				m.statusMessage = ""
				return m, m.summaryEditor.Focus()
			}
		case "r":
			m.loading = true
			m.threadsLoaded = false
			m.filesLoaded = false
			m.spinner.SetVisible(true)
			return m, tea.Batch(m.fetchThreads(), m.fetchChangedFiles(), m.spinner.Tick())
		case "o":
			return m, m.openInBrowser()
		case "c":
			return m, m.copyPRURLToClipboard()
		case "y":
			return m, m.copyPRCommitsToClipboard()
		case "a":
			if !m.CanAbandonPR() {
				m.statusMessage = "PR abandonment is available only for active Azure DevOps pull requests"
				return m, nil
			}
			m.confirmingAbandonment = true
			m.statusMessage = ""
			return m, nil
		case "m":
			if !m.CanCompletePR() {
				m.statusMessage = "PR completion is available only for active Azure DevOps pull requests"
				return m, nil
			}
			m.completionPicker.SetConfig("Approve and complete pull request", []components.ListPickerOption{
				{Name: "Fast-forward (rebase)", Icon: "↪"},
				{Name: "Squash commit", Icon: "◉"},
			}, "", false)
			m.completionPicker.SetSize(m.width, m.height)
			m.completionPicker.Show()
			return m, nil
		}

	case threadsMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.threads = diff.FilterSystemThreadsP(msg.threads)
		m.threadsLoaded = true
		m.finishLoading()

	case changedFilesMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.changedFiles = filterFileChanges(msg.changes)
		m.fileIndex = 0
		m.filesLoaded = true
		m.finishLoading()

	case voteResultMsg:
		if msg.err != nil {
			m.err = msg.err
			return m, nil
		}
		m.statusMessage = msg.message
		m.loading = true
		m.spinner.SetVisible(true)
		return m, tea.Batch(m.fetchThreads(), m.spinner.Tick())

	case components.ListPickerSelectedMsg:
		switch msg.Value {
		case "Fast-forward (rebase)":
			m.pendingMergeStrategy = provider.PRMergeFastForward
		case "Squash commit":
			m.pendingMergeStrategy = provider.PRMergeSquash
		default:
			m.statusMessage = fmt.Sprintf("Unsupported merge strategy %q", msg.Value)
			return m, nil
		}
		m.confirmingCompletion = true
		m.statusMessage = ""
		return m, nil

	case completePullRequestResultMsg:
		m.loading = false
		m.spinner.SetVisible(false)
		if msg.err != nil {
			if msg.approved {
				m.statusMessage = fmt.Sprintf("PR approved, but completion failed: %v", msg.err)
			} else {
				m.statusMessage = fmt.Sprintf("Failed to approve PR: %v", msg.err)
			}
			return m, nil
		}
		m.pr.Status = "completed"
		m.pr.StatusCategory = provider.StateCategoryClosedDone
		m.statusMessage = "PR approved and completed using " + mergeStrategyLabel(msg.strategy)
		if m.ready {
			m.updateViewportContent()
		}
		return m, nil

	case abandonPullRequestResultMsg:
		m.loading = false
		m.spinner.SetVisible(false)
		if msg.err != nil {
			m.statusMessage = fmt.Sprintf("Failed to abandon PR: %v", msg.err)
			return m, nil
		}
		m.pr.Status = "abandoned"
		m.pr.StatusCategory = provider.StateCategoryRemoved
		m.statusMessage = "PR abandoned"
		if m.ready {
			m.updateViewportContent()
		}
		return m, nil

	case openURLResultMsg:
		if msg.err != nil {
			m.statusMessage = fmt.Sprintf("Failed to open browser: %v", msg.err)
		} else {
			m.statusMessage = "Opened in browser"
		}
		return m, nil
	}

	return m, nil
}

// finishLoading clears the loading state when both threads and files have arrived
func (m *DetailModel) finishLoading() {
	if !m.threadsLoaded || !m.filesLoaded {
		return
	}
	m.loading = false
	m.spinner.SetVisible(false)
	m.commentCounts = diff.CountCommentsPerFileP(m.threads)
	if m.ready {
		m.updateViewportContent()
	}
}

// View renders the detail view
func (m *DetailModel) View() string {
	if m.editingSummary {
		if m.savingSummary {
			return fmt.Sprintf("Saving summary for PR #%d...", prNumericID(m.pr))
		}
		helpText := "Enter/Ctrl+Enter: save • Esc: normal mode"
		if m.vimNormalMode {
			helpText = "NORMAL • ggdG: clear all • i: edit • Esc: cancel"
		}
		help := m.styles.Muted.Render(helpText)
		title := m.styles.Header.Render(fmt.Sprintf("Edit summary — PR #%d", prNumericID(m.pr)))
		return lipgloss.JoinVertical(lipgloss.Left, title, "", m.summaryEditor.View(), help)
	}
	if m.editingDescription {
		if m.savingDescription {
			return fmt.Sprintf("Saving description for PR #%d...", prNumericID(m.pr))
		}
		helpText := "Enter: new line • Ctrl+Enter: save • Esc: normal mode"
		if m.vimNormalMode {
			helpText = "NORMAL • ggdG: clear all • i: edit • Esc: cancel"
		}
		help := m.styles.Muted.Render(helpText)
		title := m.styles.Header.Render(fmt.Sprintf("Edit description — PR #%d", prNumericID(m.pr)))
		return lipgloss.JoinVertical(lipgloss.Left, title, "", m.descriptionEditor.View(), help)
	}

	if m.votePicker.IsVisible() {
		return m.votePicker.View()
	}
	if m.completionPicker.IsVisible() {
		return m.completionPicker.View()
	}
	if m.confirmingCompletion {
		message := fmt.Sprintf("Approve and complete PR #%d using %s?", prNumericID(m.pr), mergeStrategyLabel(m.pendingMergeStrategy))
		help := m.styles.Muted.Render("y/enter: confirm • n/esc: cancel")
		return lipgloss.JoinVertical(lipgloss.Left, m.styles.Header.Render(message), "", help)
	}
	if m.confirmingAbandonment {
		help := m.styles.Muted.Render("y/enter: confirm • n/esc: cancel")
		message := fmt.Sprintf("Abandon PR #%d? This cannot be undone.", prNumericID(m.pr))
		return lipgloss.JoinVertical(lipgloss.Left, m.styles.Header.Render(message), "", help)
	}

	wrapContent := func(content string) string {
		contentStyle := lipgloss.NewStyle().
			Width(m.width)
		return contentStyle.Render(content)
	}

	if m.err != nil {
		return wrapContent(fmt.Sprintf("Error: %v\n\nPress r to retry, Esc to go back", m.err))
	}

	if m.loading {
		return wrapContent(m.spinner.View())
	}

	var sb strings.Builder

	// Header with PR title
	sb.WriteString(m.styles.Header.Render(fmt.Sprintf("PR #%d: %s", prNumericID(m.pr), m.pr.Title)))
	sb.WriteString("\n")

	// Branch info
	sb.WriteString(m.styles.Muted.Render(fmt.Sprintf("%s → %s", m.pr.SourceRefName, m.pr.TargetRefName)))
	sb.WriteString("\n")
	separatorWidth := min(m.width-2, 60)
	if separatorWidth < 1 {
		separatorWidth = 60
	}
	sb.WriteString(strings.Repeat("─", separatorWidth))
	sb.WriteString("\n")

	// Viewport with scrollable content
	if m.ready {
		sb.WriteString(m.viewport.View())
	}

	contentStyle := lipgloss.NewStyle().
		Width(m.width)

	return contentStyle.Render(sb.String())
}

// updateViewportContent builds the content and sets it in the viewport
func (m *DetailModel) updateViewportContent() {
	var sb strings.Builder

	// Description
	if m.pr.Description != "" {
		descStyle := m.styles.Value.Width(m.width)
		sb.WriteString(descStyle.Render(m.pr.Description))
		sb.WriteString("\n\n")
	}

	// "Go to PR" link
	if m.client != nil {
		prURL := m.client.PRURL(m.pr.Identity.Scope, m.pr.RepositoryID, prNumericID(m.pr))
		if prURL != "" {
			sb.WriteString(hyperlink(m.styles.Link.Render("Go to PR"), prURL))
			sb.WriteString("\n\n")
		}
	}

	// Creation timestamp
	if !m.pr.CreationDate.IsZero() {
		sb.WriteString(m.styles.Label.Render("Created: "))
		sb.WriteString(m.pr.CreationDate.Format("2006-01-02 15:04"))
		if m.pr.CreatedByName != "" {
			sb.WriteString(" by " + m.pr.CreatedByName)
		}
		sb.WriteString("\n\n")
	}

	if m.pr.StatusCategory == provider.StateCategoryClosedDone {
		if m.pr.SourceCommitID != "" || m.pr.MergeCommitID != "" {
			sb.WriteString(m.styles.Label.Render("Commits"))
			sb.WriteString("\n")
			if m.pr.SourceCommitID != "" {
				sb.WriteString(fmt.Sprintf("  Source: %s\n", m.pr.SourceCommitID))
			}
			if m.pr.MergeCommitID != "" {
				sb.WriteString(fmt.Sprintf("  Merge:  %s\n", m.pr.MergeCommitID))
			}
			sb.WriteString("\n")
		}
	}

	// Reviewers section
	if len(m.pr.Reviewers) > 0 {
		sb.WriteString(m.styles.Label.Render("Reviewers"))
		sb.WriteString("\n")
		for _, reviewer := range m.pr.Reviewers {
			icon := reviewerVoteIconWithStyles(reviewer.Kind, m.styles)
			voteDesc := reviewerVoteDescription(reviewer.Kind)
			sb.WriteString(fmt.Sprintf("  %s %s (%s)\n", icon, reviewer.DisplayName, m.styles.Muted.Render(voteDesc)))
		}
		sb.WriteString("\n")
	}

	// General comments entry (selectable, navigable like files)
	generalThreads := diff.FilterGeneralThreadsP(m.threads)
	if len(generalThreads) > 0 {
		generalLine := fmt.Sprintf("  💬 General comments (%d)", len(generalThreads))
		if m.fileIndex == 0 {
			sb.WriteString(m.styles.Selected.Render(generalLine))
		} else {
			sb.WriteString(m.styles.Info.Render(generalLine))
		}
		sb.WriteString("\n\n")
	}

	// Changed files section
	sb.WriteString(m.styles.Label.Render(fmt.Sprintf("Changed files (%d)", len(m.changedFiles))))
	sb.WriteString("\n")

	if len(m.changedFiles) > 0 {
		for i, change := range m.changedFiles {
			line := m.renderFileEntry(change, i+m.generalCommentsOffset() == m.fileIndex)
			sb.WriteString(line)
			sb.WriteString("\n")
		}
	} else {
		sb.WriteString(m.styles.Muted.Render("  No changed files"))
		sb.WriteString("\n")
	}

	m.viewport.SetContent(sb.String())
}

// renderFileEntry renders a single file in the changed files list
func (m *DetailModel) renderFileEntry(change provider.IterationChange, selected bool) string {
	icon, style := changeTypeDisplay(change.ChangeType, m.styles)

	path := change.Path
	if change.ChangeType == "rename" && change.OriginalPath != "" {
		path = fmt.Sprintf("%s -> %s", change.OriginalPath, change.Path)
	}

	line := fmt.Sprintf("  %s %s", icon, path)

	// Add comment count if there are comments for this file
	count := m.commentCounts[change.Path]
	if count > 0 {
		line += " " + m.styles.DiffCommentCount.Render(fmt.Sprintf("(%d)", count))
	}

	if selected {
		return m.styles.Selected.Render(line)
	}
	return style.Render(line)
}

// changeTypeDisplay returns an icon and style for a change type
func changeTypeDisplay(changeType string, s *styles.Styles) (string, lipgloss.Style) {
	switch changeType {
	case "add":
		return "+", s.Success
	case "edit":
		return "~", s.Info
	case "delete":
		return "-", s.Error
	case "rename":
		return "→", s.Warning
	default:
		return "?", s.Muted
	}
}

// SetSize sets the size of the detail view
func (m *DetailModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	m.descriptionEditor.SetWidth(max(10, width-4))
	m.summaryEditor.Width = max(10, width-4)

	// Account for header lines rendered in View(): title (1) + branch (1) + separator (1) = 3
	headerLines := 3
	viewportHeight := height - headerLines
	if viewportHeight < 1 {
		viewportHeight = 1
	}

	if !m.ready {
		m.viewport = viewport.New(width, viewportHeight)
		m.ready = true
	} else {
		m.viewport.Width = width
		m.viewport.Height = viewportHeight
	}

	m.updateViewportContent()
}

// ensureSelectedVisible scrolls the viewport to keep the selected item visible
func (m *DetailModel) ensureSelectedVisible() {
	if !m.ready || m.totalSelectableItems() == 0 {
		return
	}

	selectedLine := m.getSelectedItemLineOffset()
	if selectedLine < m.viewport.YOffset {
		m.viewport.SetYOffset(selectedLine)
	} else if selectedLine >= m.viewport.YOffset+m.viewport.Height {
		m.viewport.SetYOffset(selectedLine - m.viewport.Height + 1)
	}
}

// SetThreads sets the threads (useful for testing)
// Filters out system-generated threads
func (m *DetailModel) SetThreads(threads []provider.Thread) {
	m.threads = diff.FilterSystemThreadsP(threads)
	m.threadsLoaded = true
	m.commentCounts = diff.CountCommentsPerFileP(m.threads)
	if m.ready {
		m.updateViewportContent()
	}
}

// SetChangedFiles sets the changed files (useful for testing)
func (m *DetailModel) SetChangedFiles(files []provider.IterationChange) {
	m.changedFiles = filterFileChanges(files)
	m.fileIndex = 0
	m.filesLoaded = true
	if m.ready {
		m.updateViewportContent()
	}
}

// MoveUp moves file selection up or scrolls viewport if at top
func (m *DetailModel) MoveUp() {
	if !m.ready {
		return
	}
	if m.fileIndex > 0 {
		m.fileIndex--
		savedOffset := m.viewport.YOffset
		m.updateViewportContent()
		m.viewport.SetYOffset(savedOffset)
		m.ensureSelectedVisible()
	} else {
		m.viewport.LineUp(1)
	}
}

// MoveDown moves file selection down or scrolls viewport if at bottom
func (m *DetailModel) MoveDown() {
	if !m.ready {
		return
	}
	maxIndex := m.totalSelectableItems() - 1
	if maxIndex >= 0 && m.fileIndex < maxIndex {
		m.fileIndex++
		savedOffset := m.viewport.YOffset
		m.updateViewportContent()
		m.viewport.SetYOffset(savedOffset)
		m.ensureSelectedVisible()
	} else {
		m.viewport.LineDown(1)
	}
}

// PageUp scrolls the viewport up by one page
func (m *DetailModel) PageUp() {
	if !m.ready {
		return
	}
	m.viewport.HalfViewUp()
	m.updateSelectionFromViewport()
}

// PageDown scrolls the viewport down by one page
func (m *DetailModel) PageDown() {
	if !m.ready {
		return
	}
	m.viewport.HalfViewDown()
	m.updateSelectionFromViewport()
}

// updateSelectionFromViewport updates the selected item based on viewport position
func (m *DetailModel) updateSelectionFromViewport() {
	total := m.totalSelectableItems()
	if total == 0 {
		return
	}

	// Find which selectable item is closest to the viewport top
	targetLine := m.viewport.YOffset + 2 // small margin from top
	bestIdx := 0
	for i := 0; i < total; i++ {
		m.fileIndex = i
		itemLine := m.getSelectedItemLineOffset()
		if itemLine <= targetLine {
			bestIdx = i
		}
	}
	m.fileIndex = bestIdx

	savedOffset := m.viewport.YOffset
	m.updateViewportContent()
	m.viewport.SetYOffset(savedOffset)
}

// getSelectedItemLineOffset returns the visual line number for the currently selected item
func (m *DetailModel) getSelectedItemLineOffset() int {
	lineOffset := 0
	if m.pr.Description != "" {
		lineOffset += strings.Count(m.pr.Description, "\n") + 2
	}
	if m.client != nil && m.pr.RepositoryID != "" {
		lineOffset += 2
	}
	if !m.pr.CreationDate.IsZero() {
		lineOffset += 2
	}
	if len(m.pr.Reviewers) > 0 {
		lineOffset += 1 + len(m.pr.Reviewers) + 1
	}

	gcOffset := m.generalCommentsOffset()
	if gcOffset > 0 && m.fileIndex == 0 {
		// General comments entry is selected — it's at this line
		return lineOffset
	}

	// Skip past general comments entry + blank line
	if gcOffset > 0 {
		lineOffset += 2
	}

	// "Changed files (N)" header line
	lineOffset += 1

	// File index within the file list
	fi := m.fileIndex - gcOffset
	lineOffset += fi
	return lineOffset
}

// generalCommentsOffset returns 1 if there are general comments (taking index 0), 0 otherwise
func (m *DetailModel) generalCommentsOffset() int {
	generalThreads := diff.FilterGeneralThreadsP(m.threads)
	if len(generalThreads) > 0 {
		return 1
	}
	return 0
}

// isGeneralCommentsSelected returns true if the general comments entry is selected
func (m *DetailModel) isGeneralCommentsSelected() bool {
	return m.generalCommentsOffset() > 0 && m.fileIndex == 0
}

// totalSelectableItems returns the total navigable items (general comments entry + files)
func (m *DetailModel) totalSelectableItems() int {
	return m.generalCommentsOffset() + len(m.changedFiles)
}

// SelectedIndex returns the current file selection index
func (m *DetailModel) SelectedIndex() int {
	return m.fileIndex
}

// SelectedFile returns the currently selected changed file
func (m *DetailModel) SelectedFile() *provider.IterationChange {
	fi := m.fileIndex - m.generalCommentsOffset()
	if fi < 0 || fi >= len(m.changedFiles) {
		return nil
	}
	return &m.changedFiles[fi]
}

// GetContextItems returns context items for the detail view
func (m *DetailModel) GetContextItems() []components.ContextItem {
	if m.completionPicker.IsVisible() {
		return []components.ContextItem{
			{Key: "↑↓", Description: "choose strategy"},
			{Key: "enter", Description: "select"},
			{Key: "esc", Description: "cancel"},
		}
	}
	if m.confirmingCompletion {
		return []components.ContextItem{
			{Key: "y/enter", Description: "confirm"},
			{Key: "n/esc", Description: "cancel"},
		}
	}
	if m.confirmingAbandonment {
		return []components.ContextItem{
			{Key: "y/enter", Description: "confirm abandonment"},
			{Key: "n/esc", Description: "cancel"},
		}
	}
	items := []components.ContextItem{
		{Key: "enter", Description: "open"},
		{Key: "↑↓", Description: "navigate"},
		{Key: "v", Description: "vote"},
		{Key: "o", Description: "open in browser"},
		{Key: "c", Description: "copy PR URL"},
		{Key: "r", Description: "refresh"},
	}
	if len(m.prCommitIDs()) > 0 {
		items = append(items, components.ContextItem{Key: "y", Description: "copy commit IDs"})
	}
	if m.CanCompletePR() {
		items = append(items, components.ContextItem{Key: "m", Description: "approve & complete"})
	}
	if m.CanAbandonPR() {
		items = append(items, components.ContextItem{Key: "a", Description: "abandon PR"})
	}
	if m.CanEditPR() {
		items = append(items, components.ContextItem{Key: "s", Description: "edit summary"})
		items = append(items, components.ContextItem{Key: "e", Description: "edit description"})
	}
	if m.editingSummary {
		if m.vimNormalMode {
			return []components.ContextItem{
				{Key: "ggdG", Description: "clear all"},
				{Key: "i", Description: "edit"},
				{Key: "esc", Description: "cancel"},
			}
		}
		return []components.ContextItem{
			{Key: "enter", Description: "save"},
			{Key: "esc", Description: "normal mode"},
		}
	}
	if m.editingDescription {
		if m.vimNormalMode {
			return []components.ContextItem{
				{Key: "ggdG", Description: "clear all"},
				{Key: "i", Description: "edit"},
				{Key: "esc", Description: "cancel"},
			}
		}
		return []components.ContextItem{
			{Key: "ctrl+enter", Description: "save"},
			{Key: "esc", Description: "normal mode"},
		}
	}
	return items
}

type prDescriptionUpdateFinishedMsg struct {
	description string
	err         error
}

type prTitleUpdateFinishedMsg struct {
	title string
	err   error
}

func (m *DetailModel) CanEditPR() bool {
	editor, ok := m.client.(provider.PREditor)
	return ok && editor.CanEditPullRequest(m.pr.Identity.Scope)
}

func (m *DetailModel) CanCompletePR() bool {
	if m.pr.StatusCategory != provider.StateCategoryActive {
		return false
	}
	completer, ok := m.client.(provider.PRCompleter)
	return ok && completer.CanCompletePullRequest(m.pr.Identity.Scope)
}

func (m *DetailModel) CanAbandonPR() bool {
	if m.pr.StatusCategory != provider.StateCategoryActive {
		return false
	}
	abandoner, ok := m.client.(provider.PRAbandoner)
	return ok && abandoner.CanAbandonPullRequest(m.pr.Identity.Scope)
}

func (m *DetailModel) IsInputActive() bool {
	return m.editingDescription || m.editingSummary || m.completionPicker.IsVisible() ||
		m.confirmingCompletion || m.confirmingAbandonment
}

func (m *DetailModel) saveDescription() tea.Cmd {
	editor, ok := m.client.(provider.PREditor)
	if !ok || !editor.CanEditPullRequest(m.pr.Identity.Scope) {
		m.statusMessage = "Editing PR descriptions is not supported by this provider"
		return nil
	}
	description := m.descriptionEditor.Value()
	m.savingDescription = true
	m.statusMessage = ""
	return func() tea.Msg {
		err := editor.UpdatePullRequestDescription(
			m.pr.Identity.Scope,
			m.pr.RepositoryID,
			prNumericID(m.pr),
			description,
		)
		return prDescriptionUpdateFinishedMsg{description: description, err: err}
	}
}

func (m *DetailModel) saveSummary() tea.Cmd {
	editor, ok := m.client.(provider.PREditor)
	if !ok || !editor.CanEditPullRequest(m.pr.Identity.Scope) {
		m.statusMessage = "Editing PR summaries is not supported by this provider"
		return nil
	}
	title := strings.TrimSpace(m.summaryEditor.Value())
	if title == "" {
		m.statusMessage = "PR summary cannot be empty"
		return nil
	}
	m.savingSummary = true
	m.statusMessage = ""
	return func() tea.Msg {
		err := editor.UpdatePullRequestTitle(
			m.pr.Identity.Scope,
			m.pr.RepositoryID,
			prNumericID(m.pr),
			title,
		)
		return prTitleUpdateFinishedMsg{title: title, err: err}
	}
}

func (m *DetailModel) handleVimNormalMode(key tea.KeyMsg) tea.Cmd {
	switch key.Type {
	case tea.KeyEsc:
		m.editingSummary = false
		m.savingSummary = false
		m.editingDescription = false
		m.savingDescription = false
		m.vimNormalMode = false
		m.vimCommand = ""
		m.summaryEditor.Blur()
		m.descriptionEditor.Blur()
		m.statusMessage = ""
		return nil
	case tea.KeyEnter, tea.KeyCtrlJ:
		if m.editingSummary && !m.savingSummary {
			return m.saveSummary()
		}
		if m.editingDescription && !m.savingDescription {
			return m.saveDescription()
		}
	case tea.KeyRunes:
		for _, r := range key.Runes {
			if r == 'i' {
				m.vimNormalMode = false
				m.vimCommand = ""
				if m.editingSummary {
					return m.summaryEditor.Focus()
				}
				return m.descriptionEditor.Focus()
			}
			m.advanceVimCommand(r)
		}
	}
	return nil
}

func (m *DetailModel) advanceVimCommand(r rune) {
	switch m.vimCommand {
	case "":
		if r == 'g' {
			m.vimCommand = "g"
		}
	case "g":
		if r == 'g' {
			m.vimCommand = "gg"
		} else if r == 'G' {
			m.vimCommand = ""
		} else {
			m.vimCommand = ""
		}
	case "gg":
		if r == 'd' {
			m.vimCommand = "ggd"
		} else if r == 'g' {
			m.vimCommand = "g"
		} else {
			m.vimCommand = ""
		}
	case "ggd":
		if r == 'G' {
			m.clearEditedField()
			m.vimCommand = ""
		} else {
			m.vimCommand = ""
		}
	default:
		m.vimCommand = ""
	}
}

func (m *DetailModel) clearEditedField() {
	if m.editingSummary {
		m.summaryEditor.SetValue("")
		m.statusMessage = "PR summary cleared"
		return
	}
	if m.editingDescription {
		m.descriptionEditor.SetValue("")
		m.statusMessage = "PR description cleared"
	}
}

// openInBrowser returns a command that opens the PR overview URL in the
// user's default browser. If no URL can be built, it sets a status
// message and returns nil.
func (m *DetailModel) openInBrowser() tea.Cmd {
	if m.client == nil {
		m.statusMessage = "Cannot open: no Azure DevOps client"
		return nil
	}
	url := m.pullRequestURL()
	if url == "" {
		m.statusMessage = "Cannot open: missing organization, project, or repository"
		return nil
	}
	return func() tea.Msg {
		return openURLResultMsg{err: openURL(url)}
	}
}

func (m *DetailModel) copyPRURLToClipboard() tea.Cmd {
	url := m.pullRequestURL()
	if url == "" {
		m.statusMessage = "Cannot copy: PR URL unavailable"
		return nil
	}
	return func() tea.Msg {
		return copyPRURLResultMsg{err: writeClipboard(url)}
	}
}

func (m *DetailModel) copyPRCommitsToClipboard() tea.Cmd {
	commitIDs := m.prCommitIDs()
	if len(commitIDs) == 0 {
		m.statusMessage = "Cannot copy: completed PR commit IDs unavailable"
		return nil
	}
	return func() tea.Msg {
		return copyPRCommitsResultMsg{err: writeClipboard(strings.Join(commitIDs, "\n"))}
	}
}

func (m *DetailModel) prCommitIDs() []string {
	if m.pr.StatusCategory != provider.StateCategoryClosedDone {
		return nil
	}
	var commitIDs []string
	for _, id := range []string{m.pr.SourceCommitID, m.pr.MergeCommitID} {
		if id != "" && (len(commitIDs) == 0 || commitIDs[len(commitIDs)-1] != id) {
			commitIDs = append(commitIDs, id)
		}
	}
	return commitIDs
}

func (m *DetailModel) pullRequestURL() string {
	if m.client == nil {
		return ""
	}
	return m.client.PRURL(m.pr.Identity.Scope, m.pr.RepositoryID, prNumericID(m.pr))
}

// GetThreads returns the current threads (for passing to DiffModel)
func (m *DetailModel) GetThreads() []provider.Thread {
	return m.threads
}

// GetChangedFiles returns the changed files
func (m *DetailModel) GetChangedFiles() []provider.IterationChange {
	return m.changedFiles
}

// GetScrollPercent returns the scroll percentage based on viewport position
func (m *DetailModel) GetScrollPercent() float64 {
	if !m.ready {
		return 0
	}
	return m.viewport.ScrollPercent() * 100
}

// GetStatusMessage returns the status message
func (m *DetailModel) GetStatusMessage() string {
	return m.statusMessage
}

// GetPR returns the pull request
func (m *DetailModel) GetPR() provider.PullRequest {
	return m.pr
}

// GetPRID returns the numeric ID of the pull request.
func (m *DetailModel) GetPRID() int {
	return prNumericID(m.pr)
}

// Identity returns the cross-backend identity of the pull request, used to
// persist and restore the open detail without colliding across providers.
func (m *DetailModel) Identity() provider.Identity {
	return m.pr.Identity
}

// Helper functions

// hyperlink creates an OSC 8 terminal hyperlink
func hyperlink(text, url string) string {
	if url == "" {
		return text
	}
	return fmt.Sprintf("\x1b]8;;%s\x07%s\x1b]8;;\x07", url, text)
}

// truncateString truncates a string to maxRunes runes (not bytes)
func truncateString(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	runes := []rune(s)
	if len(runes) <= maxRunes {
		return s
	}
	return string(runes[:maxRunes])
}

// shortenFilePath shortens a file path to show only the last 2 segments
func shortenFilePath(path string) string {
	if path == "" {
		return ""
	}

	parts := strings.Split(path, "/")
	var nonEmpty []string
	for _, p := range parts {
		if p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}

	if len(nonEmpty) == 0 {
		return path
	}

	if len(nonEmpty) == 1 {
		return nonEmpty[0]
	}

	if len(nonEmpty) >= 2 {
		return "../" + nonEmpty[len(nonEmpty)-2] + "/" + nonEmpty[len(nonEmpty)-1]
	}

	return path
}

// reviewerVoteIconWithStyles returns an icon for the reviewer's vote using provided styles.
// Delegates to the display map so glyph+color stay in one place.
func reviewerVoteIconWithStyles(kind provider.VoteKind, s *styles.Styles) string {
	return display.VoteStyle(kind, s).Render(display.VoteGlyph(kind))
}

// reviewerVoteDescription returns a human-readable description of a vote kind.
// Delegates to the display map.
func reviewerVoteDescription(kind provider.VoteKind) string {
	return display.VoteLabel(kind)
}

// voteResultDescription returns a human-readable result message for a vote action
func voteResultDescription(vote int) string {
	switch vote {
	case azdevops.VoteApprove:
		return "PR approved"
	case azdevops.VoteApproveWithSuggestions:
		return "PR approved with suggestions"
	case azdevops.VoteWaitForAuthor:
		return "Waiting for author"
	case azdevops.VoteReject:
		return "PR rejected"
	case azdevops.VoteNoVote:
		return "Vote reset"
	default:
		return "Vote submitted"
	}
}

func mergeStrategyLabel(strategy provider.PRMergeStrategy) string {
	switch strategy {
	case provider.PRMergeFastForward:
		return "fast-forward (rebase)"
	case provider.PRMergeSquash:
		return "squash commit"
	default:
		return string(strategy)
	}
}

// threadStatusIconWithStyles returns an icon for the thread status using provided styles
func threadStatusIconWithStyles(status string, s *styles.Styles) string {
	switch status {
	case "active":
		return s.Info.Render("●")
	case "fixed":
		return s.Success.Render("✓")
	case "wontFix", "closed":
		return s.Muted.Render("○")
	case "pending":
		return s.Warning.Render("◐")
	default:
		return s.Muted.Render("○")
	}
}

// Messages

type threadsMsg struct {
	threads []provider.Thread
	err     error
}

type voteResultMsg struct {
	message string
	err     error
}

type completePullRequestResultMsg struct {
	strategy provider.PRMergeStrategy
	approved bool
	err      error
}

type abandonPullRequestResultMsg struct {
	err error
}

// openFileDiffMsg signals that the user wants to open the diff for a specific file
type openFileDiffMsg struct {
	file provider.IterationChange
}

// openGeneralCommentsMsg signals that the user wants to view general PR comments
type openGeneralCommentsMsg struct{}

// fetchThreads fetches PR threads via the provider
func (m *DetailModel) fetchThreads() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return threadsMsg{threads: nil, err: nil}
		}
		threads, err := m.client.GetPRThreads(m.pr.Identity.Scope, m.pr.RepositoryID, prNumericID(m.pr))
		return threadsMsg{threads: threads, err: err}
	}
}

// fetchChangedFiles loads iterations and changed files via the provider
func (m *DetailModel) fetchChangedFiles() tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return changedFilesMsg{changes: nil, err: nil}
		}

		iterations, err := m.client.GetPRIterations(m.pr.Identity.Scope, m.pr.RepositoryID, prNumericID(m.pr))
		if err != nil {
			return changedFilesMsg{err: err}
		}
		if len(iterations) == 0 {
			return changedFilesMsg{changes: nil, err: nil}
		}

		latestID := iterations[len(iterations)-1].ID
		changes, err := m.client.GetPRIterationChanges(m.pr.Identity.Scope, m.pr.RepositoryID, prNumericID(m.pr), latestID)
		if err != nil {
			return changedFilesMsg{err: err}
		}

		return changedFilesMsg{changes: changes}
	}
}

// votePR submits a vote on the PR
func (m *DetailModel) votePR(vote int) tea.Cmd {
	return func() tea.Msg {
		if m.client == nil {
			return voteResultMsg{message: "", err: nil}
		}
		err := m.client.VotePullRequest(m.pr.Identity.Scope, m.pr.RepositoryID, prNumericID(m.pr), vote)
		if err != nil {
			return voteResultMsg{message: "", err: err}
		}

		return voteResultMsg{message: voteResultDescription(vote), err: nil}
	}
}

func (m *DetailModel) approveAndCompletePullRequest(strategy provider.PRMergeStrategy) tea.Cmd {
	client := m.client
	scope, repositoryID, pullRequestID := m.pr.Identity.Scope, m.pr.RepositoryID, prNumericID(m.pr)
	return func() tea.Msg {
		if client == nil {
			return completePullRequestResultMsg{strategy: strategy, err: fmt.Errorf("provider is unavailable")}
		}
		completer, ok := client.(provider.PRCompleter)
		if !ok || !completer.CanCompletePullRequest(scope) {
			return completePullRequestResultMsg{strategy: strategy, err: fmt.Errorf("pull request completion is not supported")}
		}
		if err := client.VotePullRequest(scope, repositoryID, pullRequestID, azdevops.VoteApprove); err != nil {
			return completePullRequestResultMsg{strategy: strategy, err: err}
		}
		if err := completer.CompletePullRequest(scope, repositoryID, pullRequestID, strategy); err != nil {
			return completePullRequestResultMsg{strategy: strategy, approved: true, err: err}
		}
		return completePullRequestResultMsg{strategy: strategy, approved: true}
	}
}

func (m *DetailModel) abandonPullRequest() tea.Cmd {
	client := m.client
	scope, repositoryID, pullRequestID := m.pr.Identity.Scope, m.pr.RepositoryID, prNumericID(m.pr)
	return func() tea.Msg {
		if client == nil {
			return abandonPullRequestResultMsg{err: fmt.Errorf("provider is unavailable")}
		}
		abandoner, ok := client.(provider.PRAbandoner)
		if !ok || !abandoner.CanAbandonPullRequest(scope) {
			return abandonPullRequestResultMsg{err: fmt.Errorf("pull request abandonment is not supported")}
		}
		return abandonPullRequestResultMsg{err: abandoner.AbandonPullRequest(scope, repositoryID, pullRequestID)}
	}
}
