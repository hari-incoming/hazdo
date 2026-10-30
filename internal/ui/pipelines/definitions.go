package pipelines

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/hari-incoming/hazdo/internal/azdevops"
	"github.com/hari-incoming/hazdo/internal/provider"
	"github.com/hari-incoming/hazdo/internal/ui/components"
	"github.com/hari-incoming/hazdo/internal/ui/components/listview"
	"github.com/hari-incoming/hazdo/internal/ui/components/table"
	"github.com/hari-incoming/hazdo/internal/ui/styles"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// DefinitionsModel browses all available Azure pipeline definitions.
type DefinitionsModel struct {
	list           listview.Model[azdevops.PipelineDefinition]
	client         *azdevops.MultiClient
	styles         *styles.Styles
	creation       *pipelineCreationModel
	history        *pipelineRunHistory
	statusMessage  string
	width          int
	height         int
	sortField      pipelineDefinitionSortField
	sortDescending bool
	sortActive     bool
}

const pipelineHistoryPageSize = 100

type pipelineDefinitionSortField int

const (
	sortPipelineID pipelineDefinitionSortField = iota
	sortPipelineName
)

type pipelineRunHistory struct {
	model             Model
	definition        azdevops.PipelineDefinition
	continuationToken string
	requestID         uint64
	loadingOlder      bool
	statusMessage     string
}

type pipelineRunHistoryLoadedMsg struct {
	projectName       string
	definitionID      int
	requestID         uint64
	runs              []provider.PipelineRun
	continuationToken string
	appendPage        bool
	err               error
}

type pipelineDefinitionUpdatedMsg struct {
	projectName  string
	operation    string
	variableName string
	definition   *azdevops.PipelineDefinition
	err          error
}

const (
	pipelineDefinitionRenameAction      = "rename"
	pipelineDefinitionMoveAction        = "move"
	pipelineDefinitionAddVariableAction = "add variable"
)

// NewDefinitionsModel creates the available-pipeline browser.
func NewDefinitionsModel(client *azdevops.MultiClient, s *styles.Styles) DefinitionsModel {
	multiProject := client != nil && client.IsMultiProject()
	columns := []listview.ColumnSpec{
		{Title: "ID", WidthPct: 8, MinWidth: 5},
		{Title: "Pipeline", WidthPct: 28, MinWidth: 14},
		{Title: "Path", WidthPct: 40, MinWidth: 12},
	}
	if multiProject {
		columns = append([]listview.ColumnSpec{{Title: "Project", WidthPct: 20, MinWidth: 10}}, columns...)
	}
	listview.NormalizeWidths(columns)

	cfg := listview.Config[azdevops.PipelineDefinition]{
		Columns:        columns,
		LoadingMessage: "Loading available pipelines...",
		EntityName:     "pipeline definitions",
		MinWidth:       50,
		ToRows: func(definitions []azdevops.PipelineDefinition, _ *styles.Styles) []table.Row {
			rows := make([]table.Row, len(definitions))
			for i, definition := range definitions {
				row := table.Row{
					strconv.Itoa(definition.ID),
					definition.Name,
					definition.Path,
				}
				if multiProject {
					row = append(table.Row{definition.ProjectDisplayName}, row...)
				}
				rows[i] = row
			}
			return rows
		},
		Fetch: func() tea.Cmd {
			return fetchPipelineDefinitions(client)
		},
		EnterDetail: func(definition azdevops.PipelineDefinition, st *styles.Styles, width, height int) (listview.DetailView, tea.Cmd) {
			var projectClient *azdevops.Client
			if client != nil {
				projectClient = client.ClientFor(definition.ProjectName)
			}
			detail := newDefinitionDetailModel(projectClient, definition, st)
			detail.SetSize(width, height)
			return &definitionDetailAdapter{model: detail}, detail.Init()
		},
		HasContextBar: func(mode listview.ViewMode) bool {
			return mode == listview.ViewDetail
		},
		OpenOnDoubleClick: true,
		FilterFunc: func(definition azdevops.PipelineDefinition, query string) bool {
			query = strings.ToLower(query)
			return strings.Contains(strings.ToLower(definition.Name), query) ||
				strings.Contains(strings.ToLower(definition.Path), query) ||
				strings.Contains(strings.ToLower(definition.ProjectDisplayName), query)
		},
	}

	list := listview.New(cfg, s).SetLoading(true)
	return DefinitionsModel{
		list:   list,
		client: client,
		styles: s,
	}
}

// Init loads all pipeline definitions from the configured projects.
func (m DefinitionsModel) Init() tea.Cmd {
	return m.list.Init()
}

// Update handles list, definition detail, and queue results.
func (m DefinitionsModel) Update(msg tea.Msg) (DefinitionsModel, tea.Cmd) {
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = size.Width
		m.height = size.Height
		if m.history != nil {
			size.Height = max(1, size.Height-1)
			var cmd tea.Cmd
			m.history.model, cmd = m.history.model.Update(size)
			return m, cmd
		}
	}
	if updated, ok := msg.(pipelineDefinitionUpdatedMsg); ok && updated.err == nil && updated.definition != nil {
		items := m.list.Items()
		for i := range items {
			if items[i].ID == updated.definition.ID && items[i].ProjectName == updated.projectName {
				items[i].Name = updated.definition.Name
				items[i].Path = updated.definition.Path
				break
			}
		}
		m.setDefinitionItems(items)
	}
	if result, ok := msg.(pipelineRunHistoryLoadedMsg); ok {
		if m.history == nil ||
			m.history.definition.ProjectName != result.projectName ||
			m.history.definition.ID != result.definitionID ||
			m.history.requestID != result.requestID {
			return m, nil
		}
		m.history.loadingOlder = false
		if result.appendPage {
			if result.err != nil {
				m.history.statusMessage = "Could not load older runs: " + result.err.Error()
				return m, nil
			}
			m.history.model.allRuns = append(m.history.model.allRuns, result.runs...)
			m.history.model.list = m.history.model.list.SetItems(
				m.history.model.applyStatusFilter(m.history.model.allRuns),
			)
			m.history.continuationToken = result.continuationToken
			m.history.statusMessage = fmt.Sprintf("Loaded %d older runs", len(result.runs))
			return m, nil
		}
		m.history.continuationToken = result.continuationToken
		m.history.statusMessage = ""
		var cmd tea.Cmd
		m.history.model, cmd = m.history.model.Update(pipelineRunsMsg{runs: result.runs, err: result.err})
		return m, cmd
	}
	if m.history != nil {
		if key, ok := msg.(tea.KeyMsg); ok {
			switch {
			case key.String() == "esc" &&
				m.history.model.GetViewMode() == ViewList &&
				!m.history.model.IsSearching():
				m.history = nil
				return m, nil
			case key.String() == "r" &&
				m.history.model.GetViewMode() == ViewList &&
				!m.history.model.IsSearching():
				m.history.continuationToken = ""
				m.history.loadingOlder = false
				m.history.statusMessage = "Loading run history..."
				m.history.model.allRuns = nil
				m.history.model.list = m.history.model.list.SetItems(nil).SetLoading(true)
				return m, m.history.fetch(m.client, "", false)
			case key.String() == "o" &&
				m.history.model.GetViewMode() == ViewList &&
				!m.history.model.IsSearching() &&
				m.history.continuationToken != "" &&
				!m.history.loadingOlder:
				m.history.loadingOlder = true
				m.history.statusMessage = "Loading older runs..."
				return m, m.history.fetch(m.client, m.history.continuationToken, true)
			}
		}
		var cmd tea.Cmd
		m.history.model, cmd = m.history.model.Update(msg)
		return m, cmd
	}
	if created, ok := msg.(pipelineCreatedMsg); ok {
		if created.err != nil {
			if m.creation != nil {
				m.creation.creating = false
				if len(created.secrets) > 0 {
					m.creation.status = "Pipeline creation failed; error details were hidden because secret variables were included"
				} else {
					m.creation.status = "Pipeline creation failed: " + created.err.Error()
				}
			}
			return m, nil
		}
		if created.definition == nil {
			if m.creation != nil {
				m.creation.creating = false
				m.creation.status = "Pipeline creation returned no definition"
			}
			return m, nil
		}
		m.creation = nil
		if created.copied {
			m.statusMessage = fmt.Sprintf("Copied pipeline %q", created.definition.Name)
		} else {
			m.statusMessage = fmt.Sprintf("Created pipeline %q", created.definition.Name)
		}
		return m, fetchPipelineDefinitions(m.client)
	}
	if m.creation != nil {
		var cmd tea.Cmd
		m.creation, cmd = m.creation.Update(msg)
		if m.creation.cancelled {
			m.creation = nil
			return m, nil
		}
		return m, cmd
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "n" &&
		m.list.GetViewMode() == listview.ViewList && !m.list.IsSearching() {
		m.creation = newPipelineCreationModel(m.client, m.styles)
		m.creation.width = m.width
		m.creation.height = m.height
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok &&
		(key.String() == "s" || key.String() == "S") &&
		m.list.GetViewMode() == listview.ViewList && !m.list.IsSearching() {
		if key.String() == "s" {
			if !m.sortActive {
				m.sortField = sortPipelineID
				m.sortActive = true
			} else {
				m.sortField = 1 - m.sortField
			}
			m.sortDescending = false
		} else if !m.sortActive {
			m.sortField = sortPipelineID
			m.sortActive = true
			m.sortDescending = true
		} else {
			m.sortDescending = !m.sortDescending
		}
		m.setDefinitionItems(m.list.Items())
		direction := "ascending"
		if m.sortDescending {
			direction = "descending"
		}
		m.statusMessage = fmt.Sprintf("Sorted by pipeline %s (%s)", m.sortFieldName(), direction)
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "c" &&
		m.list.GetViewMode() == listview.ViewList && !m.list.IsSearching() {
		items := m.list.Items()
		index := m.list.SelectedIndex()
		if index >= 0 && index < len(items) {
			creation, cmd := newPipelineCopyModel(m.client, m.styles, items[index])
			m.creation = creation
			m.creation.width = m.width
			m.creation.height = m.height
			return m, cmd
		}
	}
	if key, ok := msg.(tea.KeyMsg); ok &&
		(key.String() == "v" || key.String() == "H" || key.String() == "shift+h") {
		var definition *azdevops.PipelineDefinition
		switch m.list.GetViewMode() {
		case listview.ViewList:
			if m.list.IsSearching() {
				break
			}
			items := m.list.Items()
			index := m.list.SelectedIndex()
			if index >= 0 && index < len(items) {
				definition = &items[index]
			}
		case listview.ViewDetail:
			if detail, ok := m.list.Detail().(*definitionDetailAdapter); ok &&
				!detail.model.IsEditing() &&
				!detail.model.showParameterList &&
				!detail.model.parameterPrefixPending {
				definition = &detail.model.selected
			}
		}
		if definition != nil {
			m.history = newPipelineRunHistory(*definition, m.client, m.styles, m.width, m.height)
			return m, m.history.fetch(m.client, "", false)
		}
	}
	if result, ok := msg.(pipelineDefinitionsMsg); ok {
		var partial *azdevops.PartialError
		if result.err != nil && !errors.As(result.err, &partial) {
			m.statusMessage = ""
			m.list = m.list.HandleFetchResult(nil, result.err)
			return m, nil
		}
		m.setDefinitionItems(result.definitions)
		m.statusMessage = ""
		if partial != nil {
			m.statusMessage = fmt.Sprintf("Loaded pipelines from %d of %d projects", partial.Total-partial.Failed, partial.Total)
		}
		return m, nil
	}

	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

// View renders the definitions list or selected-pipeline editor.
func (m DefinitionsModel) View() string {
	if m.creation != nil {
		return m.creation.View()
	}
	if m.history != nil {
		title := fmt.Sprintf("Run history — %s (#%d)", m.history.definition.Name, m.history.definition.ID)
		return lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("99")).Render(title) +
			"\n" + m.history.model.View()
	}
	return m.list.View()
}

// IsSearching reports whether a list or variable editor is capturing input.
func (m DefinitionsModel) IsSearching() bool {
	if m.creation != nil {
		return m.creation.searching || m.creation.input.Focused()
	}
	if m.history != nil {
		return m.history.model.IsSearching()
	}
	if m.list.IsSearching() {
		return true
	}
	if m.list.GetViewMode() == listview.ViewDetail {
		if detail, ok := m.list.Detail().(*definitionDetailAdapter); ok {
			return detail.model.IsEditing() || detail.model.showParameterList || detail.model.parameterPrefixPending
		}
	}
	return false
}

// IsShowingRunHistoryList reports whether mouse events should be routed to the
// nested pipeline-run list rather than the selected definition or run detail.
func (m DefinitionsModel) IsShowingRunHistoryList() bool {
	return m.history != nil && m.history.model.GetViewMode() == ViewList
}

// IsShowingDefinitionList reports whether the un-nested definition list is
// visible and can receive row mouse events.
func (m DefinitionsModel) IsShowingDefinitionList() bool {
	return m.history == nil && m.creation == nil && m.list.GetViewMode() == listview.ViewList
}

// HasContextBar reports whether this view has context actions or a status.
func (m DefinitionsModel) HasContextBar() bool {
	if m.creation != nil {
		return true
	}
	if m.history != nil {
		return true
	}
	return m.list.GetViewMode() == listview.ViewList || m.list.HasContextBar() || m.statusMessage != ""
}

// GetContextItems returns current actions.
func (m DefinitionsModel) GetContextItems() []components.ContextItem {
	if m.creation != nil {
		return []components.ContextItem{
			{Key: "j/k", Description: "select"},
			{Key: "/", Description: "filter"},
			{Key: "enter", Description: "select"},
			{Key: "esc", Description: "back"},
		}
	}
	if m.history != nil {
		if m.history.model.GetViewMode() == ViewList {
			items := []components.ContextItem{
				{Key: "j/k", Description: "navigate"},
				{Key: "enter", Description: "view run"},
				{Key: "r", Description: "refresh history"},
				{Key: "esc", Description: "back to pipeline"},
			}
			if !m.history.model.IsSearching() && m.history.continuationToken != "" && !m.history.loadingOlder {
				items = append(items, components.ContextItem{Key: "o", Description: "load older runs"})
			}
			return items
		}
		return m.history.model.GetContextItems()
	}
	if m.list.GetViewMode() == listview.ViewList {
		if m.list.IsSearching() {
			return []components.ContextItem{
				{Key: "enter", Description: "open match"},
				{Key: "esc", Description: "clear search"},
			}
		}
		return []components.ContextItem{
			{Key: "j/k", Description: "navigate"},
			{Key: "/", Description: "search"},
			{Key: "s", Description: "sort by " + m.nextSortFieldName()},
			{Key: "S", Description: "reverse sort"},
			{Key: "v", Description: "run history"},
			{Key: "enter", Description: "open"},
			{Key: "r", Description: "refresh"},
			{Key: "n", Description: "create pipeline"},
			{Key: "c", Description: "copy pipeline"},
			{Key: "P", Description: "add Azure project"},
		}
	}
	return m.list.GetContextItems()
}

func (m *DefinitionsModel) setDefinitionItems(items []azdevops.PipelineDefinition) {
	selectedID, selectedProject := 0, ""
	selectedIndex := m.list.SelectedIndex()
	if !m.list.IsSearching() && selectedIndex >= 0 && selectedIndex < len(m.list.Items()) {
		selected := m.list.Items()[selectedIndex]
		selectedID, selectedProject = selected.ID, selected.ProjectName
	}
	items = append([]azdevops.PipelineDefinition(nil), items...)
	if m.sortActive {
		sortPipelineDefinitions(items, m.sortField, m.sortDescending)
	}
	m.list = m.list.SetItems(items)
	if selectedID == 0 {
		return
	}
	for index, definition := range items {
		if definition.ID == selectedID && definition.ProjectName == selectedProject {
			m.list.SetCursor(index)
			return
		}
	}
}

func sortPipelineDefinitions(items []azdevops.PipelineDefinition, field pipelineDefinitionSortField, descending bool) {
	less := func(i, j int) bool {
		left, right := items[i], items[j]
		if field == sortPipelineName {
			leftName, rightName := strings.ToLower(left.Name), strings.ToLower(right.Name)
			if leftName != rightName {
				return leftName < rightName
			}
			if left.ID != right.ID {
				return left.ID < right.ID
			}
		} else {
			if left.ID != right.ID {
				return left.ID < right.ID
			}
			leftName, rightName := strings.ToLower(left.Name), strings.ToLower(right.Name)
			if leftName != rightName {
				return leftName < rightName
			}
		}
		return strings.ToLower(left.ProjectName) < strings.ToLower(right.ProjectName)
	}
	sort.SliceStable(items, func(i, j int) bool {
		if descending {
			return less(j, i)
		}
		return less(i, j)
	})
}

func (m DefinitionsModel) sortFieldName() string {
	if m.sortField == sortPipelineName {
		return "name"
	}
	return "ID"
}

func (m DefinitionsModel) nextSortFieldName() string {
	if !m.sortActive || m.sortField == sortPipelineName {
		return "ID"
	}
	return "name"
}

// GetScrollPercent returns the detail scroll position, if any.
func (m DefinitionsModel) GetScrollPercent() float64 {
	if m.history != nil {
		return m.history.model.GetScrollPercent()
	}
	return m.list.GetScrollPercent()
}

// GetStatusMessage returns the current detail or partial-load status.
func (m DefinitionsModel) GetStatusMessage() string {
	if m.history != nil {
		if m.history.statusMessage != "" {
			return m.history.statusMessage
		}
		return m.history.model.GetStatusMessage()
	}
	if m.creation != nil && m.creation.status != "" {
		return m.creation.status
	}
	if status := m.list.GetStatusMessage(); status != "" {
		return status
	}
	return m.statusMessage
}

// SetSize forwards the available dimensions to the list and detail.
func (m DefinitionsModel) SetSize(width, height int) (DefinitionsModel, tea.Cmd) {
	m.width = width
	m.height = height
	if m.creation != nil {
		m.creation.width = width
		m.creation.height = height
		return m, nil
	}
	return m.Update(tea.WindowSizeMsg{Width: width, Height: height})
}

func newPipelineRunHistory(definition azdevops.PipelineDefinition, client *azdevops.MultiClient, s *styles.Styles, width, height int) *pipelineRunHistory {
	model := NewModelWithAzureClient(azdevops.NewAdapter(client), client, s)
	model.list = model.list.SetLoading(true)
	childHeight := max(1, height-1)
	model, _ = model.Update(tea.WindowSizeMsg{Width: width, Height: childHeight})
	return &pipelineRunHistory{
		model:         model,
		definition:    definition,
		statusMessage: "Loading run history...",
	}
}

func (h *pipelineRunHistory) fetch(client *azdevops.MultiClient, continuationToken string, appendPage bool) tea.Cmd {
	h.requestID++
	requestID := h.requestID
	definition := h.definition
	return func() tea.Msg {
		if client == nil {
			return pipelineRunHistoryLoadedMsg{
				projectName:  definition.ProjectName,
				definitionID: definition.ID,
				requestID:    requestID,
				appendPage:   appendPage,
				err:          fmt.Errorf("Azure DevOps is not configured"),
			}
		}
		projectClient := client.ClientFor(definition.ProjectName)
		if projectClient == nil {
			return pipelineRunHistoryLoadedMsg{
				projectName:  definition.ProjectName,
				definitionID: definition.ID,
				requestID:    requestID,
				appendPage:   appendPage,
				err:          fmt.Errorf("Azure DevOps client for project %q is unavailable", definition.ProjectName),
			}
		}
		runs, nextToken, err := projectClient.ListPipelineRunsForDefinition(
			definition.ID, pipelineHistoryPageSize, continuationToken)
		mapped := make([]provider.PipelineRun, len(runs))
		for i, run := range runs {
			mapped[i] = azdevops.MapPipelineRun(
				run, definition.ProjectName, client.DisplayNameFor(definition.ProjectName))
		}
		return pipelineRunHistoryLoadedMsg{
			projectName:       definition.ProjectName,
			definitionID:      definition.ID,
			requestID:         requestID,
			runs:              mapped,
			continuationToken: nextToken,
			appendPage:        appendPage,
			err:               err,
		}
	}
}

type pipelineDefinitionsMsg struct {
	definitions []azdevops.PipelineDefinition
	err         error
}

func fetchPipelineDefinitions(client *azdevops.MultiClient) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return pipelineDefinitionsMsg{err: fmt.Errorf("Azure DevOps is not configured")}
		}
		definitions, err := client.ListPipelineDefinitions()
		return pipelineDefinitionsMsg{definitions: definitions, err: err}
	}
}

type definitionDetailAdapter struct {
	model *definitionDetailModel
}

func (a *definitionDetailAdapter) Update(msg tea.Msg) (listview.DetailView, tea.Cmd) {
	var cmd tea.Cmd
	a.model, cmd = a.model.Update(msg)
	return a, cmd
}

func (a *definitionDetailAdapter) View() string {
	return a.model.View()
}

func (a *definitionDetailAdapter) SetSize(width, height int) {
	a.model.SetSize(width, height)
}

func (a *definitionDetailAdapter) GetContextItems() []components.ContextItem {
	return a.model.GetContextItems()
}

func (a *definitionDetailAdapter) GetScrollPercent() float64 {
	return 0
}

func (a *definitionDetailAdapter) GetStatusMessage() string {
	return a.model.statusMessage
}

func (a *definitionDetailAdapter) ShouldHandleEscape() bool {
	return a.model.IsEditing() || a.model.showParameterList || a.model.parameterPrefixPending
}

type definitionLoadedMsg struct {
	definition        *azdevops.PipelineDefinition
	parameters        []runtimeParameter
	yamlVariableNames []string
	yamlVariableErr   error
	parameterErr      error
	err               error
}

type pipelineQueuedMsg struct {
	run *azdevops.PipelineRun
	err error
}

type copyPipelineErrorResultMsg struct {
	err error
}

type pipelineParametersLoadedMsg struct {
	branch            string
	parameters        []runtimeParameter
	yamlVariableNames []string
	yamlVariableErr   error
	err               error
}

type previousRunParametersLoadedMsg struct {
	parameters map[string]any
	err        error
}

type parameterListSequenceTimeoutMsg struct {
	sequenceID uint64
}

type definitionEditor int

const (
	editorNone definitionEditor = iota
	editorBranch
	editorPipelineName
	editorPipelinePath
	editorParameterValue
	editorVariableName
	editorVariableSecret
	editorVariableValue
)

type definitionDetailModel struct {
	client                 *azdevops.Client
	selected               azdevops.PipelineDefinition
	loaded                 bool
	queueing               bool
	updatingDefinition     bool
	branch                 string
	variables              map[string]azdevops.PipelineVariable
	overrides              map[string]azdevops.PipelineVariable
	variableNames          []string
	yamlVariableNames      []string
	yamlVariableErr        error
	parameters             []runtimeParameter
	parameterOverrides     map[string]any
	parametersLoading      bool
	parameterLoadError     string
	previousRunParameters  map[string]any
	previousRunParamsReady bool
	previousRunParamsLoad  bool
	previousRunParamsError error
	previousRunParamsUsed  bool
	showParameterList      bool
	parameterPrefixPending bool
	parameterSequenceID    uint64
	selectedIndex          int
	editor                 definitionEditor
	input                  textinput.Model
	pendingName            string
	pendingSecret          bool
	persistVariable        bool
	statusMessage          string
	width                  int
	height                 int
	newRunMode             bool
	styles                 *styles.Styles
	lastError              string
	showErrorDetails       bool
	errorViewport          viewport.Model
	errorViewportReady     bool
}

func newDefinitionDetailModel(client *azdevops.Client, selected azdevops.PipelineDefinition, s *styles.Styles) *definitionDetailModel {
	input := textinput.New()
	input.CharLimit = 500
	return &definitionDetailModel{
		client:    client,
		selected:  selected,
		branch:    "main",
		variables: make(map[string]azdevops.PipelineVariable),
		overrides: make(map[string]azdevops.PipelineVariable),
		input:     input,
		styles:    s,
	}
}

func (m *definitionDetailModel) Init() tea.Cmd {
	client := m.client
	definitionID := m.selected.ID
	selected := m.selected
	return func() tea.Msg {
		if client == nil {
			return definitionLoadedMsg{err: fmt.Errorf("project client is unavailable")}
		}
		definition, err := client.GetPipelineDefinition(definitionID)
		result := definitionLoadedMsg{definition: definition, err: err}
		if err != nil || definition == nil {
			return result
		}
		if definition.Process.Type == 0 {
			definition.Process.Type = selected.Process.Type
		}
		if definition.Process.YAMLFileName == "" {
			definition.Process.YAMLFileName = selected.Process.YAMLFileName
		}
		if definition.Repository.ID == "" {
			definition.Repository.ID = selected.Repository.ID
		}
		if definition.Repository.DefaultBranch == "" {
			definition.Repository.DefaultBranch = selected.Repository.DefaultBranch
		}
		if definition.Process.Type != 2 && definition.Process.YAMLFileName == "" {
			return result
		}
		yamlPath := definition.Process.YAMLFileName
		repositoryID := definition.Repository.ID
		branch := strings.TrimPrefix(definition.Repository.DefaultBranch, "refs/heads/")
		if yamlPath == "" || repositoryID == "" || branch == "" {
			result.parameterErr = fmt.Errorf("pipeline definition is missing its repository ID, YAML path, or default branch")
			return result
		}
		contents, err := client.GetRepositoryFile(repositoryID, yamlPath, branch)
		if err != nil {
			result.parameterErr = err
			return result
		}
		result.parameters, result.parameterErr = parseRuntimeParameters(contents)
		result.yamlVariableNames, result.yamlVariableErr = parseYAMLVariableNames(contents)
		return result
	}
}

func (m *definitionDetailModel) Update(msg tea.Msg) (*definitionDetailModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.SetSize(msg.Width, msg.Height)
	case parameterListSequenceTimeoutMsg:
		if m.parameterPrefixPending && msg.sequenceID == m.parameterSequenceID {
			m.parameterPrefixPending = false
			m.statusMessage = ""
		}
	case definitionLoadedMsg:
		if msg.err != nil {
			m.statusMessage = "Could not load pipeline variables: " + msg.err.Error()
			return m, nil
		}
		if msg.definition == nil {
			m.statusMessage = "Could not load pipeline definition"
			return m, nil
		}
		m.variables = msg.definition.Variables
		m.selected.Process = msg.definition.Process
		m.selected.Repository = msg.definition.Repository
		m.yamlVariableNames = msg.yamlVariableNames
		m.yamlVariableErr = msg.yamlVariableErr
		if m.variables == nil {
			m.variables = make(map[string]azdevops.PipelineVariable)
		}
		if branch := strings.TrimPrefix(msg.definition.Repository.DefaultBranch, "refs/heads/"); branch != "" {
			m.branch = branch
		}
		m.parameters = msg.parameters
		m.parameterOverrides = make(map[string]any)
		m.parametersLoading = false
		m.parameterLoadError = ""
		m.refreshVariableNames()
		if m.newRunMode && len(m.parameters) > 0 {
			m.selectedIndex = 1
		}
		m.loaded = true
		m.statusMessage = ""
		if msg.parameterErr != nil {
			m.parameterLoadError = msg.parameterErr.Error()
			m.statusMessage = "Could not load YAML parameters: " + msg.parameterErr.Error()
		}
		if m.previousRunParamsLoad {
			m.statusMessage = "Loading previous run parameters..."
		}
		m.applyPreviousRunParameters()
	case pipelineParametersLoadedMsg:
		if msg.branch != m.branch {
			return m, nil
		}
		m.parametersLoading = false
		m.parameters = msg.parameters
		m.yamlVariableNames = msg.yamlVariableNames
		m.yamlVariableErr = msg.yamlVariableErr
		m.parameterOverrides = make(map[string]any)
		m.parameterLoadError = ""
		m.selectedIndex = 0
		if m.newRunMode && len(m.parameters) > 0 {
			m.selectedIndex = 1
		}
		m.statusMessage = ""
		if msg.err != nil {
			m.parameterLoadError = msg.err.Error()
			m.statusMessage = "Could not load YAML parameters: " + msg.err.Error()
		}
		m.previousRunParamsUsed = false
		m.applyPreviousRunParameters()
	case previousRunParametersLoadedMsg:
		m.previousRunParamsLoad = false
		m.previousRunParamsReady = true
		m.previousRunParameters = msg.parameters
		m.previousRunParamsError = msg.err
		m.applyPreviousRunParameters()
	case pipelineQueuedMsg:
		m.queueing = false
		if msg.err != nil {
			m.lastError = msg.err.Error()
			m.showErrorDetails = false
			m.statusMessage = "Pipeline queue failed — press D for details or c to copy"
			return m, nil
		}
		if msg.run == nil {
			m.statusMessage = "Pipeline queue returned no run"
			return m, nil
		}
		m.lastError = ""
		m.showErrorDetails = false
		m.statusMessage = fmt.Sprintf("Queued %s #%s (run %d)", m.selected.Name, msg.run.BuildNumber, msg.run.ID)
	case copyPipelineErrorResultMsg:
		if msg.err != nil {
			m.statusMessage = fmt.Sprintf("Could not copy pipeline error: %v", msg.err)
		} else {
			m.statusMessage = "Pipeline error copied to clipboard"
		}
		return m, nil
	case pipelineDefinitionUpdatedMsg:
		m.updatingDefinition = false
		if msg.err != nil {
			m.statusMessage = fmt.Sprintf("Pipeline %s failed: %v", msg.operation, msg.err)
			return m, nil
		}
		if msg.definition == nil {
			m.statusMessage = fmt.Sprintf("Pipeline %s returned no definition", msg.operation)
			return m, nil
		}
		m.selected.Name = msg.definition.Name
		m.selected.Path = msg.definition.Path
		switch msg.operation {
		case pipelineDefinitionRenameAction:
			m.statusMessage = fmt.Sprintf("Renamed pipeline to %q", msg.definition.Name)
		case pipelineDefinitionMoveAction:
			m.statusMessage = fmt.Sprintf("Moved pipeline to %q", msg.definition.Path)
		case pipelineDefinitionAddVariableAction:
			m.variables = msg.definition.Variables
			if m.variables == nil {
				m.variables = make(map[string]azdevops.PipelineVariable)
			}
			m.selected.Variables = m.variables
			m.refreshVariableNames()
			m.statusMessage = fmt.Sprintf("Saved variable %q to pipeline definition", msg.variableName)
		}
	case tea.KeyMsg:
		if m.showErrorDetails {
			switch msg.String() {
			case "esc":
				m.showErrorDetails = false
				return m, nil
			case "c":
				return m, m.copyPipelineError()
			}
			var cmd tea.Cmd
			m.errorViewport, cmd = m.errorViewport.Update(msg)
			return m, cmd
		}
		if m.updatingDefinition {
			return m, nil
		}
		if m.previousRunParamsLoad && msg.String() != "esc" {
			return m, nil
		}
		if m.editor != editorNone {
			return m.updateEditor(msg)
		}
		if m.parameterPrefixPending {
			m.parameterPrefixPending = false
			m.statusMessage = ""
			if msg.String() == "P" || msg.String() == "p" || msg.String() == "shift+p" {
				m.showParameterList = !m.showParameterList
				return m, nil
			}
		}
		if m.showParameterList && msg.String() == "esc" {
			m.showParameterList = false
			return m, nil
		}
		switch msg.String() {
		case "esc":
			return m, nil
		case "D":
			if m.lastError != "" {
				m.openErrorDetails()
			}
		case "c":
			if m.lastError != "" {
				return m, m.copyPipelineError()
			}
		case "z":
			m.parameterPrefixPending = true
			m.parameterSequenceID++
			sequenceID := m.parameterSequenceID
			m.statusMessage = "Press P (or p) after z to list YAML parameters"
			return m, tea.Tick(1500*time.Millisecond, func(time.Time) tea.Msg {
				return parameterListSequenceTimeoutMsg{sequenceID: sequenceID}
			})
		case "up", "k":
			m.moveSelection(-1)
		case "down", "j":
			m.moveSelection(1)
		case "e":
			return m.editSelected()
		case "N":
			m.beginEditor(editorPipelineName, "Pipeline name: ", m.selected.Name, false)
			return m, m.input.Focus()
		case "M":
			m.beginEditor(editorPipelinePath, "Folder path (\\ for root): ", m.selected.Path, false)
			return m, m.input.Focus()
		case "A":
			return m.addPersistentVariable()
		case "a":
			return m.addVariable()
		case "d":
			m.removeSelectedOverride()
		case "x":
			if m.newRunMode {
				m.clearSelectedVariable()
			}
		case "r":
			return m.runPipeline()
		}
	}
	return m, nil
}

func (m *definitionDetailModel) updateEditor(msg tea.KeyMsg) (*definitionDetailModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.cancelEditor()
		return m, nil
	case "enter":
		return m.commitEditor()
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *definitionDetailModel) editSelected() (*definitionDetailModel, tea.Cmd) {
	if m.selectedIndex == 0 {
		m.beginEditor(editorBranch, "Branch: ", m.branch, false)
		return m, m.input.Focus()
	}
	if parameterIndex := m.selectedIndex - 1; parameterIndex >= 0 && parameterIndex < len(m.parameters) {
		parameter := m.parameters[parameterIndex]
		value := parameterText(parameter.Default, parameter.Type)
		if override, ok := m.parameterOverrides[parameter.Name]; ok {
			value = parameterText(override, parameter.Type)
		}
		m.pendingName = parameter.Name
		m.beginEditor(editorParameterValue, "Value for "+parameter.Name+": ", value, false)
		return m, m.input.Focus()
	}
	name := m.selectedVariable()
	variable, exists := m.variables[name]
	if exists && !variable.AllowOverride {
		m.statusMessage = fmt.Sprintf("Variable %q does not allow queue-time overrides", name)
		return m, nil
	}
	value := variable.Value
	secret := variable.IsSecret
	if override, ok := m.overrides[name]; ok {
		value = override.Value
	}
	if secret {
		value = ""
	}
	m.pendingName = name
	m.pendingSecret = secret
	m.beginEditor(editorVariableValue, "Value for "+name+": ", value, secret)
	return m, m.input.Focus()
}

func (m *definitionDetailModel) addVariable() (*definitionDetailModel, tea.Cmd) {
	m.pendingName = ""
	m.pendingSecret = false
	m.persistVariable = false
	m.beginEditor(editorVariableName, "Variable name: ", "", false)
	return m, m.input.Focus()
}

func (m *definitionDetailModel) addPersistentVariable() (*definitionDetailModel, tea.Cmd) {
	m.pendingName = ""
	m.pendingSecret = false
	m.persistVariable = true
	m.beginEditor(editorVariableName, "Variable name to save: ", "", false)
	return m, m.input.Focus()
}

func (m *definitionDetailModel) beginEditor(mode definitionEditor, prompt, value string, secret bool) {
	m.editor = mode
	m.input.Prompt = prompt
	m.input.SetValue(value)
	m.input.EchoMode = textinput.EchoNormal
	if secret {
		m.input.EchoMode = textinput.EchoPassword
	}
	m.input.Focus()
}

func (m *definitionDetailModel) commitEditor() (*definitionDetailModel, tea.Cmd) {
	value := m.input.Value()
	switch m.editor {
	case editorBranch:
		value = strings.TrimSpace(value)
		if value == "" {
			m.statusMessage = "Branch cannot be empty"
			return m, nil
		}
		m.branch = value
		m.finishEditor()
		if m.isYAMLPipeline() {
			m.parametersLoading = true
			m.statusMessage = "Loading YAML parameters for branch..."
			return m, m.loadParameters(value)
		}
	case editorPipelineName:
		name := strings.TrimSpace(value)
		if name == "" {
			m.statusMessage = "Pipeline name cannot be empty"
			return m, nil
		}
		if name == m.selected.Name {
			m.finishEditor()
			m.statusMessage = "Pipeline name unchanged"
			return m, nil
		}
		return m.updateDefinition(pipelineDefinitionRenameAction, name)
	case editorPipelinePath:
		definitionPath := strings.TrimSpace(value)
		if definitionPath == m.selected.Path {
			m.finishEditor()
			m.statusMessage = "Pipeline folder unchanged"
			return m, nil
		}
		return m.updateDefinition(pipelineDefinitionMoveAction, definitionPath)
	case editorParameterValue:
		parameter, ok := m.parameterByName(m.pendingName)
		if !ok {
			m.statusMessage = "Selected YAML parameter is no longer available"
			m.finishEditor()
			return m, nil
		}
		parsed, err := parseParameterOverride(value, parameter)
		if err != nil {
			m.statusMessage = err.Error()
			return m, nil
		}
		m.parameterOverrides[parameter.Name] = parsed
		parameterIndex := m.parameterIndex(parameter.Name)
		m.selectedIndex = 1 + parameterIndex
		if parameterIndex >= 0 && parameterIndex+1 < len(m.parameters) {
			m.selectedIndex++
		}
		m.finishEditor()
		m.statusMessage = fmt.Sprintf("Updated parameter %q", parameter.Name)
	case editorVariableName:
		name := strings.TrimSpace(value)
		if name == "" {
			m.statusMessage = "Variable name cannot be empty"
			return m, nil
		}
		if m.persistVariable {
			if m.hasVariableName(name) {
				m.statusMessage = fmt.Sprintf("Variable %q already exists", name)
				return m, nil
			}
			m.pendingName = name
			m.beginEditor(editorVariableSecret, "Store as secret? (y/n): ", "", false)
			return m, m.input.Focus()
		}
		if _, exists := m.variables[name]; exists {
			m.statusMessage = fmt.Sprintf("Variable %q already exists", name)
			return m, nil
		}
		if _, exists := m.overrides[name]; exists {
			m.statusMessage = fmt.Sprintf("Variable %q already exists", name)
			return m, nil
		}
		m.pendingName = name
		m.beginEditor(editorVariableValue, "Value for "+name+": ", "", false)
	case editorVariableSecret:
		choice := strings.ToLower(strings.TrimSpace(value))
		switch choice {
		case "y", "yes":
			m.pendingSecret = true
		case "", "n", "no":
			m.pendingSecret = false
		default:
			m.statusMessage = "Enter y or n to choose whether this variable is secret"
			return m, nil
		}
		m.beginEditor(editorVariableValue, "Value for "+m.pendingName+": ", "", m.pendingSecret)
	case editorVariableValue:
		name := m.pendingName
		if m.persistVariable {
			return m.savePersistentVariable(name, value, m.pendingSecret)
		}
		m.overrides[name] = azdevops.PipelineVariable{
			Value:    value,
			IsSecret: m.pendingSecret,
		}
		m.refreshVariableNames()
		for i, name := range m.variableNames {
			if name == m.pendingName {
				m.selectedIndex = m.variableStartIndex() + i
				break
			}
		}
		m.finishEditor()
		if _, isDefinitionVariable := m.variables[name]; isDefinitionVariable {
			m.statusMessage = fmt.Sprintf("Set queue-time override for %q", name)
		} else {
			m.statusMessage = fmt.Sprintf("Added queue-time variable %q for this run", name)
		}
	}
	return m, nil
}

func (m *definitionDetailModel) updateDefinition(operation, value string) (*definitionDetailModel, tea.Cmd) {
	if m.client == nil {
		m.finishEditor()
		m.statusMessage = fmt.Sprintf("Cannot %s pipeline: project client is unavailable", operation)
		return m, nil
	}
	client := m.client
	definitionID := m.selected.ID
	projectName := m.selected.ProjectName
	m.finishEditor()
	m.updatingDefinition = true
	if operation == pipelineDefinitionRenameAction {
		m.statusMessage = "Renaming pipeline..."
	} else {
		m.statusMessage = "Moving pipeline..."
	}
	return m, func() tea.Msg {
		var definition *azdevops.PipelineDefinition
		var err error
		if operation == pipelineDefinitionRenameAction {
			definition, err = client.RenamePipelineDefinition(definitionID, value)
		} else {
			definition, err = client.MovePipelineDefinition(definitionID, value)
		}
		return pipelineDefinitionUpdatedMsg{
			projectName: projectName,
			operation:   operation,
			definition:  definition,
			err:         err,
		}
	}
}

func (m *definitionDetailModel) hasVariableName(name string) bool {
	for existing := range m.variables {
		if strings.EqualFold(existing, name) {
			return true
		}
	}
	for _, existing := range m.yamlVariableNames {
		if strings.EqualFold(existing, name) {
			return true
		}
	}
	return false
}

func (m *definitionDetailModel) savePersistentVariable(name, value string, secret bool) (*definitionDetailModel, tea.Cmd) {
	if m.client == nil {
		m.finishEditor()
		m.statusMessage = "Cannot save variable: project client is unavailable"
		return m, nil
	}
	client := m.client
	definitionID := m.selected.ID
	projectName := m.selected.ProjectName
	m.finishEditor()
	m.updatingDefinition = true
	m.statusMessage = fmt.Sprintf("Saving variable %q to pipeline definition...", name)
	return m, func() tea.Msg {
		definition, err := client.AddPipelineDefinitionVariable(definitionID, name, value, secret)
		return pipelineDefinitionUpdatedMsg{
			projectName:  projectName,
			operation:    pipelineDefinitionAddVariableAction,
			variableName: name,
			definition:   definition,
			err:          err,
		}
	}
}

func (m *definitionDetailModel) finishEditor() {
	m.editor = editorNone
	m.input.Blur()
	m.pendingName = ""
	m.pendingSecret = false
	m.persistVariable = false
}

func (m *definitionDetailModel) cancelEditor() {
	m.finishEditor()
	m.statusMessage = "Edit cancelled"
}

func (m *definitionDetailModel) removeSelectedOverride() {
	if m.selectedIndex == 0 {
		m.statusMessage = "Edit the branch with e"
		return
	}
	if parameterIndex := m.selectedIndex - 1; parameterIndex >= 0 && parameterIndex < len(m.parameters) {
		name := m.parameters[parameterIndex].Name
		if _, exists := m.parameterOverrides[name]; !exists {
			m.statusMessage = fmt.Sprintf("No override set for parameter %q", name)
			return
		}
		delete(m.parameterOverrides, name)
		m.statusMessage = fmt.Sprintf("Reset parameter %q", name)
		return
	}
	name := m.selectedVariable()
	if _, exists := m.overrides[name]; !exists {
		m.statusMessage = fmt.Sprintf("No override set for %q", name)
		return
	}
	delete(m.overrides, name)
	m.refreshVariableNames()
	m.statusMessage = fmt.Sprintf("Removed override for %q", name)
}

func (m *definitionDetailModel) hasSelectedOverride() bool {
	if m.selectedIndex <= 0 {
		return false
	}
	parameterIndex := m.selectedIndex - 1
	if parameterIndex < len(m.parameters) {
		_, exists := m.parameterOverrides[m.parameters[parameterIndex].Name]
		return exists
	}
	name := m.selectedVariable()
	_, exists := m.overrides[name]
	return name != "" && exists
}

func (m *definitionDetailModel) clearSelectedVariable() {
	if m.selectedIndex < m.variableStartIndex() {
		m.statusMessage = "Select a variable to clear"
		return
	}
	name := m.selectedVariable()
	if name == "" {
		m.statusMessage = "No variable is selected"
		return
	}
	variable, exists := m.variables[name]
	if exists && !variable.AllowOverride {
		m.statusMessage = fmt.Sprintf("Variable %q does not allow queue-time overrides", name)
		return
	}
	if override, hasOverride := m.overrides[name]; hasOverride {
		variable = override
	}
	variable.Value = ""
	m.overrides[name] = variable
	m.statusMessage = fmt.Sprintf("Cleared value for variable %q", name)
}

func (m *definitionDetailModel) runPipeline() (*definitionDetailModel, tea.Cmd) {
	if m.queueing {
		return m, nil
	}
	if m.previousRunParamsLoad {
		m.statusMessage = "Previous run parameters are still loading"
		return m, nil
	}
	if !m.loaded {
		m.statusMessage = "Pipeline definition is still loading"
		return m, nil
	}
	if m.parametersLoading {
		m.statusMessage = "YAML parameters are still loading"
		return m, nil
	}
	variables := make(map[string]azdevops.PipelineVariable, len(m.overrides))
	for name, override := range m.overrides {
		variables[name] = override
	}
	client := m.client
	definitionID := m.selected.ID
	branch := m.branch
	parameters := pipelineParameterMap(m.parameters, m.parameterOverrides)
	isYAML := m.selected.Process.Type == 2 || m.selected.Process.YAMLFileName != ""
	secrets := make([]string, 0, len(variables))
	for _, variable := range variables {
		if variable.IsSecret && variable.Value != "" {
			secrets = append(secrets, variable.Value)
		}
	}
	m.queueing = true
	m.lastError = ""
	m.showErrorDetails = false
	m.statusMessage = "Queueing pipeline..."
	return m, func() tea.Msg {
		if client == nil {
			return pipelineQueuedMsg{err: fmt.Errorf("project client is unavailable")}
		}
		var run *azdevops.PipelineRun
		var err error
		if isYAML {
			run, err = client.QueueYAMLPipelineRun(definitionID, branch, parameters, variables)
		} else {
			run, err = client.QueuePipelineWithVariables(definitionID, branch, variables)
		}
		if err != nil && len(secrets) > 0 {
			err = fmt.Errorf("Azure DevOps rejected the run request; error details were hidden because secret variables were included")
		}
		return pipelineQueuedMsg{run: run, err: err}
	}
}

func (m *definitionDetailModel) loadParameters(branch string) tea.Cmd {
	client := m.client
	repositoryID := m.selected.Repository.ID
	yamlPath := m.selected.Process.YAMLFileName
	return func() tea.Msg {
		if client == nil {
			return pipelineParametersLoadedMsg{branch: branch, err: fmt.Errorf("project client is unavailable")}
		}
		if repositoryID == "" || yamlPath == "" {
			return pipelineParametersLoadedMsg{branch: branch, err: fmt.Errorf("pipeline repository or YAML path is unavailable")}
		}
		contents, err := client.GetRepositoryFile(repositoryID, yamlPath, branch)
		if err != nil {
			return pipelineParametersLoadedMsg{branch: branch, err: err}
		}
		parameters, err := parseRuntimeParameters(contents)
		variableNames, variableErr := parseYAMLVariableNames(contents)
		return pipelineParametersLoadedMsg{
			branch:            branch,
			parameters:        parameters,
			yamlVariableNames: variableNames,
			yamlVariableErr:   variableErr,
			err:               err,
		}
	}
}

func (m *definitionDetailModel) applyPreviousRunParameters() {
	if !m.loaded || !m.previousRunParamsReady || m.previousRunParamsUsed {
		return
	}
	m.previousRunParamsUsed = true
	if m.previousRunParamsError != nil {
		m.statusMessage = "Could not load previous run parameters: " + m.previousRunParamsError.Error()
		return
	}
	if len(m.previousRunParameters) == 0 {
		m.statusMessage = "No previous runtime parameters were captured; YAML defaults will be used"
		return
	}

	names := make([]string, 0, len(m.previousRunParameters))
	for name := range m.previousRunParameters {
		names = append(names, name)
	}
	sort.Strings(names)
	var copied, skipped []string
	for _, name := range names {
		parameter, ok := m.parameterByName(name)
		if !ok {
			skipped = append(skipped, name)
			continue
		}
		value, err := parseParameterOverride(parameterText(m.previousRunParameters[name], parameter.Type), parameter)
		if err != nil {
			skipped = append(skipped, name)
			continue
		}
		m.parameterOverrides[name] = value
		copied = append(copied, name)
	}
	switch {
	case len(copied) > 0 && len(skipped) > 0:
		m.statusMessage = fmt.Sprintf("Copied previous parameters: %s; skipped incompatible parameters: %s",
			strings.Join(copied, ", "), strings.Join(skipped, ", "))
	case len(copied) > 0:
		m.statusMessage = "Copied previous parameters: " + strings.Join(copied, ", ")
	case len(skipped) > 0:
		m.statusMessage = "Could not copy previous parameters; incompatible or missing in current YAML: " + strings.Join(skipped, ", ")
	}
}

func (m *definitionDetailModel) refreshVariableNames() {
	names := make(map[string]struct{}, len(m.variables)+len(m.overrides))
	for name := range m.variables {
		names[name] = struct{}{}
	}
	for name := range m.overrides {
		names[name] = struct{}{}
	}
	m.variableNames = make([]string, 0, len(names))
	for name := range names {
		m.variableNames = append(m.variableNames, name)
	}
	sort.Strings(m.variableNames)
	maxIndex := len(m.parameters) + len(m.variableNames)
	if m.selectedIndex > maxIndex {
		m.selectedIndex = maxIndex
	}
}

func (m *definitionDetailModel) moveSelection(delta int) {
	m.selectedIndex += delta
	if m.selectedIndex < 0 {
		m.selectedIndex = 0
	}
	maxIndex := len(m.parameters) + len(m.variableNames)
	if m.selectedIndex > maxIndex {
		m.selectedIndex = maxIndex
	}
}

func (m *definitionDetailModel) selectedVariable() string {
	index := m.selectedIndex - 1 - len(m.parameters)
	if index < 0 || index >= len(m.variableNames) {
		return ""
	}
	return m.variableNames[index]
}

func (m *definitionDetailModel) parameterByName(name string) (runtimeParameter, bool) {
	for _, parameter := range m.parameters {
		if parameter.Name == name {
			return parameter, true
		}
	}
	return runtimeParameter{}, false
}

func (m *definitionDetailModel) parameterIndex(name string) int {
	for index, parameter := range m.parameters {
		if parameter.Name == name {
			return index
		}
	}
	return -1
}

func (m *definitionDetailModel) variableStartIndex() int {
	return 1 + len(m.parameters)
}

func (m *definitionDetailModel) variableDisplayValue(name string) string {
	variable, exists := m.overrides[name]
	if !exists {
		variable = m.variables[name]
	}
	if variable.IsSecret {
		if _, overridden := m.overrides[name]; overridden {
			return "[secret]"
		}
		return "<secret>"
	}
	if variable.Value == "" {
		return "<empty>"
	}
	return variable.Value
}

func (m *definitionDetailModel) parameterDisplayValue(parameter runtimeParameter) string {
	if value, exists := m.parameterOverrides[parameter.Name]; exists {
		return parameterText(value, parameter.Type)
	}
	if parameter.Default == nil {
		return "<no default>"
	}
	return parameterText(parameter.Default, parameter.Type)
}

func (m *definitionDetailModel) isYAMLPipeline() bool {
	yamlPath := strings.ToLower(m.selected.Process.YAMLFileName)
	return m.selected.Process.Type == 2 || strings.HasSuffix(yamlPath, ".yml") || strings.HasSuffix(yamlPath, ".yaml")
}

func (m *definitionDetailModel) IsEditing() bool {
	return m.editor != editorNone
}

func (m *definitionDetailModel) SetSize(width, height int) {
	m.width = width
	m.height = height
	if m.errorViewportReady {
		m.errorViewport.Width = max(width, 1)
		m.errorViewport.Height = max(height-4, 1)
		m.errorViewport.SetContent(wrapDiagnostic(m.lastError, max(width-2, 1)))
	}
}

func (m *definitionDetailModel) GetContextItems() []components.ContextItem {
	if m.showErrorDetails {
		return []components.ContextItem{
			{Key: "↑↓/pgup/pgdn", Description: "scroll error details"},
			{Key: "c", Description: "copy full error"},
			{Key: "esc", Description: "back to pipeline"},
		}
	}
	if m.IsEditing() {
		return []components.ContextItem{
			{Key: "enter", Description: "save"},
			{Key: "esc", Description: "cancel edit"},
		}
	}
	if m.newRunMode {
		items := []components.ContextItem{
			{Key: "j/k", Description: "select parameter"},
			{Key: "e", Description: "edit selected"},
		}
		if m.hasSelectedOverride() {
			items = append(items, components.ContextItem{Key: "d", Description: "reset selected override"})
		}
		if m.selectedIndex >= m.variableStartIndex() && m.selectedVariable() != "" {
			items = append(items, components.ContextItem{Key: "x", Description: "clear variable value"})
		}
		items = append(items,
			components.ContextItem{Key: "r", Description: "save and run"},
			components.ContextItem{Key: "esc", Description: "cancel"},
		)
		return append(items, m.errorContextItems()...)
	}
	if m.showParameterList {
		return []components.ContextItem{
			{Key: "esc", Description: "back to pipeline"},
			{Key: "zP", Description: "close parameter list"},
		}
	}
	items := []components.ContextItem{
		{Key: "j/k", Description: "select"},
		{Key: "zP", Description: "list YAML parameters"},
		{Key: "v/H", Description: "view run history"},
		{Key: "e", Description: "edit"},
		{Key: "N", Description: "rename pipeline"},
		{Key: "M", Description: "move to folder"},
		{Key: "A", Description: "save variable to pipeline (secret option)"},
		{Key: "a", Description: "add variable for this run"},
		{Key: "d", Description: "reset override"},
		{Key: "r", Description: "run pipeline"},
		{Key: "esc", Description: "back"},
	}
	return append(items, m.errorContextItems()...)
}

func (m *definitionDetailModel) View() string {
	if m.showErrorDetails {
		return m.errorDetailsView()
	}
	if m.newRunMode {
		return m.runCreatorView()
	}
	if m.showParameterList {
		return m.parameterListView()
	}
	title := fmt.Sprintf("%s  #%d", m.selected.Name, m.selected.ID)
	if m.selected.ProjectDisplayName != "" {
		title += "  [" + m.selected.ProjectDisplayName + "]"
	}
	var lines []string
	lines = append(lines, title, strings.Repeat("─", max(1, min(m.width, len(title)))))
	lines = append(lines, m.renderRow(0, "Branch", m.branch))
	if !m.loaded {
		lines = append(lines, "", "Loading pipeline variables...")
	} else {
		if m.isYAMLPipeline() || len(m.parameters) > 0 {
			lines = append(lines, "", "YAML Parameters (zP to view)")
			if m.parameterLoadError != "" {
				lines = append(lines, "Could not load parameters: "+m.parameterLoadError)
			} else if len(m.parameters) == 0 {
				lines = append(lines, "No YAML runtime parameters declared.")
			}
			for i, parameter := range m.parameters {
				value := m.parameterDisplayValue(parameter)
				if len(parameter.Values) > 0 {
					value += "  (allowed: " + joinParameterValues(parameter.Values, parameter.Type) + ")"
				}
				name := parameter.Name + " (" + parameter.Type + ")"
				lines = append(lines, m.renderRow(i+1, name, value))
			}
		}
		if m.isYAMLPipeline() {
			if m.yamlVariableErr != nil && m.parameterLoadError == "" {
				lines = append(lines, "", "Could not parse YAML variables: "+m.yamlVariableErr.Error())
			} else if len(m.yamlVariableNames) > 0 {
				lines = append(lines, "", "Variables defined in YAML (names only)")
				for _, name := range m.yamlVariableNames {
					lines = append(lines, "  "+name)
				}
			}
		}
		if len(m.variableNames) == 0 {
			lines = append(lines, "", "No queue-time variables declared. Press A to save one to the pipeline, or a for this run only.")
		} else {
			lines = append(lines, "", "Queue-time variables")
			for i, name := range m.variableNames {
				lines = append(lines, m.renderRow(m.variableStartIndex()+i, name, m.variableDisplayValue(name)))
			}
		}
	}
	if m.IsEditing() {
		lines = append(lines, "", m.input.View(), "enter save • esc cancel")
	}
	if m.statusMessage != "" {
		lines = append(lines, "", m.statusMessage)
	}
	return lipgloss.NewStyle().Width(m.width).Render(strings.Join(lines, "\n"))
}

func (m *definitionDetailModel) errorContextItems() []components.ContextItem {
	if m.lastError == "" {
		return nil
	}
	return []components.ContextItem{
		{Key: "D", Description: "view detailed error"},
		{Key: "c", Description: "copy detailed error"},
	}
}

func (m *definitionDetailModel) openErrorDetails() {
	if m.lastError == "" {
		return
	}
	if !m.errorViewportReady {
		m.errorViewport = viewport.New(max(m.width, 1), max(m.height-4, 1))
		m.errorViewportReady = true
	} else {
		m.errorViewport.Width = max(m.width, 1)
		m.errorViewport.Height = max(m.height-4, 1)
	}
	m.errorViewport.SetContent(wrapDiagnostic(m.lastError, max(m.width-2, 1)))
	m.errorViewport.GotoTop()
	m.showErrorDetails = true
}

func (m *definitionDetailModel) errorDetailsView() string {
	title := "Pipeline Error Details"
	lines := []string{
		title,
		strings.Repeat("─", max(1, min(m.width, len(title)))),
	}
	if m.errorViewportReady {
		lines = append(lines, m.errorViewport.View())
	}
	if m.statusMessage != "" {
		lines = append(lines, m.statusMessage)
	}
	return lipgloss.NewStyle().Width(m.width).Render(strings.Join(lines, "\n"))
}

func (m *definitionDetailModel) copyPipelineError() tea.Cmd {
	if m.lastError == "" {
		m.statusMessage = "No pipeline error details to copy"
		return nil
	}
	details := m.lastError
	return func() tea.Msg {
		return copyPipelineErrorResultMsg{err: writeClipboard(details)}
	}
}

func wrapDiagnostic(text string, width int) string {
	width = max(width, 1)
	paragraphs := strings.Split(text, "\n")
	lines := make([]string, 0, len(paragraphs))
	for _, paragraph := range paragraphs {
		words := strings.Fields(paragraph)
		if len(words) == 0 {
			lines = append(lines, "")
			continue
		}
		line := words[0]
		for _, word := range words[1:] {
			if len([]rune(line))+1+len([]rune(word)) > width {
				lines = append(lines, line)
				line = word
			} else {
				line += " " + word
			}
		}
		lines = append(lines, line)
	}
	return strings.Join(lines, "\n")
}

func (m *definitionDetailModel) runCreatorView() string {
	title := fmt.Sprintf("Run Pipeline — %s  #%d", m.selected.Name, m.selected.ID)
	var lines []string
	lines = append(lines, title, strings.Repeat("─", max(1, min(m.width, len(title)))))
	if m.parametersLoading || !m.loaded {
		lines = append(lines, "", "Loading pipeline parameters...")
	} else if m.parameterLoadError != "" {
		lines = append(lines, "", "Could not load YAML parameters: "+m.parameterLoadError)
	} else {
		type parameterRow struct {
			index int
			name  string
			value string
		}
		rows := make([]parameterRow, 0, 1+len(m.parameters)+len(m.variableNames))
		rows = append(rows, parameterRow{index: 0, name: "Branch", value: m.branch})
		for i, parameter := range m.parameters {
			value := m.parameterDisplayValue(parameter)
			if len(parameter.Values) > 0 {
				value += "  (allowed: " + joinParameterValues(parameter.Values, parameter.Type) + ")"
			}
			rows = append(rows, parameterRow{
				index: 1 + i,
				name:  parameter.Name + " (" + parameter.Type + ")",
				value: value,
			})
		}
		for i, name := range m.variableNames {
			rows = append(rows, parameterRow{
				index: m.variableStartIndex() + i,
				name:  name,
				value: m.variableDisplayValue(name),
			})
		}
		if len(m.parameters) == 0 {
			lines = append(lines, "", "No YAML runtime parameters are declared.")
		} else {
			lines = append(lines, "", "Parameters and queue-time variables:")
		}

		visibleRows := max(1, m.height-8)
		start := max(0, m.selectedIndex-visibleRows+1)
		if start > len(rows)-visibleRows {
			start = max(0, len(rows)-visibleRows)
		}
		end := min(len(rows), start+visibleRows)
		if start > 0 {
			lines = append(lines, "  ... more above")
		}
		for _, row := range rows[start:end] {
			lines = append(lines, m.renderRow(row.index, row.name, row.value))
		}
		if end < len(rows) {
			lines = append(lines, "  ... more below")
		}
		if len(rows) > visibleRows {
			lines = append(lines, fmt.Sprintf("Showing %d-%d of %d", start+1, end, len(rows)))
		}
	}
	if m.IsEditing() {
		lines = append(lines, "", m.input.View(), "enter save value • esc cancel edit")
	} else {
		actions := "j/k select • e edit"
		if m.hasSelectedOverride() {
			actions += " • d reset override"
		}
		if m.selectedIndex >= m.variableStartIndex() && m.selectedVariable() != "" {
			actions += " • x clear variable"
		}
		lines = append(lines, "", actions+" • r save and run • esc cancel")
	}
	if m.previousRunParamsLoad {
		lines = append(lines, "", "Loading values from the selected run...")
	}
	if m.statusMessage != "" {
		lines = append(lines, "", m.statusMessage)
	}
	return lipgloss.NewStyle().Width(m.width).Render(strings.Join(lines, "\n"))
}

func (m *definitionDetailModel) parameterListView() string {
	title := fmt.Sprintf("YAML Parameters — %s  #%d", m.selected.Name, m.selected.ID)
	var lines []string
	lines = append(lines, title, strings.Repeat("─", max(1, min(m.width, len(title)))))
	if !m.loaded || m.parametersLoading {
		lines = append(lines, "", "Loading pipeline parameters...")
	} else if m.parameterLoadError != "" {
		lines = append(lines, "", "Could not load YAML parameters: "+m.parameterLoadError)
	} else if !m.isYAMLPipeline() {
		lines = append(lines, "", "This definition is not a YAML pipeline.")
	} else if len(m.parameters) == 0 {
		lines = append(lines, "", "No YAML runtime parameters are declared for this pipeline.")
	} else {
		for i, parameter := range m.parameters {
			value := m.parameterDisplayValue(parameter)
			if len(parameter.Values) > 0 {
				value += "  (allowed: " + joinParameterValues(parameter.Values, parameter.Type) + ")"
			}
			name := parameter.Name + " (" + parameter.Type + ")"
			lines = append(lines, m.renderRow(i, name, value))
		}
	}
	lines = append(lines, "", "zP close • esc back")
	return lipgloss.NewStyle().Width(m.width).Render(strings.Join(lines, "\n"))
}

func (m *definitionDetailModel) renderRow(index int, name, value string) string {
	line := fmt.Sprintf("%-24s %s", name, value)
	if index == m.selectedIndex && !m.IsEditing() {
		return m.styles.TableSelected.Render(line)
	}
	return m.styles.TableCell.Render(line)
}
