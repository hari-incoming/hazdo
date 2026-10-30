package workitems

import (
	"fmt"
	"sort"
	"strings"

	"github.com/hari-incoming/hazdo/internal/provider"
	"github.com/hari-incoming/hazdo/internal/ui/styles"
	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type workItemCreationStep int

const (
	workItemCreationProject workItemCreationStep = iota
	workItemCreationType
	workItemCreationTitle
	workItemCreationAssignee
	workItemCreationDescription
)

type workItemCreateSubmittedMsg struct {
	scope        string
	workItemType string
	title        string
	description  string
	assignedTo   string
}

type workItemCreatedMsg struct {
	item *provider.WorkItem
	err  error
}

type workItemCreateDismissedMsg struct{}

type workItemCreationForm struct {
	styles      *styles.Styles
	scopes      []string
	scopeIndex  int
	step        workItemCreationStep
	typeInput   textinput.Model
	titleInput  textinput.Model
	assignee    textinput.Model
	description textarea.Model
	visible     bool
	busy        bool
	finished    bool
	status      string
	width       int
	height      int
}

func newWorkItemCreationForm(scopes []string, s *styles.Styles) *workItemCreationForm {
	scopes = append([]string(nil), scopes...)
	sort.Strings(scopes)

	typeInput := textinput.New()
	typeInput.Prompt = ""
	typeInput.Placeholder = "Task"
	typeInput.SetValue("Task")
	typeInput.CharLimit = 100
	typeInput.Width = 56

	titleInput := textinput.New()
	titleInput.Prompt = ""
	titleInput.Placeholder = "Work item title"
	titleInput.CharLimit = 500
	titleInput.Width = 56

	assignee := textinput.New()
	assignee.Prompt = ""
	assignee.Placeholder = "email or Azure DevOps identity"
	assignee.CharLimit = 256
	assignee.Width = 56

	description := textarea.New()
	description.Placeholder = "Optional description"
	description.ShowLineNumbers = false
	description.SetHeight(6)
	description.SetWidth(56)

	f := &workItemCreationForm{
		styles:      s,
		scopes:      scopes,
		typeInput:   typeInput,
		titleInput:  titleInput,
		assignee:    assignee,
		description: description,
		visible:     true,
	}
	if len(scopes) > 1 {
		f.step = workItemCreationProject
	} else {
		f.step = workItemCreationType
	}
	f.focusCurrent()
	return f
}

func (f *workItemCreationForm) focusCurrent() tea.Cmd {
	switch f.step {
	case workItemCreationType:
		f.titleInput.Blur()
		f.assignee.Blur()
		f.description.Blur()
		return f.typeInput.Focus()
	case workItemCreationTitle:
		f.typeInput.Blur()
		f.assignee.Blur()
		f.description.Blur()
		return f.titleInput.Focus()
	case workItemCreationAssignee:
		f.typeInput.Blur()
		f.titleInput.Blur()
		f.description.Blur()
		return f.assignee.Focus()
	case workItemCreationDescription:
		f.typeInput.Blur()
		f.titleInput.Blur()
		f.assignee.Blur()
		return f.description.Focus()
	default:
		f.typeInput.Blur()
		f.titleInput.Blur()
		f.assignee.Blur()
		f.description.Blur()
		return nil
	}
}

func (f *workItemCreationForm) SetSize(width, height int) {
	f.width, f.height = width, height
	inputWidth := width - 16
	if inputWidth > 56 {
		inputWidth = 56
	}
	if inputWidth < 20 {
		inputWidth = 20
	}
	f.typeInput.Width = inputWidth
	f.titleInput.Width = inputWidth
	f.assignee.Width = inputWidth
	f.description.SetWidth(inputWidth)
}

func (f *workItemCreationForm) IsVisible() bool {
	return f != nil && f.visible
}

func (f *workItemCreationForm) Update(msg tea.Msg) (*workItemCreationForm, tea.Cmd) {
	if !f.IsVisible() {
		return f, nil
	}
	if f.busy {
		return f, nil
	}
	if f.finished {
		if _, ok := msg.(tea.KeyMsg); ok {
			f.visible = false
			return f, func() tea.Msg { return workItemCreateDismissedMsg{} }
		}
		return f, nil
	}

	if wmsg, ok := msg.(tea.WindowSizeMsg); ok {
		f.SetSize(wmsg.Width, wmsg.Height)
		return f, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.Type {
		case tea.KeyEsc:
			f.visible = false
			return f, func() tea.Msg { return workItemCreateDismissedMsg{} }
		case tea.KeyUp:
			if f.step == workItemCreationProject {
				if f.scopeIndex > 0 {
					f.scopeIndex--
				}
				return f, nil
			}
		case tea.KeyDown:
			if f.step == workItemCreationProject {
				if f.scopeIndex < len(f.scopes)-1 {
					f.scopeIndex++
				}
				return f, nil
			}
		case tea.KeyEnter:
			if f.step == workItemCreationDescription {
				var cmd tea.Cmd
				f.description, cmd = f.description.Update(msg)
				return f, cmd
			}
			f.status = ""
			switch f.step {
			case workItemCreationProject:
				if len(f.scopes) > 0 {
					f.step = workItemCreationType
					return f, f.focusCurrent()
				}
			case workItemCreationType:
				if strings.TrimSpace(f.typeInput.Value()) == "" {
					f.status = "Enter a work item type."
					return f, nil
				}
				f.step = workItemCreationTitle
				return f, f.focusCurrent()
			case workItemCreationTitle:
				if strings.TrimSpace(f.titleInput.Value()) == "" {
					f.status = "Enter a title."
					return f, nil
				}
				f.step = workItemCreationAssignee
				return f, f.focusCurrent()
			case workItemCreationAssignee:
				f.step = workItemCreationDescription
				return f, f.focusCurrent()
			}
			return f, nil
		case tea.KeyCtrlS:
			if f.step != workItemCreationDescription {
				f.status = "Complete the type and title fields first."
				return f, nil
			}
			if strings.TrimSpace(f.typeInput.Value()) == "" || strings.TrimSpace(f.titleInput.Value()) == "" {
				f.status = "Work item type and title are required."
				return f, nil
			}
			if len(f.scopes) == 0 {
				f.status = "No Azure DevOps project is available."
				return f, nil
			}
			f.busy = true
			f.status = "Creating work item..."
			scope := f.scopes[f.scopeIndex]
			return f, func() tea.Msg {
				return workItemCreateSubmittedMsg{
					scope:        scope,
					workItemType: strings.TrimSpace(f.typeInput.Value()),
					title:        strings.TrimSpace(f.titleInput.Value()),
					description:  strings.TrimSpace(f.description.Value()),
					assignedTo:   strings.TrimSpace(f.assignee.Value()),
				}
			}
		}
	}

	var cmd tea.Cmd
	switch f.step {
	case workItemCreationType:
		f.typeInput, cmd = f.typeInput.Update(msg)
	case workItemCreationTitle:
		f.titleInput, cmd = f.titleInput.Update(msg)
	case workItemCreationAssignee:
		f.assignee, cmd = f.assignee.Update(msg)
	case workItemCreationDescription:
		f.description, cmd = f.description.Update(msg)
	}
	return f, cmd
}

func (f *workItemCreationForm) Finish(item *provider.WorkItem, err error) {
	f.busy = false
	if err != nil {
		f.status = "Could not create work item: " + err.Error()
		return
	}
	f.finished = true
	if item != nil && item.Identity.ID != "" {
		f.status = fmt.Sprintf("Created work item #%s. Press any key to close.", item.Identity.ID)
	} else {
		f.status = "Work item created. Press any key to close."
	}
}

func (f *workItemCreationForm) View() string {
	if !f.IsVisible() {
		return ""
	}

	var content string
	switch f.step {
	case workItemCreationProject:
		lines := []string{"Choose a project:"}
		for i, scope := range f.scopes {
			marker := "  "
			if i == f.scopeIndex {
				marker = "> "
			}
			lines = append(lines, marker+scope)
		}
		content = strings.Join(lines, "\n")
	case workItemCreationType:
		content = "Work item type (for example: Task, Bug, User Story)\n" + f.typeInput.View()
	case workItemCreationTitle:
		content = "Title\n" + f.titleInput.View()
	case workItemCreationAssignee:
		content = "Assignee (optional)\n" + f.assignee.View()
	case workItemCreationDescription:
		content = "Description (optional)\n" + f.description.View()
	}

	instructions := "enter: next • esc: cancel"
	if f.step == workItemCreationDescription {
		instructions = "Ctrl+S: create • enter: new line • esc: cancel"
	}
	if f.busy {
		instructions = "Creating..."
	}
	if f.finished {
		instructions = "Press any key to close"
	}
	if f.status != "" {
		content += "\n\n" + f.status
	}
	content += "\n\n" + instructions

	width := 72
	if f.width > 0 && f.width-4 < width {
		width = f.width - 4
	}
	if width < 30 {
		width = 30
	}
	modal := lipgloss.NewStyle().
		Border(lipgloss.RoundedBorder()).
		BorderForeground(f.styles.Theme.GetBorder()).
		Padding(1, 2).
		Width(width).
		Render(content)
	if f.width > 0 && f.height > 0 {
		modal = lipgloss.Place(f.width, f.height, lipgloss.Center, lipgloss.Center, modal)
	}
	return modal
}
