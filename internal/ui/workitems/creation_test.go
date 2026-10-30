package workitems

import (
	"testing"

	"github.com/hari-incoming/hazdo/internal/provider"
	"github.com/hari-incoming/hazdo/internal/ui/styles"
	tea "github.com/charmbracelet/bubbletea"
)

type workItemCreationProvider struct {
	provider.Provider
	createdScope       string
	createdType        string
	createdTitle       string
	createdDescription string
	createdAssignee    string
}

func (p *workItemCreationProvider) WorkItemCreationScopes() []string {
	return []string{"project-a", "project-b"}
}

func (p *workItemCreationProvider) IsMultiProject() bool {
	return false
}

func (p *workItemCreationProvider) CreateWorkItem(scope, itemType, title, description, assignedTo string) (*provider.WorkItem, error) {
	p.createdScope = scope
	p.createdType = itemType
	p.createdTitle = title
	p.createdDescription = description
	p.createdAssignee = assignedTo
	return &provider.WorkItem{Identity: provider.Identity{ID: "321"}}, nil
}

func TestModel_CreateWorkItemFromList(t *testing.T) {
	client := &workItemCreationProvider{}
	m := NewModelWithStyles(client, styles.DefaultStyles())
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("n")})
	if !m.IsCreateFormVisible() {
		t.Fatal("pressing n should open the creation form")
	}
	var cmd tea.Cmd

	// Select the second project, accept the default Task type, then enter a title and assignee.
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, r := range "New item" {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, r := range "user@example.com" {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, r := range "Description line one" {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	for _, r := range "line two" {
		m, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	m, cmd = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	if cmd == nil {
		t.Fatal("Ctrl+S should start work-item creation")
	}
	request, ok := cmd().(workItemCreateSubmittedMsg)
	if !ok {
		t.Fatal("creation form should submit a work-item request")
	}
	if request.scope != "project-b" ||
		request.workItemType != "Task" ||
		request.title != "New item" ||
		request.assignedTo != "user@example.com" ||
		request.description != "Description line one\nline two" {
		t.Errorf("submitted fields = (%q, %q, %q, %q)", request.scope, request.workItemType, request.title, request.description)
	}
	m, cmd = m.Update(request)
	result := cmd()
	submitted, ok := result.(workItemCreatedMsg)
	if !ok {
		t.Fatalf("creation command message type = %T, want workItemCreatedMsg", result)
	}
	if submitted.err != nil {
		t.Fatalf("creation command error = %v", submitted.err)
	}
	if client.createdScope != "project-b" ||
		client.createdType != "Task" ||
		client.createdTitle != "New item" ||
		client.createdAssignee != "user@example.com" ||
		client.createdDescription != "Description line one\nline two" {
		t.Errorf("created fields = (%q, %q, %q, %q)", client.createdScope, client.createdType, client.createdTitle, client.createdDescription)
	}
}
