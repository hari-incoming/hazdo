package pipelines

import (
	"fmt"
	"strings"

	"github.com/hari-incoming/hazdo/internal/provider"
	"github.com/hari-incoming/hazdo/internal/ui/components"
	"github.com/hari-incoming/hazdo/internal/ui/styles"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type pipelineApprovalsLoadedMsg struct {
	generation uint64
	requestID  uint64
	approvals  []provider.PipelineApproval
	err        error
}

type pipelineApprovalUpdatedMsg struct {
	generation uint64
	approvalID string
	err        error
}

type approvalListModel struct {
	source     provider.PipelineApprovalSource
	scope      string
	width      int
	styles     *styles.Styles
	generation uint64
	approvals  []provider.PipelineApproval
	selected   int
	requestID  uint64
	loading    bool
	updating   string
	err        error
	status     string
}

func newApprovalListModel(source provider.PipelineApprovalSource, scope string, width int, generation uint64, s *styles.Styles) *approvalListModel {
	return &approvalListModel{
		source:     source,
		scope:      scope,
		width:      width,
		generation: generation,
		styles:     s,
	}
}

func (m *approvalListModel) Init() tea.Cmd {
	return m.load()
}

func (m *approvalListModel) SetSize(width int) {
	m.width = width
}

func (m *approvalListModel) Update(msg tea.Msg) (*approvalListModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
	case pipelineApprovalsLoadedMsg:
		if msg.generation != m.generation || msg.requestID != m.requestID {
			return m, nil
		}
		m.loading = false
		m.err = msg.err
		if msg.err == nil {
			m.approvals = msg.approvals
			if m.selected >= len(m.approvals) {
				m.selected = max(0, len(m.approvals)-1)
			}
			m.status = ""
		}
	case pipelineApprovalUpdatedMsg:
		if msg.generation != m.generation || msg.approvalID != m.updating {
			return m, nil
		}
		m.updating = ""
		if msg.err != nil {
			m.status = fmt.Sprintf("Could not approve request: %v", msg.err)
			return m, nil
		}
		for i, approval := range m.approvals {
			if approval.ID == msg.approvalID {
				m.approvals = append(m.approvals[:i], m.approvals[i+1:]...)
				break
			}
		}
		if m.selected >= len(m.approvals) {
			m.selected = max(0, len(m.approvals)-1)
		}
		m.status = "Approved resource request " + msg.approvalID
	case tea.KeyMsg:
		if m.loading || m.updating != "" {
			return m, nil
		}
		switch msg.String() {
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected+1 < len(m.approvals) {
				m.selected++
			}
		case "r":
			return m, m.load()
		case "a":
			return m, m.approveSelected()
		}
	}
	return m, nil
}

func (m *approvalListModel) load() tea.Cmd {
	m.requestID++
	requestID := m.requestID
	generation := m.generation
	m.loading = true
	m.err = nil
	m.status = ""
	return func() tea.Msg {
		approvals, err := m.source.ListPendingPipelineApprovals(m.scope)
		return pipelineApprovalsLoadedMsg{generation: generation, requestID: requestID, approvals: approvals, err: err}
	}
}

func (m *approvalListModel) approveSelected() tea.Cmd {
	if len(m.approvals) == 0 || m.selected >= len(m.approvals) {
		m.status = "No pending approval selected"
		return nil
	}
	approvalID := m.approvals[m.selected].ID
	m.updating = approvalID
	m.status = "Approving resource request..."
	source, scope, generation := m.source, m.scope, m.generation
	return func() tea.Msg {
		return pipelineApprovalUpdatedMsg{
			generation: generation,
			approvalID: approvalID,
			err:        source.ApprovePipelineResourceApproval(scope, approvalID),
		}
	}
}

func (m *approvalListModel) View() string {
	var sb strings.Builder
	title := fmt.Sprintf("Pending resource approvals — %s", m.scope)
	sb.WriteString(m.styles.Header.Render(title))
	sb.WriteString("\n")
	sb.WriteString(strings.Repeat("─", max(0, min(m.width-2, 60))))
	sb.WriteString("\n\n")
	sb.WriteString("Project-wide requests; check instructions before approving.\n\n")

	switch {
	case m.loading:
		sb.WriteString("Loading pending approvals...")
	case m.err != nil:
		sb.WriteString(fmt.Sprintf("Could not load approvals: %v\nPress r to retry", m.err))
	case len(m.approvals) == 0:
		sb.WriteString("No pending resource approvals.")
	default:
		for i, approval := range m.approvals {
			prefix := "  "
			if i == m.selected {
				prefix = "› "
			}
			created := ""
			if !approval.CreatedOn.IsZero() {
				created = approval.CreatedOn.Local().Format("2006-01-02 15:04")
			}
			line := fmt.Sprintf("%s%s  %s", prefix, approval.ID, created)
			if i == m.selected {
				line = m.styles.Selected.Render(line)
			}
			sb.WriteString(line)
			if approval.Instructions != "" {
				sb.WriteString("\n")
				sb.WriteString(lipgloss.NewStyle().PaddingLeft(4).Render(approval.Instructions))
			}
			if i < len(m.approvals)-1 {
				sb.WriteString("\n\n")
			}
		}
	}
	return lipgloss.NewStyle().Width(m.width).Render(sb.String())
}

func (m *approvalListModel) GetContextItems() []components.ContextItem {
	return []components.ContextItem{
		{Key: "j/k", Description: "select approval"},
		{Key: "a", Description: "approve selected request"},
		{Key: "r", Description: "refresh approvals"},
		{Key: "esc", Description: "back to pipeline"},
	}
}
