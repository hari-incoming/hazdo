package pipelines

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/hari-incoming/hazdo/internal/azdevops"
	"github.com/hari-incoming/hazdo/internal/ui/styles"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"gopkg.in/yaml.v3"
)

type creationStep int

const (
	creationProject creationStep = iota
	creationRepository
	creationBranch
	creationYAMLFile
	creationPool
	creationName
	creationReview
	creationVariableName
	creationVariableSecret
	creationVariableValue
)

const yamlPoolChoice = "No default queue (use YAML pool if configured)"

type repositoryOptionsMsg struct {
	repositories []azdevops.Repository
	err          error
}

type branchOptionsMsg struct {
	branches []string
	err      error
}

type yamlFilesMsg struct {
	files []string
	err   error
}

type agentQueuesMsg struct {
	queues         []azdevops.AgentPoolQueue
	yamlPoolName   string
	yamlPoolDetail string
	queueErr       error
	yamlErr        error
}

type pipelineCreatedMsg struct {
	definition *azdevops.PipelineDefinition
	err        error
	secrets    []string
	copied     bool
}

type pipelineCopySourceLoadedMsg struct {
	definition *azdevops.PipelineDefinition
	repository azdevops.Repository
	yamlFiles  []string
	err        error
}

type pipelineCreationModel struct {
	client             *azdevops.MultiClient
	styles             *styles.Styles
	step               creationStep
	projects           []string
	project            string
	projectClient      *azdevops.Client
	repositories       []azdevops.Repository
	repository         azdevops.Repository
	branches           []string
	branch             string
	yamlFiles          []string
	yamlPath           string
	agentQueues        []azdevops.AgentPoolQueue
	agentQueue         *azdevops.AgentPoolQueue
	yamlPoolName       string
	yamlPoolDetail     string
	yamlPoolQueue      int
	agentQueueFromYAML bool
	pipelinePath       string
	name               string
	variables          map[string]azdevops.PipelineVariable
	pendingName        string
	pendingSecret      bool
	pendingExisting    bool
	copyMode           bool
	sourceName         string
	sourceYAMLPath     string
	secretsToReset     []string
	input              textinput.Model
	searchInput        textinput.Model
	searching          bool
	cursor             int
	loading            bool
	creating           bool
	status             string
	width              int
	height             int
	cancelled          bool
}

func newPipelineCreationModel(client *azdevops.MultiClient, s *styles.Styles) *pipelineCreationModel {
	input := textinput.New()
	input.CharLimit = 500
	search := textinput.New()
	search.Prompt = "/"
	search.CharLimit = 200
	projects := []string(nil)
	if client != nil {
		projects = client.Projects()
		sort.Strings(projects)
	}
	return &pipelineCreationModel{
		client:        client,
		styles:        s,
		step:          creationProject,
		projects:      projects,
		variables:     make(map[string]azdevops.PipelineVariable),
		input:         input,
		searchInput:   search,
		yamlPoolQueue: -1,
	}
}

func newPipelineCopyModel(client *azdevops.MultiClient, s *styles.Styles, selected azdevops.PipelineDefinition) (*pipelineCreationModel, tea.Cmd) {
	m := newPipelineCreationModel(client, s)
	m.copyMode = true
	m.sourceName = selected.Name
	m.project = selected.ProjectName
	m.name = selected.Name + " copy"
	m.step = creationYAMLFile
	m.loading = true
	m.status = "Loading source pipeline..."
	if client == nil {
		m.loading = false
		m.status = "Cannot copy pipeline: Azure DevOps client is unavailable"
		return m, nil
	}
	projectClient := client.ClientFor(selected.ProjectName)
	if projectClient == nil {
		m.loading = false
		m.status = "Cannot copy pipeline: project client is unavailable"
		return m, nil
	}

	return m, func() tea.Msg {
		definition, err := projectClient.GetPipelineDefinition(selected.ID)
		if err != nil {
			return pipelineCopySourceLoadedMsg{err: err}
		}
		if definition.Process.Type != 2 {
			return pipelineCopySourceLoadedMsg{err: fmt.Errorf("only YAML pipelines can be copied")}
		}
		if strings.TrimSpace(definition.Process.YAMLFileName) == "" {
			return pipelineCopySourceLoadedMsg{err: fmt.Errorf("source pipeline has no YAML file")}
		}
		if strings.TrimSpace(definition.Repository.ID) == "" {
			return pipelineCopySourceLoadedMsg{err: fmt.Errorf("source pipeline has no repository")}
		}
		repositories, err := projectClient.ListRepositories()
		if err != nil {
			return pipelineCopySourceLoadedMsg{err: err}
		}
		var repository azdevops.Repository
		for _, candidate := range repositories {
			if candidate.ID == definition.Repository.ID {
				repository = candidate
				break
			}
		}
		if repository.ID == "" {
			return pipelineCopySourceLoadedMsg{err: fmt.Errorf("source pipeline repository %q was not found", definition.Repository.ID)}
		}
		if branch := strings.TrimSpace(definition.Repository.DefaultBranch); branch != "" {
			repository.DefaultBranch = branch
		}
		files, err := projectClient.ListYAMLPipelineFiles(repository.ID, repository.DefaultBranch)
		if err != nil {
			return pipelineCopySourceLoadedMsg{err: err}
		}
		return pipelineCopySourceLoadedMsg{
			definition: definition,
			repository: repository,
			yamlFiles:  files,
		}
	}
}

func (m *pipelineCreationModel) Update(msg tea.Msg) (*pipelineCreationModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width, m.height = msg.Width, msg.Height
	case pipelineCopySourceLoadedMsg:
		m.loading = false
		if msg.err != nil {
			m.status = "Could not prepare pipeline copy: " + msg.err.Error()
			return m, nil
		}
		if msg.definition == nil {
			m.status = "Could not prepare pipeline copy: source definition is unavailable"
			return m, nil
		}
		if m.client == nil {
			m.status = "Could not prepare pipeline copy: Azure DevOps client is unavailable"
			return m, nil
		}
		m.projectClient = m.client.ClientFor(m.project)
		if m.projectClient == nil {
			m.status = "Could not prepare pipeline copy: project client is unavailable"
			return m, nil
		}
		m.repository = msg.repository
		m.pipelinePath = msg.definition.Path
		m.branch = strings.TrimPrefix(msg.repository.DefaultBranch, "refs/heads/")
		m.agentQueue = msg.definition.Queue
		m.yamlFiles = make([]string, 0, len(msg.yamlFiles))
		m.sourceYAMLPath = msg.definition.Process.YAMLFileName
		for _, file := range msg.yamlFiles {
			if cleanPipelineYAMLPath(file) != cleanPipelineYAMLPath(m.sourceYAMLPath) {
				m.yamlFiles = append(m.yamlFiles, file)
			}
		}
		if len(m.yamlFiles) == 0 {
			m.status = "No other YAML files were found in the source repository"
			return m, nil
		}
		m.variables = make(map[string]azdevops.PipelineVariable, len(msg.definition.Variables))
		m.secretsToReset = nil
		for name, variable := range msg.definition.Variables {
			if variable.IsSecret {
				variable.Value = ""
				m.secretsToReset = append(m.secretsToReset, name)
			}
			m.variables[name] = variable
		}
		sort.Strings(m.secretsToReset)
		m.cursor = 0
		m.resetSearch()
		m.status = ""
	case repositoryOptionsMsg:
		m.loading = false
		if msg.err != nil {
			m.status = "Could not list repositories: " + msg.err.Error()
			return m, nil
		}
		m.repositories = msg.repositories
		sort.Slice(m.repositories, func(i, j int) bool {
			return strings.ToLower(m.repositories[i].Name) < strings.ToLower(m.repositories[j].Name)
		})
		m.step = creationRepository
		m.cursor = 0
		m.resetSearch()
		m.status = ""
	case branchOptionsMsg:
		m.loading = false
		if msg.err != nil {
			m.status = "Could not list repository branches: " + msg.err.Error()
			return m, nil
		}
		if len(msg.branches) == 0 {
			m.status = "No branches were found in the selected repository"
			return m, nil
		}
		m.branches = msg.branches
		m.step = creationBranch
		m.cursor = 0
		defaultBranch := strings.TrimPrefix(strings.TrimSpace(m.repository.DefaultBranch), "refs/heads/")
		for i, branch := range m.branches {
			if branch == defaultBranch {
				m.cursor = i
				break
			}
		}
		m.branch = m.branches[m.cursor]
		m.resetSearch()
		m.status = ""
	case yamlFilesMsg:
		m.loading = false
		if msg.err != nil {
			m.status = "Could not browse repository YAML files: " + msg.err.Error()
			return m, nil
		}
		m.yamlFiles = msg.files
		m.step = creationYAMLFile
		m.cursor = 0
		m.resetSearch()
		m.status = ""
	case agentQueuesMsg:
		m.loading = false
		m.agentQueues = msg.queues
		m.step = creationPool
		m.agentQueue = nil
		m.agentQueueFromYAML = false
		m.yamlPoolName = msg.yamlPoolName
		m.yamlPoolDetail = msg.yamlPoolDetail
		m.yamlPoolQueue = matchingAgentQueue(msg.queues, msg.yamlPoolName)
		m.cursor = 0
		if m.yamlPoolQueue >= 0 {
			m.cursor = m.yamlPoolQueue
		} else if msg.yamlErr != nil || m.yamlPoolDetail != "" {
			m.cursor = len(m.agentQueues)
		}
		m.resetSearch()
		var notices []string
		if msg.yamlErr != nil {
			notices = append(notices, "Could not inspect the YAML pool: "+msg.yamlErr.Error()+"; no queue override is preselected.")
		} else if m.yamlPoolDetail != "" {
			if m.yamlPoolQueue >= 0 {
				notices = append(notices, fmt.Sprintf("YAML pool %q matches %s; that queue is preselected. Choose another queue to change it.", m.yamlPoolName, agentQueueLabel(msg.queues[m.yamlPoolQueue])))
			} else {
				notices = append(notices, fmt.Sprintf("YAML pool %q is retained; no matching project queue was found. Choose a queue to override it.", m.yamlPoolDetail))
			}
		} else {
			notices = append(notices, "No pool is defined in the YAML; choose a queue or continue without a default queue.")
		}
		if msg.queueErr != nil {
			notices = append(notices, "Could not list agent queues: "+msg.queueErr.Error())
		}
		m.status = strings.Join(notices, " ")
	case tea.KeyMsg:
		if m.loading || m.creating {
			return m, nil
		}
		if m.searching {
			return m.updateSearch(msg)
		}
		if m.input.Focused() {
			return m.updateInput(msg)
		}
		switch msg.String() {
		case "esc", "q":
			m.back()
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < len(m.visibleChoices())-1 {
				m.cursor++
			}
		case "/":
			if m.step == creationProject || m.step == creationRepository || m.step == creationBranch || m.step == creationYAMLFile || m.step == creationPool {
				m.searching = true
				m.searchInput.SetValue("")
				m.searchInput.Focus()
			}
		case "enter":
			return m.selectCurrent()
		case "y", "Y":
			if m.step == creationVariableSecret {
				return m, m.chooseSecret(true)
			}
		case "n", "N":
			if m.step == creationVariableSecret {
				return m, m.chooseSecret(false)
			}
		case "a":
			if m.step == creationReview {
				m.pendingName = ""
				m.pendingSecret = false
				m.pendingExisting = false
				m.step = creationVariableName
				m.beginInput("Variable name: ", "", false)
				return m, m.input.Focus()
			}
		case "c":
			if m.step == creationReview {
				return m.create()
			}
		}
	}
	return m, nil
}

func (m *pipelineCreationModel) updateSearch(msg tea.KeyMsg) (*pipelineCreationModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.searching = false
		m.searchInput.Blur()
		m.searchInput.SetValue("")
		m.cursor = 0
		return m, nil
	case "enter":
		m.searching = false
		m.searchInput.Blur()
		m.cursor = 0
		return m, nil
	}
	var cmd tea.Cmd
	m.searchInput, cmd = m.searchInput.Update(msg)
	m.cursor = 0
	return m, cmd
}

func (m *pipelineCreationModel) updateInput(msg tea.KeyMsg) (*pipelineCreationModel, tea.Cmd) {
	switch msg.String() {
	case "esc":
		m.input.Blur()
		m.status = "Edit cancelled"
		if m.step == creationName {
			m.step = creationYAMLFile
		} else if m.step == creationVariableName {
			m.step = creationReview
		} else if m.step == creationVariableValue {
			if m.pendingExisting {
				m.secretsToReset = append([]string{m.pendingName}, m.secretsToReset...)
			} else {
				delete(m.variables, m.pendingName)
			}
			m.step = creationReview
			m.pendingName = ""
			m.pendingSecret = false
			m.pendingExisting = false
		}
		return m, nil
	case "enter":
		value := m.input.Value()
		switch m.step {
		case creationName:
			m.name = strings.TrimSpace(value)
			if m.name == "" {
				m.status = "Pipeline name cannot be empty"
				return m, nil
			}
			m.input.Blur()
			if m.promptNextSecret() {
				return m, m.input.Focus()
			}
			m.step = creationReview
			m.status = ""
		case creationVariableName:
			name := strings.TrimSpace(value)
			if name == "" {
				m.status = "Variable name cannot be empty"
				return m, nil
			}
			if _, exists := m.variables[name]; exists {
				m.status = fmt.Sprintf("Variable %q already exists", name)
				return m, nil
			}
			m.pendingName = name
			m.input.Blur()
			m.step = creationVariableSecret
			m.status = ""
		case creationVariableValue:
			variable := m.variables[m.pendingName]
			variable.Value = value
			variable.IsSecret = m.pendingSecret
			if !m.pendingExisting {
				variable.AllowOverride = true
			}
			m.variables[m.pendingName] = variable
			m.input.Blur()
			m.pendingName = ""
			m.pendingSecret = false
			m.pendingExisting = false
			if m.promptNextSecret() {
				return m, m.input.Focus()
			}
			m.step = creationReview
			m.status = "Variable added"
		}
		return m, nil
	}
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)
	return m, cmd
}

func (m *pipelineCreationModel) selectCurrent() (*pipelineCreationModel, tea.Cmd) {
	choices := m.visibleChoices()
	if m.cursor < 0 || m.cursor >= len(choices) {
		return m, nil
	}
	switch m.step {
	case creationProject:
		m.project = choices[m.cursor]
		m.resetSearch()
		m.projectClient = m.client.ClientFor(m.project)
		if m.projectClient == nil {
			m.status = "Project client is unavailable"
			return m, nil
		}
		m.loading = true
		m.status = "Loading repositories..."
		client := m.projectClient
		return m, func() tea.Msg {
			repositories, err := client.ListRepositories()
			return repositoryOptionsMsg{repositories: repositories, err: err}
		}
	case creationRepository:
		m.repository = m.repositories[m.filteredRepositoryIndexes()[m.cursor]]
		m.resetSearch()
		if m.projectClient == nil {
			m.status = "Project client is unavailable"
			return m, nil
		}
		m.loading = true
		m.status = "Loading repository branches..."
		client := m.projectClient
		repository := m.repository
		return m, func() tea.Msg {
			branches, err := client.ListBranches(repository.ID)
			return branchOptionsMsg{branches: branches, err: err}
		}
	case creationBranch:
		m.branch = choices[m.cursor]
		m.repository.DefaultBranch = "refs/heads/" + m.branch
		m.resetSearch()
		if m.projectClient == nil {
			m.status = "Project client is unavailable"
			return m, nil
		}
		m.loading = true
		m.status = "Browsing YAML files on " + m.branch + "..."
		m.step = creationYAMLFile
		client := m.projectClient
		repository := m.repository
		return m, func() tea.Msg {
			files, err := client.ListYAMLPipelineFiles(repository.ID, repository.DefaultBranch)
			return yamlFilesMsg{files: files, err: err}
		}
	case creationYAMLFile:
		m.yamlPath = choices[m.cursor]
		m.resetSearch()
		if !m.copyMode {
			m.name = strings.TrimSuffix(path.Base(m.yamlPath), path.Ext(m.yamlPath))
			if m.projectClient == nil {
				m.status = "Project client is unavailable"
				return m, nil
			}
			m.step = creationPool
			m.loading = true
			m.status = "Loading agent queues..."
			client := m.projectClient
			repository := m.repository
			yamlPath := m.yamlPath
			return m, func() tea.Msg {
				contents, yamlErr := client.GetRepositoryFile(repository.ID, yamlPath, repository.DefaultBranch)
				var yamlPoolName, yamlPoolDetail string
				if yamlErr == nil {
					yamlPoolName, yamlPoolDetail, yamlErr = parseYAMLAgentPool(contents)
				}
				queues, queueErr := client.ListAgentQueues()
				return agentQueuesMsg{
					queues:         queues,
					yamlPoolName:   yamlPoolName,
					yamlPoolDetail: yamlPoolDetail,
					queueErr:       queueErr,
					yamlErr:        yamlErr,
				}
			}
		}
		m.step = creationName
		m.beginInput("Pipeline name: ", m.name, false)
		return m, m.input.Focus()
	case creationPool:
		m.agentQueue = nil
		m.agentQueueFromYAML = false
		if choice := choices[m.cursor]; choice != yamlPoolChoice {
			for i, queue := range m.agentQueues {
				if agentQueueLabel(queue) == choice {
					selectedQueue := queue
					m.agentQueue = &selectedQueue
					m.agentQueueFromYAML = i == m.yamlPoolQueue
					break
				}
			}
		}
		m.status = ""
		m.step = creationName
		m.beginInput("Pipeline name: ", m.name, false)
		return m, m.input.Focus()
	case creationVariableSecret:
		return m, nil
	}
	return m, nil
}

func (m *pipelineCreationModel) chooseSecret(secret bool) tea.Cmd {
	m.pendingSecret = secret
	m.pendingExisting = false
	m.step = creationVariableValue
	m.beginInput("Value: ", "", secret)
	return m.input.Focus()
}

func (m *pipelineCreationModel) promptNextSecret() bool {
	if len(m.secretsToReset) == 0 {
		return false
	}
	m.pendingName = m.secretsToReset[0]
	m.secretsToReset = m.secretsToReset[1:]
	m.pendingSecret = true
	m.pendingExisting = true
	m.step = creationVariableValue
	m.beginInput("Re-enter secret variable "+m.pendingName+": ", "", true)
	return true
}

func (m *pipelineCreationModel) create() (*pipelineCreationModel, tea.Cmd) {
	if m.creating {
		return m, nil
	}
	if len(m.secretsToReset) > 0 {
		m.status = "Re-enter all source secret variables before creating the copy"
		return m, nil
	}
	m.creating = true
	m.status = "Creating pipeline..."
	client := m.projectClient
	name, repository, yamlPath, pipelinePath, queue := m.name, m.repository, m.yamlPath, m.pipelinePath, m.agentQueue
	copied := m.copyMode
	variables := make(map[string]azdevops.PipelineVariable, len(m.variables))
	secrets := make([]string, 0, len(m.variables))
	for key, variable := range m.variables {
		variables[key] = variable
		if variable.IsSecret && variable.Value != "" {
			secrets = append(secrets, variable.Value)
		}
	}
	return m, func() tea.Msg {
		if client == nil {
			return pipelineCreatedMsg{err: fmt.Errorf("project client is unavailable"), secrets: secrets, copied: copied}
		}
		definition, err := client.CreateYAMLPipelineDefinition(name, repository, yamlPath, variables, pipelinePath, queue)
		return pipelineCreatedMsg{definition: definition, err: err, secrets: secrets, copied: copied}
	}
}

func (m *pipelineCreationModel) visibleChoices() []string {
	var choices []string
	switch m.step {
	case creationProject:
		choices = append(choices, m.projects...)
	case creationRepository:
		for _, index := range m.filteredRepositoryIndexes() {
			choices = append(choices, m.repositories[index].Name)
		}
	case creationBranch:
		choices = append(choices, m.branches...)
	case creationYAMLFile:
		choices = append(choices, m.yamlFiles...)
	case creationPool:
		for _, queue := range m.agentQueues {
			choices = append(choices, agentQueueLabel(queue))
		}
		choices = append(choices, yamlPoolChoice)
	}
	filter := strings.ToLower(m.searchInput.Value())
	if filter == "" {
		return choices
	}
	filtered := make([]string, 0, len(choices))
	for _, choice := range choices {
		if strings.Contains(strings.ToLower(choice), filter) {
			filtered = append(filtered, choice)
		}
	}
	return filtered
}

func (m *pipelineCreationModel) filteredRepositoryIndexes() []int {
	filter := strings.ToLower(m.searchInput.Value())
	var indexes []int
	for i, repository := range m.repositories {
		if filter == "" || strings.Contains(strings.ToLower(repository.Name), filter) {
			indexes = append(indexes, i)
		}
	}
	return indexes
}

func cleanPipelineYAMLPath(file string) string {
	return path.Clean("/" + strings.TrimSpace(file))
}

func parseYAMLAgentPool(contents string) (string, string, error) {
	var document yaml.Node
	if err := yaml.Unmarshal([]byte(contents), &document); err != nil {
		return "", "", fmt.Errorf("parse pipeline YAML: %w", err)
	}
	if len(document.Content) == 0 || document.Content[0].Kind != yaml.MappingNode {
		return "", "", nil
	}

	var pool *yaml.Node
	root := document.Content[0]
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "pool" {
			pool = root.Content[i+1]
			break
		}
	}
	if pool == nil || pool.Tag == "!!null" {
		return "", "", nil
	}
	if pool.Kind == yaml.AliasNode && pool.Alias != nil {
		pool = pool.Alias
	}
	if pool.Kind == yaml.ScalarNode {
		name := strings.TrimSpace(pool.Value)
		return name, name, nil
	}
	if pool.Kind == yaml.MappingNode {
		for i := 0; i+1 < len(pool.Content); i += 2 {
			key, value := pool.Content[i].Value, pool.Content[i+1]
			if value.Kind != yaml.ScalarNode || value.Tag == "!!null" {
				continue
			}
			if key == "name" {
				name := strings.TrimSpace(value.Value)
				if name != "" {
					return name, "name: " + name, nil
				}
			}
		}
		for i := 0; i+1 < len(pool.Content); i += 2 {
			key, value := pool.Content[i].Value, pool.Content[i+1]
			if key == "vmImage" && value.Kind == yaml.ScalarNode && value.Tag != "!!null" {
				image := strings.TrimSpace(value.Value)
				if image != "" {
					return "", "vmImage: " + image, nil
				}
			}
		}
	}
	return "", "configured pool settings", nil
}

func matchingAgentQueue(queues []azdevops.AgentPoolQueue, poolName string) int {
	poolName = strings.TrimSpace(poolName)
	if poolName == "" {
		return -1
	}
	for i, queue := range queues {
		if strings.EqualFold(strings.TrimSpace(queue.Name), poolName) {
			return i
		}
	}
	for i, queue := range queues {
		if strings.EqualFold(strings.TrimSpace(queue.Pool.Name), poolName) {
			return i
		}
	}
	return -1
}

func agentQueueLabel(queue azdevops.AgentPoolQueue) string {
	return fmt.Sprintf("%s (pool: %s)", queue.Name, queue.Pool.Name)
}

func (m *pipelineCreationModel) beginInput(prompt, value string, secret bool) {
	m.input.Prompt = prompt
	m.input.SetValue(value)
	m.input.EchoMode = textinput.EchoNormal
	if secret {
		m.input.EchoMode = textinput.EchoPassword
	}
	m.input.Focus()
}

func (m *pipelineCreationModel) back() {
	if m.searchInput.Value() != "" {
		m.searchInput.SetValue("")
		m.cursor = 0
		return
	}
	switch m.step {
	case creationProject:
		m.status = "Creation cancelled"
		m.cancelled = true
	case creationRepository:
		m.step = creationProject
	case creationBranch:
		m.step = creationRepository
		for i, repository := range m.repositories {
			if repository.ID == m.repository.ID {
				m.cursor = i
				break
			}
		}
	case creationYAMLFile:
		if m.copyMode {
			m.cancelled = true
		} else {
			m.step = creationBranch
			for i, branch := range m.branches {
				if branch == m.branch {
					m.cursor = i
					break
				}
			}
		}
	case creationPool:
		m.step = creationYAMLFile
	case creationName:
		if m.copyMode {
			m.step = creationYAMLFile
		} else {
			m.step = creationPool
		}
	case creationReview:
		m.step = creationName
		m.beginInput("Pipeline name: ", m.name, false)
	case creationVariableName, creationVariableSecret, creationVariableValue:
		if m.step == creationVariableValue && m.pendingExisting {
			m.secretsToReset = append([]string{m.pendingName}, m.secretsToReset...)
		}
		m.step = creationReview
		m.pendingName = ""
		m.pendingSecret = false
		m.pendingExisting = false
		m.input.Blur()
	}
	m.resetSearch()
}

func (m *pipelineCreationModel) resetSearch() {
	m.searching = false
	m.searchInput.Blur()
	m.searchInput.SetValue("")
}

func (m *pipelineCreationModel) View() string {
	title := "Create YAML Pipeline"
	if m.copyMode {
		title = "Copy YAML Pipeline"
	}
	var lines []string
	lines = append(lines, title, strings.Repeat("─", len(title)))
	switch m.step {
	case creationProject, creationRepository, creationBranch, creationYAMLFile, creationPool:
		switch m.step {
		case creationProject:
			lines = append(lines, "Choose a project:")
		case creationRepository:
			lines = append(lines, "Choose a repository in "+m.project+":")
		case creationBranch:
			lines = append(lines, "Choose a branch in "+m.repository.Name+" ("+m.branch+"): ")
		case creationYAMLFile:
			prompt := "Choose a YAML file in "
			if m.copyMode {
				prompt = "Choose a different YAML file in "
			}
			lines = append(lines, prompt+m.repository.Name+" ("+m.branch+"): ")
		case creationPool:
			lines = append(lines, "Choose an agent queue or continue without a default queue:")
		}
		if m.step == creationPool && m.yamlPoolDetail != "" {
			if m.yamlPoolQueue >= 0 {
				lines = append(lines, "YAML pool "+m.yamlPoolDetail+" matches a queue, selected by default; move to change it.")
			} else {
				lines = append(lines, "YAML pool "+m.yamlPoolDetail+" is retained unless you choose a queue override.")
			}
		}
		if m.searching {
			lines = append(lines, m.searchInput.View())
		} else {
			lines = append(lines, "(/ filters choices)")
		}
		choices := m.visibleChoices()
		if len(choices) == 0 {
			lines = append(lines, "  No choices found")
		}
		for i, choice := range choices {
			cursor := "  "
			if i == m.cursor {
				cursor = "> "
			}
			lines = append(lines, cursor+choice)
		}
	case creationName:
		lines = append(lines, "Set a name for the pipeline:", "", m.input.View())
	case creationReview:
		reviewTitle := "Review before creating:"
		if m.copyMode {
			reviewTitle = "Review pipeline copy:"
		}
		agentQueue := "No default queue"
		if m.yamlPoolDetail != "" {
			agentQueue = "YAML pool: " + m.yamlPoolDetail
		}
		if m.agentQueue != nil {
			agentQueue = agentQueueLabel(*m.agentQueue)
			if m.agentQueueFromYAML {
				agentQueue += " (selected from YAML)"
			}
		}
		lines = append(lines, reviewTitle, "Project: "+m.project, "Repository: "+m.repository.Name,
			"Branch: "+m.branch, "YAML: "+m.yamlPath, "Agent queue: "+agentQueue)
		if m.yamlPoolDetail != "" {
			lines = append(lines, "YAML pool: "+m.yamlPoolDetail)
		}
		lines = append(lines, "Name: "+m.name)
		if m.copyMode {
			lines = append(lines, "Copy of: "+m.sourceName)
		}
		lines = append(lines, "", "Variables:")
		if len(m.variables) == 0 {
			lines = append(lines, "  none")
		} else {
			names := make([]string, 0, len(m.variables))
			for name := range m.variables {
				names = append(names, name)
			}
			sort.Strings(names)
			for _, name := range names {
				value := m.variables[name].Value
				if m.variables[name].IsSecret {
					value = "[secret]"
				} else if value == "" {
					value = "<empty>"
				}
				lines = append(lines, fmt.Sprintf("  %s = %s", name, value))
			}
		}
		if m.copyMode && len(m.secretsToReset) > 0 {
			lines = append(lines, "", "Re-enter the remaining source secret variables before creating.")
		}
		lines = append(lines, "", "a add variable • c create • esc back")
	case creationVariableName:
		lines = append(lines, "Enter a variable name:", "", m.input.View())
	case creationVariableSecret:
		lines = append(lines, "Should "+m.pendingName+" be secret? (y/n)")
	case creationVariableValue:
		prompt := "Enter variable value:"
		if m.pendingExisting {
			prompt = "Re-enter secret variable " + m.pendingName + ":"
		}
		lines = append(lines, prompt, "", m.input.View())
		if m.pendingSecret {
			lines = append(lines, "Secret input is hidden.")
			if m.pendingExisting {
				lines = append(lines, "Azure DevOps does not return existing secret values; re-enter them to copy the pipeline.")
			}
		}
	}
	if m.loading {
		lines = append(lines, "", m.status)
	}
	if m.status != "" && !m.loading {
		lines = append(lines, "", m.status)
	}
	lines = append(lines, "", "j/k move • enter select • esc back")
	view := lipgloss.NewStyle().Width(m.width).Render(strings.Join(lines, "\n"))
	if m.width > 0 && m.height > 0 {
		view = lipgloss.Place(m.width, m.height, lipgloss.Center, lipgloss.Center, view)
	}
	return view
}
