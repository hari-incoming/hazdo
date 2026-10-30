package pipelines

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/hari-incoming/hazdo/internal/azdevops"
	"github.com/hari-incoming/hazdo/internal/provider"
	"github.com/hari-incoming/hazdo/internal/ui/components"
	"github.com/hari-incoming/hazdo/internal/ui/components/listview"
	"github.com/hari-incoming/hazdo/internal/ui/components/table"
	"github.com/hari-incoming/hazdo/internal/ui/display"
	"github.com/hari-incoming/hazdo/internal/ui/styles"
	tea "github.com/charmbracelet/bubbletea"
)

// ViewMode represents the current view in the pipelines UI
type ViewMode int

const (
	ViewList       ViewMode = iota // Pipeline list view
	ViewDetail                     // Pipeline detail/timeline view
	ViewLogs                       // Log viewer
	ViewRunCreator                 // New run parameters editor
)

// Model represents the pipeline list view with sub-views
type Model struct {
	list                    listview.Model[provider.PipelineRun]
	client                  provider.Provider
	azureClient             *azdevops.MultiClient
	logViewer               *LogViewerModel
	runCreator              *definitionDetailModel
	viewMode                ViewMode
	width                   int
	height                  int
	styles                  *styles.Styles
	activeStatus            string
	statusPicker            components.ListPicker
	allRuns                 []provider.PipelineRun
	durationTimerGeneration uint64
	durationTimerScheduled  bool
}

type pipelineDurationTickMsg struct {
	generation uint64
}

// NewModel creates a new pipeline list model with default styles
func NewModel(client provider.Provider) Model {
	return NewModelWithStyles(client, styles.DefaultStyles())
}

// runBaseColumns are the per-row column specs for the pipeline run list,
// excluding the optional project and glyph columns.
var runBaseColumns = []listview.ColumnSpec{
	{Title: "Status", WidthPct: 10, MinWidth: 10},
	{Title: "Pipeline", WidthPct: 12, MinWidth: 15},
	{Title: "Branch", WidthPct: 20, MinWidth: 10},
	{Title: "Build", WidthPct: 24, MinWidth: 8},
	{Title: "Timestamp", WidthPct: 15, MinWidth: 16},
	{Title: "Duration", WidthPct: 10, MinWidth: 8},
}

// NewModelWithStyles creates a new pipeline list model with custom styles
func NewModelWithStyles(client provider.Provider, s *styles.Styles) Model {
	return NewModelWithAzureClient(client, nil, s)
}

// NewModelWithAzureClient creates a pipeline list model with the Azure client
// used to load definitions and queue a new run from a recent run's detail.
func NewModelWithAzureClient(client provider.Provider, azureClient *azdevops.MultiClient, s *styles.Styles) Model {
	isMulti := client != nil && client.IsMultiProject()

	// toColumns derives column specs from the current items, mirroring the
	// cell gating in runsToRows / runsToRowsMulti exactly:
	//   [glyph?] [project?] [status] [pipeline] [branch] [build] [timestamp] [duration]
	toColumns := func(items []provider.PipelineRun) []listview.ColumnSpec {
		kinds := make([]provider.Kind, len(items))
		for i, run := range items {
			kinds[i] = run.Identity.Kind
		}
		mixed := display.MixedKinds(kinds)

		cols := make([]listview.ColumnSpec, len(runBaseColumns))
		copy(cols, runBaseColumns)

		if isMulti {
			cols = append([]listview.ColumnSpec{{Title: "Project", WidthPct: 12, MinWidth: 10}}, cols...)
		}
		if mixed {
			cols = append([]listview.ColumnSpec{{Title: "", WidthPct: 3, MinWidth: 3}}, cols...)
		}

		listview.NormalizeWidths(cols)
		return cols
	}

	toRows := runsToRows
	if isMulti {
		toRows = runsToRowsMulti
	}

	filterFunc := filterPipelineRun
	if isMulti {
		filterFunc = filterPipelineRunMulti
	}

	cfg := listview.Config[provider.PipelineRun]{
		LoadingMessage: "Loading pipeline runs...",
		EntityName:     "pipeline runs",
		MinWidth:       50,
		ToRows:         toRows,
		ToColumns:      toColumns,
		Fetch: func() tea.Cmd {
			return fetchPipelineRuns(client)
		},
		EnterDetail: func(item provider.PipelineRun, st *styles.Styles, w, h int) (listview.DetailView, tea.Cmd) {
			d := NewDetailModelWithStyles(client, item, st)
			d.SetSize(w, h)
			return &detailAdapter{d}, d.Init()
		},
		OpenOnDoubleClick: true,
		HasContextBar: func(mode listview.ViewMode) bool {
			return mode == listview.ViewDetail
		},
		FilterFunc: filterFunc,
	}

	return Model{
		list:         listview.New(cfg, s),
		client:       client,
		azureClient:  azureClient,
		viewMode:     ViewList,
		styles:       s,
		statusPicker: components.NewListPicker(s),
	}
}

// Init initializes the model
func (m Model) Init() tea.Cmd {
	return m.list.Init()
}

// Update handles messages
func (m Model) Update(msg tea.Msg) (Model, tea.Cmd) {
	// Track window size for log viewer
	if wmsg, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = wmsg.Width
		m.height = wmsg.Height
		if m.runCreator != nil {
			m.runCreator.SetSize(wmsg.Width, wmsg.Height)
		}
	}

	// Handle domain-specific messages
	switch msg := msg.(type) {
	case pipelineDurationTickMsg:
		if msg.generation != m.durationTimerGeneration {
			return m, nil
		}
		m.durationTimerScheduled = false
		if m.viewMode != ViewList || !hasRunningPipelineRun(m.allRuns) {
			return m, nil
		}
		m.list = m.list.SetItems(m.applyStatusFilter(m.allRuns))
		return m, m.ensurePipelineDurationTimer()
	case pipelineRunsMsg:
		criticalCmd := components.NewCriticalErrorCmd(msg.err)
		if criticalCmd != nil {
			// Critical errors are shown via the error modal; don't display inline
			m.stopPipelineDurationTimer()
			m.list = m.list.HandleFetchResult(nil, nil)
			return m, criticalCmd
		}
		// For partial errors, treat data as valid (some projects succeeded)
		var partialErr *azdevops.PartialError
		if errors.As(msg.err, &partialErr) {
			m.allRuns = msg.runs
			m.list = m.list.HandleFetchResult(m.applyStatusFilter(msg.runs), nil)
			return m, m.ensurePipelineDurationTimer()
		}
		m.allRuns = msg.runs
		m.list = m.list.HandleFetchResult(m.applyStatusFilter(msg.runs), msg.err)
		if msg.err != nil {
			m.stopPipelineDurationTimer()
			return m, nil
		}
		return m, m.ensurePipelineDurationTimer()
	case SetRunsMsg:
		m.allRuns = msg.Runs
		m.list = m.list.SetItems(m.applyStatusFilter(msg.Runs))
		return m, m.ensurePipelineDurationTimer()

	case components.ListPickerSelectedMsg:
		m.activeStatus = msg.Value
		m.statusPicker.Hide()
		m.list = m.list.SetItems(m.applyStatusFilter(m.allRuns))
		return m, nil
	}

	// When status picker is visible, route all input to it
	if m.statusPicker.IsVisible() {
		if kmsg, ok := msg.(tea.KeyMsg); ok {
			var cmd tea.Cmd
			m.statusPicker, cmd = m.statusPicker.Update(kmsg)
			return m, cmd
		}
		return m, nil
	}

	// Route by pipeline-specific view mode
	switch m.viewMode {
	case ViewLogs:
		return m.updateLogViewer(msg)
	case ViewDetail:
		return m.updateDetail(msg)
	case ViewRunCreator:
		return m.updateRunCreator(msg)
	default:
		if keyMsg, ok := msg.(tea.KeyMsg); ok && keyMsg.String() == "S" && !m.list.IsSearching() && m.viewMode == ViewList {
			statuses := getPipelineStatuses()
			options := make([]components.ListPickerOption, len(statuses))
			for i, status := range statuses {
				options[i] = components.ListPickerOption{Name: status.Name, Icon: status.Icon}
			}
			m.statusPicker.SetConfig("Filter by Status", options, m.activeStatus, true)
			m.statusPicker.Show()
			return m, nil
		}
		var cmd tea.Cmd
		m.list, cmd = m.list.Update(msg)
		// Sync viewMode from generic model
		if m.list.GetViewMode() == listview.ViewDetail {
			m.viewMode = ViewDetail
		} else {
			m.viewMode = ViewList
		}
		if m.viewMode == ViewList {
			return m, tea.Batch(cmd, m.ensurePipelineDurationTimer())
		}
		m.stopPipelineDurationTimer()
		return m, cmd
	}
}

// updateDetail intercepts detail-mode messages for enter (expand/collapse + log nav)
func (m Model) updateDetail(msg tea.Msg) (Model, tea.Cmd) {
	if keyMsg, ok := msg.(tea.KeyMsg); ok {
		if adapter, ok := m.list.Detail().(*detailAdapter); ok && adapter.model.showRunParams {
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			return m, cmd
		}

		// When the detail view is searching, let it handle all keys
		// except enter (which should still toggle expand / open logs)
		if adapter, ok := m.list.Detail().(*detailAdapter); ok && adapter.model.IsSearching() {
			if keyMsg.String() != "enter" {
				var cmd tea.Cmd
				m.list, cmd = m.list.Update(msg)
				return m, cmd
			}
		}

		switch keyMsg.String() {
		case "enter":
			// Get the detail adapter to access the underlying DetailModel
			if adapter, ok := m.list.Detail().(*detailAdapter); ok {
				detail := adapter.model
				if selected := detail.SelectedItem(); selected != nil && selected.HasChildren() {
					detail.ToggleExpand()
					return m, nil
				}
				return m.enterLogView(adapter)
			}
			return m, nil
		case "esc":
			// Delegate to generic model which handles esc -> list
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			m.viewMode = ViewList
			return m, tea.Batch(cmd, m.ensurePipelineDurationTimer())
		case "n":
			if adapter, ok := m.list.Detail().(*detailAdapter); ok &&
				adapter.model.run.Identity.Kind == provider.KindAzure &&
				!adapter.model.IsSearching() {
				return m.startNewRun(adapter.model)
			}
		}
	}

	// Delegate other messages to the generic model
	var cmd tea.Cmd
	m.list, cmd = m.list.Update(msg)
	return m, cmd
}

func (m Model) startNewRun(detail *DetailModel) (Model, tea.Cmd) {
	run := detail.GetRun()
	if run.DefinitionID <= 0 {
		detail.statusMessage = "Cannot run new: pipeline definition ID is unavailable"
		return m, nil
	}
	if m.azureClient == nil {
		detail.statusMessage = "Cannot run new: Azure DevOps client is unavailable"
		return m, nil
	}
	projectClient := m.azureClient.ClientFor(run.Identity.Scope)
	if projectClient == nil {
		detail.statusMessage = "Cannot run new: no Azure DevOps client for this project"
		return m, nil
	}
	m.runCreator = newDefinitionDetailModel(projectClient, azdevops.PipelineDefinition{
		ID:                 run.DefinitionID,
		Name:               run.DefinitionName,
		ProjectName:        run.Identity.Scope,
		ProjectDisplayName: run.Identity.ScopeDisplay,
	}, m.styles)
	m.runCreator.newRunMode = true
	m.runCreator.SetSize(m.width, m.height)
	m.viewMode = ViewRunCreator
	commands := []tea.Cmd{m.runCreator.Init()}
	if detail.runParamsDone {
		m.runCreator.previousRunParameters = detail.runParameters
		m.runCreator.previousRunParamsReady = true
	} else {
		m.runCreator.previousRunParamsLoad = true
		m.runCreator.statusMessage = "Loading previous run parameters..."
		source, ok := m.client.(provider.PipelineRunParameterSource)
		runID := parseBuildID(run.Identity.ID)
		commands = append(commands, func() tea.Msg {
			if !ok {
				return previousRunParametersLoadedMsg{err: fmt.Errorf("pipeline run parameters are unavailable for this provider")}
			}
			if runID <= 0 {
				return previousRunParametersLoadedMsg{err: fmt.Errorf("pipeline run ID is unavailable")}
			}
			parameters, err := source.GetPipelineRunParameters(run.Identity.Scope, run.DefinitionID, runID)
			return previousRunParametersLoadedMsg{parameters: parameters, err: err}
		})
	}
	return m, tea.Batch(commands...)
}

func (m Model) updateRunCreator(msg tea.Msg) (Model, tea.Cmd) {
	if m.runCreator == nil {
		m.viewMode = ViewDetail
		return m, nil
	}
	if key, ok := msg.(tea.KeyMsg); ok && key.String() == "esc" &&
		!m.runCreator.IsEditing() && !m.runCreator.showParameterList && !m.runCreator.parameterPrefixPending {
		m.runCreator = nil
		m.viewMode = ViewDetail
		return m, nil
	}
	var cmd tea.Cmd
	m.runCreator, cmd = m.runCreator.Update(msg)
	return m, cmd
}

// updateLogViewer handles updates for the log viewer
func (m Model) updateLogViewer(msg tea.Msg) (Model, tea.Cmd) {
	if m.logViewer == nil {
		m.viewMode = ViewDetail
		return m, nil
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			if m.logViewer.IsSearching() {
				var cmd tea.Cmd
				m.logViewer, cmd = m.logViewer.Update(msg)
				return m, cmd
			}
			m.viewMode = ViewDetail
			m.logViewer = nil
			return m, nil
		}
	}

	var cmd tea.Cmd
	m.logViewer, cmd = m.logViewer.Update(msg)
	return m, cmd
}

// enterLogView navigates to the log viewer for the selected timeline item
func (m Model) enterLogView(adapter *detailAdapter) (Model, tea.Cmd) {
	detail := adapter.model
	selected := detail.SelectedItem()
	if selected == nil || selected.Record.LogID == 0 {
		return m, nil
	}

	run := detail.GetRun()
	m.logViewer = NewLogViewerModelWithStyles(
		m.client,
		run.Identity.Scope,
		parseBuildID(run.Identity.ID),
		selected.Record.LogID,
		selected.Record.Name,
		m.styles,
	)
	m.logViewer.SetSize(m.width, m.height)
	m.viewMode = ViewLogs

	return m, m.logViewer.Init()
}

// View renders the view
func (m Model) View() string {
	if m.viewMode == ViewRunCreator && m.runCreator != nil {
		return m.runCreator.View()
	}
	if m.viewMode == ViewLogs && m.logViewer != nil {
		return m.logViewer.View()
	}
	return m.list.View()
}

// GetViewMode returns the current view mode (for testing)
func (m Model) GetViewMode() ViewMode {
	return m.viewMode
}

// GetContextItems returns context bar items for the current view
func (m Model) GetContextItems() []components.ContextItem {
	if m.viewMode == ViewRunCreator && m.runCreator != nil {
		return m.runCreator.GetContextItems()
	}
	if m.viewMode == ViewLogs && m.logViewer != nil {
		return m.logViewer.GetContextItems()
	}
	return m.list.GetContextItems()
}

// GetScrollPercent returns the scroll percentage for the current view
func (m Model) GetScrollPercent() float64 {
	if m.viewMode == ViewRunCreator {
		return 0
	}
	if m.viewMode == ViewLogs && m.logViewer != nil {
		return m.logViewer.GetScrollPercent()
	}
	return m.list.GetScrollPercent()
}

// GetStatusMessage returns the status message for the current view
func (m Model) GetStatusMessage() string {
	if m.viewMode == ViewRunCreator && m.runCreator != nil {
		return m.runCreator.statusMessage
	}
	return m.list.GetStatusMessage()
}

// HasContextBar returns true if the current view should show a context bar
func (m Model) HasContextBar() bool {
	if m.viewMode == ViewLogs || m.viewMode == ViewRunCreator {
		return true
	}
	return m.list.HasContextBar()
}

// IsSearching returns true if the list or detail view is currently in search/filter mode.
func (m Model) IsSearching() bool {
	if m.list.IsSearching() {
		return true
	}
	if m.viewMode == ViewRunCreator && m.runCreator != nil {
		return m.runCreator.IsEditing() || m.runCreator.showParameterList || m.runCreator.parameterPrefixPending
	}
	if m.viewMode == ViewLogs && m.logViewer != nil {
		return m.logViewer.IsSearching()
	}
	if m.viewMode == ViewDetail {
		if adapter, ok := m.list.Detail().(*detailAdapter); ok {
			return adapter.model.IsSearching()
		}
	}
	return false
}

// detailAdapter wraps *DetailModel to satisfy listview.DetailView
type detailAdapter struct {
	model *DetailModel
}

func (a *detailAdapter) Update(msg tea.Msg) (listview.DetailView, tea.Cmd) {
	var cmd tea.Cmd
	a.model, cmd = a.model.Update(msg)
	return a, cmd
}

func (a *detailAdapter) View() string {
	return a.model.View()
}

func (a *detailAdapter) SetSize(width, height int) {
	a.model.SetSize(width, height)
}

func (a *detailAdapter) GetContextItems() []components.ContextItem {
	return a.model.GetContextItems()
}

func (a *detailAdapter) GetScrollPercent() float64 {
	return a.model.GetScrollPercent()
}

func (a *detailAdapter) ShouldHandleEscape() bool {
	return a.model.searching || a.model.showRunParams || a.model.approvals != nil
}

func (a *detailAdapter) GetStatusMessage() string {
	return a.model.GetStatusMessage()
}

// runsToRows converts pipeline runs to table rows.
// When the items span more than one distinct provider Kind (detected via
// display.MixedKinds), a leading glyph cell is prepended to each row so the
// user can tell which backend each entry originates from.
func runsToRows(items []provider.PipelineRun, s *styles.Styles) []table.Row {
	kinds := make([]provider.Kind, len(items))
	for i, run := range items {
		kinds[i] = run.Identity.Kind
	}
	mixed := display.MixedKinds(kinds)

	rows := make([]table.Row, len(items))
	for i, run := range items {
		cells := table.Row{
			statusIconWithStyles(run.RunStatus, s),
			run.DefinitionName,
			run.SourceBranch,
			run.BuildNumber,
			runTimestamp(run.QueueTime),
			runDuration(run.StartTime, run.FinishTime),
		}
		if mixed {
			cells = append(table.Row{display.KindStyle(run.Identity.Kind, s).Render(display.KindGlyph(run.Identity.Kind))}, cells...)
		}
		rows[i] = cells
	}
	return rows
}

// statusIconWithStyles returns a colored status icon using the provided styles.
// It uses the neutral RunStatus enum and the display map so theming is
// centralised in internal/ui/display.
func statusIconWithStyles(runStatus provider.RunStatus, s *styles.Styles) string {
	glyph := display.RunStatusGlyph(runStatus)
	label := display.RunStatusLabel(runStatus)
	style := display.RunStatusStyle(runStatus, s)
	if label == "" {
		// RunStatusUnknown (and detail-only statuses) have no list label;
		// render just the glyph to avoid showing an empty string.
		return style.Render(glyph)
	}
	return style.Render(glyph + " " + label)
}

// runsToRowsMulti converts pipeline runs to table rows with a Project column.
// When the items span more than one distinct provider Kind (detected via
// display.MixedKinds), a leading glyph cell is prepended before the Project
// column so the layout is: [glyph?] [project] [status] [pipeline] …
func runsToRowsMulti(items []provider.PipelineRun, s *styles.Styles) []table.Row {
	kinds := make([]provider.Kind, len(items))
	for i, run := range items {
		kinds[i] = run.Identity.Kind
	}
	mixed := display.MixedKinds(kinds)

	rows := make([]table.Row, len(items))
	for i, run := range items {
		cells := table.Row{
			run.Identity.ScopeDisplay,
			statusIconWithStyles(run.RunStatus, s),
			run.DefinitionName,
			run.SourceBranch,
			run.BuildNumber,
			runTimestamp(run.QueueTime),
			runDuration(run.StartTime, run.FinishTime),
		}
		if mixed {
			cells = append(table.Row{display.KindStyle(run.Identity.Kind, s).Render(display.KindGlyph(run.Identity.Kind))}, cells...)
		}
		rows[i] = cells
	}
	return rows
}

// filterPipelineRun returns true if the pipeline run matches the search query.
func filterPipelineRun(run provider.PipelineRun, query string) bool {
	if query == "" {
		return true
	}
	q := strings.ToLower(query)
	return strings.Contains(strings.ToLower(run.DefinitionName), q) ||
		strings.Contains(strings.ToLower(run.SourceBranch), q) ||
		strings.Contains(strings.ToLower(run.BuildNumber), q)
}

// filterPipelineRunMulti matches pipeline run fields including project name.
func filterPipelineRunMulti(run provider.PipelineRun, query string) bool {
	if query == "" {
		return true
	}
	q := strings.ToLower(query)
	return strings.Contains(strings.ToLower(run.Identity.ScopeDisplay), q) ||
		strings.Contains(strings.ToLower(run.Identity.Scope), q) ||
		strings.Contains(strings.ToLower(run.DefinitionName), q) ||
		strings.Contains(strings.ToLower(run.SourceBranch), q) ||
		strings.Contains(strings.ToLower(run.BuildNumber), q)
}

type pipelineStatus struct {
	Name string
	Icon string
}

func getPipelineStatuses() []pipelineStatus {
	return []pipelineStatus{
		{Name: "Running", Icon: "●"},
		{Name: "Queued", Icon: "○"},
		{Name: "Success", Icon: "✓"},
		{Name: "Failed", Icon: "✗"},
		{Name: "Cancel", Icon: "⊘"},
		{Name: "Partial", Icon: "◐"},
	}
}

func getStatusKey(runStatus provider.RunStatus) string {
	return display.RunStatusLabel(runStatus)
}

func (m Model) applyStatusFilter(runs []provider.PipelineRun) []provider.PipelineRun {
	if m.activeStatus == "" {
		return runs
	}
	var filtered []provider.PipelineRun
	for _, run := range runs {
		if getStatusKey(run.RunStatus) == m.activeStatus {
			filtered = append(filtered, run)
		}
	}
	return filtered
}

func (m Model) IsStatusFilterActive() bool {
	return m.activeStatus != ""
}

func (m Model) ActiveStatus() string {
	return m.activeStatus
}

func (m Model) IsStatusPickerVisible() bool {
	return m.statusPicker.IsVisible()
}

func (m Model) StatusPickerView() string {
	return m.statusPicker.View()
}

func (m *Model) SetStatusPickerSize(width, height int) {
	m.statusPicker.SetSize(width, height)
}

// Messages

type pipelineRunsMsg struct {
	runs []provider.PipelineRun
	err  error
}

// SetRunsMsg is a message to directly set the pipeline runs (from polling)
type SetRunsMsg struct {
	Runs []provider.PipelineRun
}

// fetchPipelineRuns fetches pipeline runs via the provider.
func fetchPipelineRuns(client provider.Provider) tea.Cmd {
	return func() tea.Msg {
		if client == nil {
			return pipelineRunsMsg{runs: nil, err: nil}
		}
		runs, err := client.ListPipelineRuns(30, provider.ListOpts{})
		return pipelineRunsMsg{runs: runs, err: err}
	}
}

// parseBuildID parses the numeric build ID from the Identity.ID string.
// Returns 0 if the string cannot be parsed.
func parseBuildID(id string) int {
	n, _ := strconv.Atoi(id)
	return n
}

// runTimestamp formats a queue time for display in the pipeline table.
func runTimestamp(t time.Time) string {
	return t.Format("2006-01-02 15:04")
}

// runDuration returns a human-readable duration for a pipeline run.
func runDuration(startTime, finishTime *time.Time) string {
	if startTime == nil {
		return "-"
	}
	end := time.Now()
	if finishTime != nil {
		end = *finishTime
	}
	d := end.Sub(*startTime)
	if d < 0 {
		d = 0
	}
	return formatDuration(d)
}

func hasRunningPipelineRun(runs []provider.PipelineRun) bool {
	for _, run := range runs {
		if run.StartTime != nil && run.FinishTime == nil {
			return true
		}
	}
	return false
}

func (m *Model) ensurePipelineDurationTimer() tea.Cmd {
	if m.viewMode != ViewList {
		return nil
	}
	if !hasRunningPipelineRun(m.allRuns) {
		m.stopPipelineDurationTimer()
		return nil
	}
	if m.durationTimerScheduled {
		return nil
	}
	m.durationTimerGeneration++
	generation := m.durationTimerGeneration
	m.durationTimerScheduled = true
	return tea.Tick(time.Second, func(time.Time) tea.Msg {
		return pipelineDurationTickMsg{generation: generation}
	})
}

func (m *Model) stopPipelineDurationTimer() {
	m.durationTimerScheduled = false
	m.durationTimerGeneration++
}
