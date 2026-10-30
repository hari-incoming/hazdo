package pipelines

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/hari-incoming/hazdo/internal/azdevops"
	"github.com/hari-incoming/hazdo/internal/ui/components/listview"
	"github.com/hari-incoming/hazdo/internal/ui/styles"
	tea "github.com/charmbracelet/bubbletea"
)

func pipelineKey(r rune) tea.KeyMsg {
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}}
}

func TestDefinitionsListContextIncludesCreateAndCopyActions(t *testing.T) {
	model := NewDefinitionsModel(nil, styles.DefaultStyles())
	if !model.HasContextBar() {
		t.Fatal("All Pipelines list should show its context actions")
	}

	items := model.GetContextItems()
	found := map[string]bool{}
	for _, item := range items {
		found[item.Key] = true
	}
	for _, key := range []string{"n", "c", "j/k", "enter", "r", "s", "S"} {
		if !found[key] {
			t.Errorf("All Pipelines context actions are missing %q: %+v", key, items)
		}
	}

	model, _ = model.Update(pipelineKey('/'))
	items = model.GetContextItems()
	for _, item := range items {
		if item.Key == "n" || item.Key == "c" {
			t.Errorf("search context should not advertise pipeline actions: %+v", items)
		}
	}

	model = NewDefinitionsModel(nil, styles.DefaultStyles())
	model, _ = model.Update(pipelineKey('n'))
	if model.creation == nil {
		t.Fatal("n should start the new pipeline wizard")
	}
}

func TestDefinitionsListSortsByPipelineIDAndName(t *testing.T) {
	model := NewDefinitionsModel(nil, styles.DefaultStyles())
	model.list = model.list.SetItems([]azdevops.PipelineDefinition{
		{ID: 20, Name: "Beta"},
		{ID: 30, Name: "Alpha"},
		{ID: 10, Name: "Gamma"},
	})
	model.list.SetCursor(0)

	model, _ = model.Update(pipelineKey('s'))
	if got := definitionIDs(model.list.Items()); !equalInts(got, []int{10, 20, 30}) {
		t.Fatalf("ID sort = %v, want ascending IDs", got)
	}
	if selected := model.list.Items()[model.list.SelectedIndex()].ID; selected != 20 {
		t.Fatalf("selected pipeline after sort = %d, want selection preserved on ID 20", selected)
	}

	model, _ = model.Update(pipelineKey('s'))
	if got := definitionIDs(model.list.Items()); !equalInts(got, []int{30, 20, 10}) {
		t.Fatalf("name sort = %v, want Alpha, Beta, Gamma", got)
	}
	model, _ = model.Update(pipelineKey('S'))
	if got := definitionIDs(model.list.Items()); !equalInts(got, []int{10, 20, 30}) {
		t.Fatalf("descending name sort = %v, want Gamma, Beta, Alpha", got)
	}

	model, _ = model.Update(pipelineDefinitionsMsg{definitions: []azdevops.PipelineDefinition{
		{ID: 20, Name: "Beta"},
		{ID: 10, Name: "Gamma"},
		{ID: 30, Name: "Alpha"},
	}})
	if got := definitionIDs(model.list.Items()); !equalInts(got, []int{10, 20, 30}) {
		t.Fatalf("refreshed sorted definitions = %v, want descending name order", got)
	}
}

func definitionIDs(definitions []azdevops.PipelineDefinition) []int {
	ids := make([]int, len(definitions))
	for i, definition := range definitions {
		ids[i] = definition.ID
	}
	return ids
}

func equalInts(left, right []int) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func TestPipelineCreationSearchFiltersSelectableYAMLFiles(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
			return
		}
		switch r.URL.Path {
		case "/git/repositories/repo-1/items":
			_, _ = w.Write([]byte(`{"content":"pool:\n  name: Self hosted\n"}`))
		case "/distributedtask/queues":
			_, _ = w.Write([]byte(`{"value":[
				{"id":4,"name":"Linux","pool":{"id":2,"name":"Self hosted"}}
			]}`))
		default:
			t.Errorf("unexpected request path %s", r.URL.Path)
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := azdevops.NewMultiClient("org", []string{"project"}, "pat", nil)
	if err != nil {
		t.Fatal(err)
	}
	client.ClientFor("project").SetBaseURL(server.URL)
	model := NewDefinitionsModel(client, styles.DefaultStyles())
	model.creation = newPipelineCreationModel(client, styles.DefaultStyles())
	model.creation.projectClient = client.ClientFor("project")
	model.creation.project = "project"
	model.creation.step = creationYAMLFile
	model.creation.repository.ID = "repo-1"
	model.creation.repository.Name = "service"
	model.creation.repository.DefaultBranch = "refs/heads/main"
	model.creation.yamlFiles = []string{
		"/azure-pipelines.yml",
		"/deploy/staging.yaml",
		"/deploy/production.yaml",
	}

	hasSearchAction := false
	for _, item := range model.GetContextItems() {
		if item.Key == "/" && item.Description == "filter" {
			hasSearchAction = true
		}
	}
	if !hasSearchAction {
		t.Fatal("pipeline creation context should advertise / to filter YAML choices")
	}

	model.creation, _ = model.creation.Update(pipelineKey('/'))
	for _, r := range "PRODUCTION" {
		model.creation, _ = model.creation.Update(pipelineKey(r))
	}
	choices := model.creation.visibleChoices()
	if len(choices) != 1 || choices[0] != "/deploy/production.yaml" {
		t.Fatalf("filtered YAML choices = %v, want only /deploy/production.yaml", choices)
	}

	model.creation, _ = model.creation.Update(tea.KeyMsg{Type: tea.KeyEnter})
	var cmd tea.Cmd
	model.creation, cmd = model.creation.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("selecting a YAML file did not load available agent queues")
	}
	model.creation, _ = model.creation.Update(cmd())
	if model.creation.yamlPath != "/deploy/production.yaml" || model.creation.step != creationPool {
		t.Fatalf("selected YAML/next step = %q/%v, want production YAML/agent queue", model.creation.yamlPath, model.creation.step)
	}
	if got := model.creation.visibleChoices(); len(got) != 2 ||
		got[0] != "Linux (pool: Self hosted)" || got[1] != yamlPoolChoice {
		t.Fatalf("pool choices = %v", got)
	}
	if model.creation.cursor != 0 {
		t.Fatalf("selected pool cursor = %d, want matching YAML queue at index 0", model.creation.cursor)
	}
	model.creation, _ = model.creation.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.creation.step != creationName || model.creation.agentQueue == nil ||
		model.creation.agentQueue.Pool.Name != "Self hosted" || !model.creation.agentQueueFromYAML {
		t.Fatalf("selected queue/next step = %+v/%v", model.creation.agentQueue, model.creation.step)
	}
}

func TestParseYAMLAgentPool(t *testing.T) {
	tests := []struct {
		name       string
		contents   string
		wantName   string
		wantDetail string
		wantErr    bool
	}{
		{
			name:       "named queue",
			contents:   "pool: Self hosted\n",
			wantName:   "Self hosted",
			wantDetail: "Self hosted",
		},
		{
			name:       "named mapping",
			contents:   "pool:\n  vmImage: ubuntu-latest\n  name: Self hosted\n",
			wantName:   "Self hosted",
			wantDetail: "name: Self hosted",
		},
		{
			name:       "hosted image",
			contents:   "pool:\n  vmImage: ubuntu-latest\n",
			wantDetail: "vmImage: ubuntu-latest",
		},
		{
			name:     "not valid yaml",
			contents: "pool: [unterminated\n",
			wantErr:  true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotName, gotDetail, err := parseYAMLAgentPool(tt.contents)
			if (err != nil) != tt.wantErr {
				t.Fatalf("parseYAMLAgentPool() error = %v, wantErr %v", err, tt.wantErr)
			}
			if gotName != tt.wantName || gotDetail != tt.wantDetail {
				t.Fatalf("parseYAMLAgentPool() = %q, %q; want %q, %q", gotName, gotDetail, tt.wantName, tt.wantDetail)
			}
		})
	}
}

func TestPipelineCreationKeepsUnmatchedYAMLPoolSelected(t *testing.T) {
	model := newPipelineCreationModel(nil, styles.DefaultStyles())
	model.step = creationPool
	model, _ = model.Update(agentQueuesMsg{
		queues: []azdevops.AgentPoolQueue{{
			ID: 4, Name: "Linux", Pool: azdevops.AgentPoolReference{ID: 2, Name: "Self hosted"},
		}},
		yamlPoolDetail: "vmImage: ubuntu-latest",
	})
	if got := model.visibleChoices(); len(got) != 2 || got[model.cursor] != yamlPoolChoice {
		t.Fatalf("pool selection = %v/%d, want YAML pool choice preselected", got, model.cursor)
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.agentQueue != nil {
		t.Fatalf("agent queue = %+v, want no queue override for YAML vmImage pool", model.agentQueue)
	}
}

func TestPipelineCreationAllowsChangingYAMLMatchedQueue(t *testing.T) {
	model := newPipelineCreationModel(nil, styles.DefaultStyles())
	model.step = creationPool
	model.agentQueues = []azdevops.AgentPoolQueue{
		{ID: 4, Name: "Linux", Pool: azdevops.AgentPoolReference{ID: 2, Name: "Self hosted"}},
		{ID: 5, Name: "Windows", Pool: azdevops.AgentPoolReference{ID: 3, Name: "Windows pool"}},
	}
	model.yamlPoolName = "Self hosted"
	model.yamlPoolDetail = "name: Self hosted"
	model.yamlPoolQueue = matchingAgentQueue(model.agentQueues, model.yamlPoolName)
	model.cursor = model.yamlPoolQueue

	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyDown})
	if got := model.visibleChoices()[model.cursor]; got != "Windows (pool: Windows pool)" {
		t.Fatalf("alternative queue = %q, want Windows queue", got)
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.agentQueue == nil || model.agentQueue.ID != 5 || model.agentQueueFromYAML {
		t.Fatalf("changed queue = %+v, selected from YAML = %t", model.agentQueue, model.agentQueueFromYAML)
	}
}

func TestDefinitionsCreatePipelineSelectsBranchBeforeYAML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/git/repositories":
			_, _ = w.Write([]byte(`{"value":[
				{"id":"repo-1","name":"service","type":"TfsGit","defaultBranch":"refs/heads/main"}
			]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/git/repositories/repo-1/refs":
			_, _ = w.Write([]byte(`{"value":[
				{"name":"refs/heads/main"},
				{"name":"refs/heads/feature/new-pipeline"}
			]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/git/repositories/repo-1/items":
			if got := r.URL.Query().Get("versionDescriptor.version"); got != "feature/new-pipeline" {
				t.Errorf("YAML branch = %q, want feature/new-pipeline", got)
			}
			if r.URL.Query().Get("includeContent") == "true" {
				_, _ = w.Write([]byte(`{"content":"pool:\n  name: Self hosted\n"}`))
			} else {
				_, _ = w.Write([]byte(`{"value":[
					{"path":"/ci/pipeline.yaml","gitObjectType":"blob"}
				]}`))
			}
		case r.Method == http.MethodGet && r.URL.Path == "/distributedtask/queues":
			if r.URL.Query().Get("actionFilter") != "use" {
				t.Errorf("agent queue list did not request usable queues: %s", r.URL.String())
			}
			_, _ = w.Write([]byte(`{"value":[
				{"id":4,"name":"Linux","pool":{"id":2,"name":"Self hosted"}},
				{"id":5,"name":"Windows","pool":{"id":3,"name":"Windows pool"}}
			]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/build/definitions":
			var request struct {
				Name       string                      `json:"name"`
				Process    azdevops.PipelineProcess    `json:"process"`
				Repository azdevops.PipelineRepository `json:"repository"`
				Queue      *azdevops.AgentPoolQueue    `json:"queue"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode created pipeline request: %v", err)
			}
			if request.Name != "pipeline" || request.Process.YAMLFileName != "/ci/pipeline.yaml" ||
				request.Repository.DefaultBranch != "refs/heads/feature/new-pipeline" ||
				request.Queue == nil || request.Queue.ID != 4 || request.Queue.Pool.ID != 2 {
				t.Errorf("created pipeline request = %+v", request)
			}
			_, _ = w.Write([]byte(`{"id":8,"name":"pipeline"}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := azdevops.NewMultiClient("org", []string{"project"}, "pat", nil)
	if err != nil {
		t.Fatal(err)
	}
	client.ClientFor("project").SetBaseURL(server.URL)
	model := NewDefinitionsModel(client, styles.DefaultStyles())

	model, _ = model.Update(pipelineKey('n'))
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("selecting a project did not load repositories")
	}
	model, _ = model.Update(cmd())
	model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("selecting a repository did not load branches")
	}
	model, _ = model.Update(cmd())
	if model.creation.step != creationBranch || model.creation.visibleChoices()[model.creation.cursor] != "main" {
		t.Fatalf("initial branch selection = %v/%v, want default main", model.creation.step, model.creation.visibleChoices())
	}

	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyUp})
	if model.creation.visibleChoices()[model.creation.cursor] != "feature/new-pipeline" {
		t.Fatalf("selected branch = %q, want feature/new-pipeline", model.creation.visibleChoices()[model.creation.cursor])
	}
	model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("selecting a branch did not load its YAML files")
	}
	model, _ = model.Update(cmd())
	if got := model.creation.visibleChoices(); len(got) != 1 || got[0] != "/ci/pipeline.yaml" {
		t.Fatalf("branch YAML choices = %v", got)
	}

	model, cmd = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("selecting the YAML file did not load agent queues")
	}
	model, _ = model.Update(cmd())
	if model.creation.step != creationPool {
		t.Fatalf("step after loading agent queues = %v, want agent pool", model.creation.step)
	}
	if model.creation.cursor != 0 || model.creation.visibleChoices()[model.creation.cursor] != "Linux (pool: Self hosted)" {
		t.Fatalf("YAML-matched queue selection = %v/%v, want Linux queue selected by default", model.creation.cursor, model.creation.visibleChoices())
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model, cmd = model.Update(pipelineKey('c'))
	if cmd == nil {
		t.Fatal("creating the pipeline did not return a command")
	}
	created, ok := cmd().(pipelineCreatedMsg)
	if !ok || created.err != nil || created.definition == nil {
		t.Fatalf("create result = %+v", created)
	}
}

func TestDefinitionsSlashSearchFiltersDefinitions(t *testing.T) {
	model := NewDefinitionsModel(nil, styles.DefaultStyles())
	model.list = model.list.SetItems([]azdevops.PipelineDefinition{
		{ID: 1, Name: "Build"},
		{ID: 2, Name: "Release", Path: `\Services`},
	})

	model, _ = model.Update(pipelineKey('/'))
	if !model.IsSearching() {
		t.Fatal("pressing / should enter search mode")
	}
	for _, r := range "services" {
		model, _ = model.Update(pipelineKey(r))
	}

	rows := model.list.Table().Rows()
	if len(rows) != 1 {
		t.Fatalf("filtered rows = %d, want 1", len(rows))
	}
	if !strings.Contains(strings.Join(rows[0], " "), "Release") {
		t.Errorf("filtered row = %v, want Release pipeline", rows[0])
	}
}

func TestDefinitionsCopyPipelineKeepsVariablesAndSelectsAnotherYAML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/build/definitions/7":
			_, _ = w.Write([]byte(`{
				"id":7,
				"name":"Source",
				"path":"\\Automation",
				"process":{"type":2,"yamlFilename":"/old.yml"},
				"repository":{"id":"repo-1","defaultBranch":"refs/heads/release"},
				"queue":{"id":4,"name":"Linux","pool":{"id":2,"name":"Self hosted"}},
				"variables":{
					"Configuration":{"value":"Release","allowOverride":true},
					"Token":{"isSecret":true,"allowOverride":true}
				}
			}`))
		case r.Method == http.MethodGet && r.URL.Path == "/git/repositories":
			_, _ = w.Write([]byte(`{"value":[
				{"id":"repo-1","name":"service","type":"TfsGit","defaultBranch":"refs/heads/main"}
			]}`))
		case r.Method == http.MethodGet && r.URL.Path == "/git/repositories/repo-1/items":
			if got := r.URL.Query().Get("versionDescriptor.version"); got != "release" {
				t.Errorf("YAML file branch = %q, want source branch release", got)
			}
			_, _ = w.Write([]byte(`{"value":[
				{"path":"/old.yml","gitObjectType":"blob"},
				{"path":"/new.yml","gitObjectType":"blob"}
			]}`))
		case r.Method == http.MethodPost && r.URL.Path == "/build/definitions":
			var request struct {
				Name       string                               `json:"name"`
				Path       string                               `json:"path"`
				Process    azdevops.PipelineProcess             `json:"process"`
				Repository azdevops.PipelineRepository          `json:"repository"`
				Variables  map[string]azdevops.PipelineVariable `json:"variables"`
				Queue      *azdevops.AgentPoolQueue             `json:"queue"`
			}
			if err := json.NewDecoder(r.Body).Decode(&request); err != nil {
				t.Errorf("decode copied pipeline request: %v", err)
			}
			if request.Name != "Source copy" || request.Path != `\Automation` ||
				request.Repository.DefaultBranch != "refs/heads/release" ||
				request.Process.YAMLFileName != "/new.yml" ||
				request.Queue == nil || request.Queue.ID != 4 || request.Queue.Pool.ID != 2 {
				t.Errorf("copy request = %+v", request)
			}
			if got := request.Variables["Configuration"]; got.Value != "Release" || !got.AllowOverride {
				t.Errorf("copied Configuration variable = %+v", got)
			}
			if got := request.Variables["Token"]; got.Value != "reentered-secret" || !got.IsSecret || !got.AllowOverride {
				t.Errorf("copied Token variable = %+v", got)
			}
			_, _ = w.Write([]byte(`{"id":8,"name":"Source copy"}`))
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	client, err := azdevops.NewMultiClient("org", []string{"project"}, "pat", nil)
	if err != nil {
		t.Fatal(err)
	}
	client.ClientFor("project").SetBaseURL(server.URL)
	model := NewDefinitionsModel(client, styles.DefaultStyles())
	model.list = model.list.SetItems([]azdevops.PipelineDefinition{{
		ID:          7,
		Name:        "Source",
		ProjectName: "project",
	}})

	model, cmd := model.Update(pipelineKey('c'))
	if model.creation == nil || cmd == nil {
		t.Fatal("copy shortcut did not start loading the selected pipeline")
	}
	model, _ = model.Update(cmd())
	if model.creation.loading {
		t.Fatal("copy wizard remained in loading state")
	}
	if got := model.creation.visibleChoices(); len(got) != 1 || got[0] != "/new.yml" {
		t.Fatalf("copy YAML choices = %v, want only /new.yml", got)
	}

	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.creation.step != creationName {
		t.Fatalf("step after selecting YAML = %v, want name", model.creation.step)
	}
	model.creation.input.SetValue("Source copy")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.creation.step != creationVariableValue || model.creation.pendingName != "Token" {
		t.Fatalf("step/pending secret = %v/%q, want secret value/Token", model.creation.step, model.creation.pendingName)
	}
	model.creation.input.SetValue("reentered-secret")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.creation.step != creationReview {
		t.Fatalf("step after secret entry = %v, want review", model.creation.step)
	}
	review := model.creation.View()
	if strings.Contains(review, "reentered-secret") || !strings.Contains(review, "Configuration = Release") {
		t.Fatalf("copy review leaked or omitted variables: %q", review)
	}

	model, cmd = model.Update(pipelineKey('c'))
	if cmd == nil {
		t.Fatal("copy review did not start pipeline creation")
	}
	created, ok := cmd().(pipelineCreatedMsg)
	if !ok || created.err != nil || created.definition == nil || !created.copied {
		t.Fatalf("copy result = %+v", created)
	}
}

func TestDefinitionDetailEditsBranchAndQueueVariable(t *testing.T) {
	model := NewDefinitionsModel(nil, styles.DefaultStyles())
	model.list = model.list.SetItems([]azdevops.PipelineDefinition{
		{ID: 7, Name: "Build", ProjectName: "project", ProjectDisplayName: "Project"},
	})

	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.list.GetViewMode() != listview.ViewDetail {
		t.Fatal("enter should open the selected pipeline definition")
	}
	model, _ = model.Update(definitionLoadedMsg{definition: &azdevops.PipelineDefinition{
		ID:   7,
		Name: "Build",
		Repository: azdevops.PipelineRepository{
			DefaultBranch: "refs/heads/main",
		},
		Variables: map[string]azdevops.PipelineVariable{
			"Configuration": {Value: "Release", AllowOverride: true},
			"Secret":        {IsSecret: true, AllowOverride: true},
		},
	}})

	detail := model.list.Detail().(*definitionDetailAdapter).model
	if detail.branch != "main" || len(detail.variableNames) != 2 {
		t.Fatalf("loaded branch/variables = %q / %v", detail.branch, detail.variableNames)
	}
	if got := detail.variableDisplayValue("Secret"); got != "<secret>" {
		t.Errorf("secret variable display = %q, want masked", got)
	}

	// Escape cancels an editor without leaving the pipeline detail view.
	model, _ = model.Update(pipelineKey('e'))
	if !model.IsSearching() {
		t.Fatal("editing the branch should capture input")
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.IsSearching() || model.list.GetViewMode() != listview.ViewDetail {
		t.Fatal("escape should cancel editing and stay in the definition detail")
	}

	detail = model.list.Detail().(*definitionDetailAdapter).model
	detail.selectedIndex = 0
	model, _ = model.Update(pipelineKey('e'))
	detail = model.list.Detail().(*definitionDetailAdapter).model
	detail.input.SetValue("release/2026")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	detail = model.list.Detail().(*definitionDetailAdapter).model
	if detail.branch != "release/2026" {
		t.Errorf("edited branch = %q, want release/2026", detail.branch)
	}

	// Select and override Configuration.
	model, _ = model.Update(pipelineKey('j'))
	model, _ = model.Update(pipelineKey('e'))
	detail = model.list.Detail().(*definitionDetailAdapter).model
	detail.input.SetValue("Debug")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	detail = model.list.Detail().(*definitionDetailAdapter).model
	if got := detail.overrides["Configuration"].Value; got != "Debug" {
		t.Errorf("Configuration override = %q, want Debug", got)
	}

	// New queue-time variables use the same editor, are included in the
	// overrides sent when the run is queued, and are identified as per-run.
	model, _ = model.Update(pipelineKey('a'))
	detail = model.list.Detail().(*definitionDetailAdapter).model
	detail.input.SetValue("Environment")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	detail = model.list.Detail().(*definitionDetailAdapter).model
	detail.input.SetValue("Staging")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	detail = model.list.Detail().(*definitionDetailAdapter).model
	if got := detail.overrides["Environment"].Value; got != "Staging" {
		t.Errorf("Environment override = %q, want Staging", got)
	}
	if want := `Added queue-time variable "Environment" for this run`; detail.statusMessage != want {
		t.Errorf("new variable status = %q, want %q", detail.statusMessage, want)
	}
}

func TestDefinitionDetailRenamesPipelineAndUpdatesListRow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/build/definitions/7" || r.URL.Query().Get("api-version") != "7.1" {
			t.Errorf("unexpected definition endpoint %s", r.URL.String())
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{
				"id":7,"name":"Build","path":"\\Automation","revision":3,
				"process":{"type":2,"yamlFilename":"/ci.yml"},
				"repository":{"id":"repo-1","defaultBranch":"refs/heads/main"},
				"variables":{"Mode":{"value":"Release"}}
			}`))
		case http.MethodPut:
			var payload struct {
				Name      string                               `json:"name"`
				Revision  int                                  `json:"revision"`
				Variables map[string]azdevops.PipelineVariable `json:"variables"`
			}
			var raw map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
				t.Errorf("decode rename request: %v", err)
			}
			if err := json.Unmarshal(raw["name"], &payload.Name); err != nil {
				t.Errorf("decode renamed name: %v", err)
			}
			if err := json.Unmarshal(raw["revision"], &payload.Revision); err != nil {
				t.Errorf("decode preserved revision: %v", err)
			}
			if err := json.Unmarshal(raw["variables"], &payload.Variables); err != nil {
				t.Errorf("decode preserved variables: %v", err)
			}
			if payload.Name != "Build Renamed" || payload.Revision != 3 || payload.Variables["Mode"].Value != "Release" {
				t.Errorf("rename request did not preserve definition: %+v", payload)
			}
			_, _ = w.Write([]byte(`{"id":7,"name":"Build Renamed","path":"\\Automation"}`))
		default:
			t.Errorf("unexpected request method %s", r.Method)
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	client, err := azdevops.NewMultiClient("org", []string{"project"}, "pat", nil)
	if err != nil {
		t.Fatal(err)
	}
	client.ClientFor("project").SetBaseURL(server.URL)
	model := NewDefinitionsModel(client, styles.DefaultStyles())
	model.list = model.list.SetItems([]azdevops.PipelineDefinition{{
		ID: 7, Name: "Build", ProjectName: "project",
	}})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = model.Update(definitionLoadedMsg{definition: &azdevops.PipelineDefinition{
		ID: 7, Name: "Build", ProjectName: "project",
		Process:    azdevops.PipelineProcess{Type: 2, YAMLFileName: "/ci.yml"},
		Repository: azdevops.PipelineRepository{ID: "repo-1", DefaultBranch: "refs/heads/main"},
		Variables:  map[string]azdevops.PipelineVariable{"Mode": {Value: "Release"}},
	}})

	hasRenameAction := false
	for _, item := range model.GetContextItems() {
		if item.Key == "N" && item.Description == "rename pipeline" {
			hasRenameAction = true
		}
	}
	if !hasRenameAction {
		t.Fatal("pipeline detail context does not show the rename action")
	}
	hasPersistentVariableAction := false
	for _, item := range model.GetContextItems() {
		if item.Key == "A" && strings.Contains(item.Description, "save variable") {
			hasPersistentVariableAction = true
		}
	}
	if !hasPersistentVariableAction {
		t.Fatal("pipeline detail context does not show the persistent variable action")
	}
	model, _ = model.Update(pipelineKey('N'))
	detail := model.list.Detail().(*definitionDetailAdapter).model
	if detail.editor != editorPipelineName || detail.input.Value() != "Build" {
		t.Fatalf("rename editor = %v/%q, want pipeline-name editor with current name", detail.editor, detail.input.Value())
	}
	detail.input.SetValue("Build Renamed")
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("saving the new pipeline name did not start an update")
	}
	model, _ = model.Update(cmd())
	detail = model.list.Detail().(*definitionDetailAdapter).model
	if detail.selected.Name != "Build Renamed" || !strings.Contains(detail.statusMessage, "Renamed pipeline") {
		t.Fatalf("detail after rename = name %q, status %q", detail.selected.Name, detail.statusMessage)
	}
	if got := model.list.Items()[0].Name; got != "Build Renamed" {
		t.Fatalf("pipeline list name = %q, want Build Renamed", got)
	}
}

func TestDefinitionDetailSavesPersistentVariable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/build/definitions/7" || r.URL.Query().Get("api-version") != "7.1" {
			t.Errorf("unexpected definition endpoint %s", r.URL.String())
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{
				"id":7,"name":"Build","path":"\\Automation",
				"variables":{"Configuration":{"value":"Release","allowOverride":true}}
			}`))
		case http.MethodPut:
			var payload struct {
				Variables map[string]azdevops.PipelineVariable `json:"variables"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode persistent variable request: %v", err)
			}
			if got := payload.Variables["KeepForever"]; got.Value != "stable" || got.IsSecret || !got.AllowOverride {
				t.Errorf("persistent variable request = %+v", got)
			}
			_, _ = w.Write([]byte(`{
				"id":7,"name":"Build","path":"\\Automation",
				"variables":{
					"Configuration":{"value":"Release","allowOverride":true},
					"KeepForever":{"value":"stable","allowOverride":true}
				}
			}`))
		default:
			t.Errorf("unexpected request method %s", r.Method)
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	client, err := azdevops.NewMultiClient("org", []string{"project"}, "pat", nil)
	if err != nil {
		t.Fatal(err)
	}
	client.ClientFor("project").SetBaseURL(server.URL)
	model := NewDefinitionsModel(client, styles.DefaultStyles())
	model.list = model.list.SetItems([]azdevops.PipelineDefinition{{
		ID: 7, Name: "Build", ProjectName: "project",
	}})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = model.Update(definitionLoadedMsg{definition: &azdevops.PipelineDefinition{
		ID: 7, Name: "Build", ProjectName: "project",
		Variables: map[string]azdevops.PipelineVariable{
			"Configuration": {Value: "Release", AllowOverride: true},
		},
	}})

	model, _ = model.Update(pipelineKey('A'))
	detail := model.list.Detail().(*definitionDetailAdapter).model
	if detail.editor != editorVariableName {
		t.Fatalf("persistent add starts in editor %v, want variable name", detail.editor)
	}
	detail.input.SetValue("KeepForever")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	detail = model.list.Detail().(*definitionDetailAdapter).model
	if detail.editor != editorVariableSecret {
		t.Fatalf("persistent add next editor = %v, want secret choice", detail.editor)
	}
	detail.input.SetValue("n")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	detail = model.list.Detail().(*definitionDetailAdapter).model
	detail.input.SetValue("stable")
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("saving a persistent variable did not issue an Azure DevOps update")
	}
	model, _ = model.Update(cmd())
	detail = model.list.Detail().(*definitionDetailAdapter).model
	if got := detail.variables["KeepForever"]; got.Value != "stable" || !got.AllowOverride {
		t.Fatalf("saved variable = %+v", got)
	}
	if !strings.Contains(detail.statusMessage, `Saved variable "KeepForever"`) {
		t.Fatalf("save status = %q", detail.statusMessage)
	}
}

func TestDefinitionDetailSavesPersistentSecretVariable(t *testing.T) {
	const secretValue = "do-not-display-this-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/build/definitions/7" || r.URL.Query().Get("api-version") != "7.1" {
			t.Errorf("unexpected definition endpoint %s", r.URL.String())
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{"id":7,"name":"Build","variables":{}}`))
		case http.MethodPut:
			var payload struct {
				Variables map[string]azdevops.PipelineVariable `json:"variables"`
			}
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode persistent secret variable request: %v", err)
			}
			if got := payload.Variables["DeployKey"]; got.Value != secretValue || !got.IsSecret || !got.AllowOverride {
				t.Errorf("persistent secret variable request = %+v", got)
			}
			_, _ = w.Write([]byte(`{
				"id":7,"name":"Build",
				"variables":{"DeployKey":{"isSecret":true,"allowOverride":true}}
			}`))
		default:
			t.Errorf("unexpected request method %s", r.Method)
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	client, err := azdevops.NewMultiClient("org", []string{"project"}, "pat", nil)
	if err != nil {
		t.Fatal(err)
	}
	client.ClientFor("project").SetBaseURL(server.URL)
	model := NewDefinitionsModel(client, styles.DefaultStyles())
	model.list = model.list.SetItems([]azdevops.PipelineDefinition{{
		ID: 7, Name: "Build", ProjectName: "project",
	}})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = model.Update(definitionLoadedMsg{definition: &azdevops.PipelineDefinition{
		ID: 7, Name: "Build", ProjectName: "project",
	}})

	model, _ = model.Update(pipelineKey('A'))
	detail := model.list.Detail().(*definitionDetailAdapter).model
	detail.input.SetValue("DeployKey")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	detail = model.list.Detail().(*definitionDetailAdapter).model
	if detail.editor != editorVariableSecret {
		t.Fatalf("persistent add editor = %v, want secret choice", detail.editor)
	}
	detail.input.SetValue("y")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	detail = model.list.Detail().(*definitionDetailAdapter).model
	detail.input.SetValue(secretValue)
	if strings.Contains(detail.View(), secretValue) {
		t.Fatal("secret value was rendered while entering the variable")
	}

	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("saving a persistent secret variable did not issue an Azure DevOps update")
	}
	model, _ = model.Update(cmd())
	detail = model.list.Detail().(*definitionDetailAdapter).model
	if got := detail.variables["DeployKey"]; !got.IsSecret || !got.AllowOverride {
		t.Fatalf("saved secret variable = %+v", got)
	}
	if strings.Contains(detail.View(), secretValue) {
		t.Fatal("secret value was rendered after saving the variable")
	}
}

func TestDefinitionDetailMovesPipelineAndUpdatesListRow(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/build/definitions/7" || r.URL.Query().Get("api-version") != "7.1" {
			t.Errorf("unexpected definition endpoint %s", r.URL.String())
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{
				"id":7,"name":"Build","path":"\\Automation","revision":3,
				"process":{"type":2,"yamlFilename":"/ci.yml"},
				"repository":{"id":"repo-1","defaultBranch":"refs/heads/main"},
				"variables":{"Mode":{"value":"Release"}}
			}`))
		case http.MethodPut:
			var payload map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode move request: %v", err)
			}
			var name, path string
			if err := json.Unmarshal(payload["name"], &name); err != nil {
				t.Errorf("decode preserved name: %v", err)
			}
			if err := json.Unmarshal(payload["path"], &path); err != nil {
				t.Errorf("decode moved path: %v", err)
			}
			if name != "Build" || path != `\Deploy\Backend` || len(payload["variables"]) == 0 {
				t.Errorf("move request did not preserve definition: %+v", payload)
			}
			_, _ = w.Write([]byte(`{"id":7,"name":"Build","path":"\\Deploy\\Backend"}`))
		default:
			t.Errorf("unexpected request method %s", r.Method)
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	client, err := azdevops.NewMultiClient("org", []string{"project"}, "pat", nil)
	if err != nil {
		t.Fatal(err)
	}
	client.ClientFor("project").SetBaseURL(server.URL)
	model := NewDefinitionsModel(client, styles.DefaultStyles())
	model.list = model.list.SetItems([]azdevops.PipelineDefinition{{
		ID: 7, Name: "Build", Path: `\Automation`, ProjectName: "project",
	}})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = model.Update(definitionLoadedMsg{definition: &azdevops.PipelineDefinition{
		ID: 7, Name: "Build", Path: `\Automation`, ProjectName: "project",
		Process:    azdevops.PipelineProcess{Type: 2, YAMLFileName: "/ci.yml"},
		Repository: azdevops.PipelineRepository{ID: "repo-1", DefaultBranch: "refs/heads/main"},
		Variables:  map[string]azdevops.PipelineVariable{"Mode": {Value: "Release"}},
	}})

	hasMoveAction := false
	for _, item := range model.GetContextItems() {
		if item.Key == "M" && item.Description == "move to folder" {
			hasMoveAction = true
		}
	}
	if !hasMoveAction {
		t.Fatal("pipeline detail context does not show the move action")
	}
	model, _ = model.Update(pipelineKey('M'))
	detail := model.list.Detail().(*definitionDetailAdapter).model
	if detail.editor != editorPipelinePath || detail.input.Value() != `\Automation` {
		t.Fatalf("move editor = %v/%q, want folder-path editor with current path", detail.editor, detail.input.Value())
	}
	detail.input.SetValue("Deploy/Backend")
	model, cmd := model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if cmd == nil {
		t.Fatal("saving the new pipeline folder did not start an update")
	}
	model, _ = model.Update(cmd())
	detail = model.list.Detail().(*definitionDetailAdapter).model
	if detail.selected.Path != `\Deploy\Backend` || !strings.Contains(detail.statusMessage, "Moved pipeline") {
		t.Fatalf("detail after move = path %q, status %q", detail.selected.Path, detail.statusMessage)
	}
	if got := model.list.Items()[0].Path; got != `\Deploy\Backend` {
		t.Fatalf("pipeline list path = %q, want \\Deploy\\Backend", got)
	}
}

func TestDefinitionDetailOpensPagedRunHistoryAndReturnsToDefinition(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query := r.URL.Query()
		if query.Get("definitions") != "7" || query.Get("$top") != "100" {
			t.Errorf("unexpected definition history query: %s", r.URL.String())
		}
		switch query.Get("continuationToken") {
		case "":
			w.Header().Set("x-ms-continuationtoken", "older-page")
			_, _ = w.Write([]byte(`{"count":1,"value":[{"id":123,"buildNumber":"2026.10.1","status":"completed","result":"succeeded","queueTime":"2026-10-19T10:00:00Z","definition":{"id":7,"name":"Build"},"project":{"name":"project"}}]}`))
		case "older-page":
			_, _ = w.Write([]byte(`{"count":1,"value":[{"id":122,"buildNumber":"2026.10.0","status":"completed","result":"failed","queueTime":"2026-10-18T10:00:00Z","definition":{"id":7,"name":"Build"},"project":{"name":"project"}}]}`))
		default:
			t.Errorf("unexpected continuation token %q", query.Get("continuationToken"))
			http.Error(w, "unexpected continuation token", http.StatusBadRequest)
		}
	}))
	defer server.Close()

	client, err := azdevops.NewMultiClient("org", []string{"project"}, "test-pat", nil)
	if err != nil {
		t.Fatal(err)
	}
	client.ClientFor("project").SetBaseURL(server.URL)
	model := NewDefinitionsModel(client, styles.DefaultStyles())
	model.list = model.list.SetItems([]azdevops.PipelineDefinition{{
		ID:          7,
		Name:        "Build",
		ProjectName: "project",
	}})
	model, _ = model.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = model.Update(definitionLoadedMsg{
		definition: &azdevops.PipelineDefinition{ID: 7, Name: "Build"},
	})

	var cmd tea.Cmd
	model, cmd = model.Update(pipelineKey('v'))
	if model.history == nil || cmd == nil {
		t.Fatal("v should open run history and start fetching runs")
	}
	model, _ = model.Update(cmd())
	if got := len(model.history.model.list.Items()); got != 1 {
		t.Fatalf("initial history page contains %d runs, want 1", got)
	}
	if model.history.continuationToken != "older-page" {
		t.Fatalf("history continuation token = %q, want older-page", model.history.continuationToken)
	}

	model, cmd = model.Update(pipelineKey('o'))
	if cmd == nil {
		t.Fatal("o should request the next history page")
	}
	model, _ = model.Update(cmd())
	if got := len(model.history.model.list.Items()); got != 2 {
		t.Fatalf("history after loading older page contains %d runs, want 2", got)
	}
	if model.history.continuationToken != "" {
		t.Fatalf("continuation token after final page = %q, want empty", model.history.continuationToken)
	}

	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.history.model.GetViewMode() != ViewDetail {
		t.Fatal("enter should open the selected historical run")
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.history == nil || model.history.model.GetViewMode() != ViewList {
		t.Fatal("first escape should return from run detail to history")
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if model.history != nil || model.list.GetViewMode() != listview.ViewDetail {
		t.Fatal("second escape should return to the selected pipeline definition")
	}
}

func TestDefinitionDetailSupportsUppercaseHistoryShortcut(t *testing.T) {
	model := NewDefinitionsModel(nil, styles.DefaultStyles())
	model.list = model.list.SetItems([]azdevops.PipelineDefinition{{ID: 7, Name: "Build"}})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})

	model, _ = model.Update(pipelineKey('H'))
	if model.history == nil {
		t.Fatal("H should open run history")
	}
}

func TestDefinitionsListOpensSelectedPipelineHistory(t *testing.T) {
	model := NewDefinitionsModel(nil, styles.DefaultStyles())
	model.list = model.list.SetItems([]azdevops.PipelineDefinition{
		{ID: 7, Name: "Build"},
	})

	model, cmd := model.Update(pipelineKey('v'))
	if model.history == nil || cmd == nil {
		t.Fatal("v should open run history for the selected pipeline")
	}
}

func TestDefinitionHistoryShortcutWorksAfterSearchingAndOpeningPipeline(t *testing.T) {
	model := NewDefinitionsModel(nil, styles.DefaultStyles())
	model.list = model.list.SetItems([]azdevops.PipelineDefinition{
		{ID: 7, Name: "Build"},
		{ID: 8, Name: "Release"},
	})

	model, _ = model.Update(pipelineKey('/'))
	for _, r := range "build" {
		model, _ = model.Update(pipelineKey(r))
	}
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if model.list.GetViewMode() != listview.ViewDetail {
		t.Fatal("enter should open the filtered pipeline definition")
	}
	if !model.list.IsSearching() {
		t.Fatal("precondition failed: list search state should remain set in detail view")
	}

	model, cmd := model.Update(pipelineKey('v'))
	if model.history == nil || cmd == nil {
		t.Fatal("v should open run history after selecting a pipeline through search")
	}
}

func TestRuntimeParameterOverridesAreTypedAndValidated(t *testing.T) {
	parameters, err := parseRuntimeParameters(`parameters:
- name: environment
  type: string
  default: dev
  values: [dev, prod]
- name: deploy
  type: boolean
  default: false
- name: retries
  type: number
  default: 2
`)
	if err != nil {
		t.Fatalf("parseRuntimeParameters() error = %v", err)
	}
	if len(parameters) != 3 || parameters[0].Name != "environment" {
		t.Fatalf("parameters = %+v", parameters)
	}
	value, err := parseParameterOverride("prod", parameters[0])
	if err != nil || value != "prod" {
		t.Fatalf("string override = %#v, %v", value, err)
	}
	if _, err := parseParameterOverride("stage", parameters[0]); err == nil {
		t.Fatal("expected disallowed value to be rejected")
	}
	value, err = parseParameterOverride("true", parameters[1])
	if err != nil || value != true {
		t.Fatalf("boolean override = %#v, %v", value, err)
	}
	if _, err := parseParameterOverride("not-bool", parameters[1]); err == nil {
		t.Fatal("expected invalid boolean to be rejected")
	}
	value, err = parseParameterOverride("3", parameters[2])
	if err != nil || value != 3 {
		t.Fatalf("number override = %#v, %v", value, err)
	}
}

func TestParseYAMLVariableNames(t *testing.T) {
	contents := `variables:
  configuration: Release
  Image: ubuntu-latest
`
	names, err := parseYAMLVariableNames(contents)
	if err != nil {
		t.Fatalf("parseYAMLVariableNames() error = %v", err)
	}
	if want := []string{"configuration", "Image"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("variable names = %v, want %v", names, want)
	}

	contents = `variables:
- name: environment
  value: staging
- group: shared-secrets
- template: variables.yml
`
	names, err = parseYAMLVariableNames(contents)
	if err != nil {
		t.Fatalf("parseYAMLVariableNames() error = %v", err)
	}
	if want := []string{"environment"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("list variable names = %v, want %v", names, want)
	}
}

func TestDefinitionDetailDisplaysYAMLVariableNames(t *testing.T) {
	definition := azdevops.PipelineDefinition{
		ID:   7,
		Name: "Build",
		Process: azdevops.PipelineProcess{
			Type: 2,
		},
	}
	detail := newDefinitionDetailModel(nil, definition, styles.DefaultStyles())
	detail, _ = detail.Update(definitionLoadedMsg{
		definition:        &definition,
		yamlVariableNames: []string{"configuration"},
	})
	view := detail.View()
	if !strings.Contains(view, "Variables defined in YAML (names only)") ||
		!strings.Contains(view, "configuration") {
		t.Fatalf("pipeline details did not show YAML variable name: %s", view)
	}
}

func TestDefinitionDetailCopiesCompatiblePreviousRunParameters(t *testing.T) {
	parameters, err := parseRuntimeParameters(`parameters:
- name: environment
  type: string
  default: dev
  values: [dev, prod]
- name: retries
  type: number
  default: 1
`)
	if err != nil {
		t.Fatalf("parseRuntimeParameters() error = %v", err)
	}
	detail := newDefinitionDetailModel(nil, azdevops.PipelineDefinition{ID: 12, Name: "Deploy"}, styles.DefaultStyles())
	detail.previousRunParamsLoad = true
	detail, _ = detail.Update(definitionLoadedMsg{
		definition: &azdevops.PipelineDefinition{ID: 12, Name: "Deploy"},
		parameters: parameters,
	})
	if detail.statusMessage != "Loading previous run parameters..." {
		t.Fatalf("status while previous values load = %q", detail.statusMessage)
	}

	detail, _ = detail.Update(previousRunParametersLoadedMsg{parameters: map[string]any{
		"environment": "prod",
		"retries":     float64(3),
		"removed":     "old",
	}})
	if detail.parameterOverrides["environment"] != "prod" || detail.parameterOverrides["retries"] != 3 {
		t.Fatalf("copied overrides = %#v", detail.parameterOverrides)
	}
	queuedParameters := pipelineParameterMap(detail.parameters, detail.parameterOverrides)
	if queuedParameters["environment"] != "prod" || queuedParameters["retries"] != 3 {
		t.Fatalf("queued parameters = %#v", queuedParameters)
	}
	if !strings.Contains(detail.statusMessage, "Copied previous parameters: environment, retries") ||
		!strings.Contains(detail.statusMessage, "skipped incompatible parameters: removed") {
		t.Fatalf("copy status does not report copied and skipped values: %q", detail.statusMessage)
	}
}

func TestDefinitionDetailEditsAndDisplaysYAMLParameter(t *testing.T) {
	model := NewDefinitionsModel(nil, styles.DefaultStyles())
	model.list = model.list.SetItems([]azdevops.PipelineDefinition{
		{ID: 7, Name: "Build", ProjectName: "project"},
	})
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	model, _ = model.Update(definitionLoadedMsg{
		definition: &azdevops.PipelineDefinition{
			ID:   7,
			Name: "Build",
			Process: azdevops.PipelineProcess{
				Type: 2, YAMLFileName: "/azure-pipelines.yml",
			},
		},
		parameters: []runtimeParameter{{
			Name: "deploy", Type: "boolean", Default: false,
		}},
	})
	detail := model.list.Detail().(*definitionDetailAdapter).model
	if !strings.Contains(detail.View(), "YAML Parameters") || !strings.Contains(detail.View(), "deploy") {
		t.Fatalf("parameter view missing from detail:\n%s", detail.View())
	}

	model, _ = model.Update(pipelineKey('j'))
	model, _ = model.Update(pipelineKey('e'))
	detail = model.list.Detail().(*definitionDetailAdapter).model
	detail.input.SetValue("true")
	model, _ = model.Update(tea.KeyMsg{Type: tea.KeyEnter})
	detail = model.list.Detail().(*definitionDetailAdapter).model
	if got := detail.parameterOverrides["deploy"]; got != true {
		t.Fatalf("deploy override = %#v, want true", got)
	}
	if got := pipelineParameterMap(detail.parameters, detail.parameterOverrides)["deploy"]; got != true {
		t.Fatalf("queued parameter = %#v, want true", got)
	}
}

func TestDefinitionDetailCanEditMultipleYAMLParametersInSequence(t *testing.T) {
	detail := newDefinitionDetailModel(nil, azdevops.PipelineDefinition{
		ID: 7, Name: "Deploy",
		Process: azdevops.PipelineProcess{Type: 2, YAMLFileName: "/azure-pipelines.yml"},
	}, styles.DefaultStyles())
	detail.loaded = true
	detail.newRunMode = true
	detail.SetSize(80, 30)
	detail.parameterOverrides = make(map[string]any)
	detail.parameters = []runtimeParameter{
		{Name: "environment", Type: "string", Default: "dev"},
		{Name: "deploy", Type: "boolean", Default: false},
		{Name: "retries", Type: "number", Default: 2},
	}
	detail.selectedIndex = 1
	view := detail.View()
	for _, parameter := range []string{"environment (string)", "deploy (boolean)", "retries (number)"} {
		if !strings.Contains(view, parameter) {
			t.Fatalf("new-run parameter list is missing %q:\n%s", parameter, view)
		}
	}

	detail, _ = detail.Update(pipelineKey('e'))
	detail.input.SetValue("prod")
	detail, _ = detail.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if detail.parameterOverrides["environment"] != "prod" || detail.selectedIndex != 2 {
		t.Fatalf("first edit/next selection = %#v/%d, want prod/second parameter", detail.parameterOverrides, detail.selectedIndex)
	}

	detail, _ = detail.Update(pipelineKey('e'))
	detail.input.SetValue("true")
	detail, _ = detail.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if detail.parameterOverrides["deploy"] != true || detail.selectedIndex != 3 {
		t.Fatalf("second edit/next selection = %#v/%d, want true/third parameter", detail.parameterOverrides, detail.selectedIndex)
	}

	queued := pipelineParameterMap(detail.parameters, detail.parameterOverrides)
	if queued["environment"] != "prod" || queued["deploy"] != true {
		t.Fatalf("queued parameters = %#v, want both edits", queued)
	}
	if _, exists := queued["retries"]; exists {
		t.Fatalf("queued parameters unexpectedly overrides untouched default: %#v", queued)
	}
}

func TestNewRunParameterListScrollsToSelectedParameter(t *testing.T) {
	detail := newDefinitionDetailModel(nil, azdevops.PipelineDefinition{ID: 7, Name: "Deploy"}, styles.DefaultStyles())
	detail.loaded = true
	detail.newRunMode = true
	detail.SetSize(80, 12)
	detail.parameters = make([]runtimeParameter, 12)
	for i := range detail.parameters {
		detail.parameters[i] = runtimeParameter{
			Name: fmt.Sprintf("parameter-%02d", i+1), Type: "string", Default: "default",
		}
	}
	detail.selectedIndex = len(detail.parameters)

	view := detail.View()
	if !strings.Contains(view, "parameter-12 (string)") {
		t.Fatalf("selected last parameter is not visible:\n%s", view)
	}
	if !strings.Contains(view, "more above") {
		t.Fatalf("scrolled parameter list does not indicate hidden earlier entries:\n%s", view)
	}
}

func TestNewRunCanClearSelectedVariableWithOneKey(t *testing.T) {
	detail := newDefinitionDetailModel(nil, azdevops.PipelineDefinition{ID: 7, Name: "Deploy"}, styles.DefaultStyles())
	detail.loaded = true
	detail.newRunMode = true
	detail.variables = map[string]azdevops.PipelineVariable{
		"PackageTag": {Value: "v1.2.3", AllowOverride: true},
	}
	detail.overrides = make(map[string]azdevops.PipelineVariable)
	detail.refreshVariableNames()
	detail.selectedIndex = detail.variableStartIndex()

	detail, _ = detail.Update(pipelineKey('x'))
	override, exists := detail.overrides["PackageTag"]
	if !exists || override.Value != "" {
		t.Fatalf("cleared variable override = %+v, exists = %t; want explicit empty override", override, exists)
	}
	if !strings.Contains(detail.View(), "PackageTag") || !strings.Contains(detail.View(), "<empty>") {
		t.Fatalf("run editor does not show cleared variable:\n%s", detail.View())
	}
	foundClearAction := false
	for _, item := range detail.GetContextItems() {
		if item.Key == "x" && item.Description == "clear variable value" {
			foundClearAction = true
		}
	}
	if !foundClearAction {
		t.Fatal("run editor should advertise x for the selected variable")
	}
}

func TestNewRunCannotClearNonOverridableVariable(t *testing.T) {
	detail := newDefinitionDetailModel(nil, azdevops.PipelineDefinition{ID: 7, Name: "Deploy"}, styles.DefaultStyles())
	detail.loaded = true
	detail.newRunMode = true
	detail.variables = map[string]azdevops.PipelineVariable{
		"ReadOnly": {Value: "fixed"},
	}
	detail.overrides = make(map[string]azdevops.PipelineVariable)
	detail.refreshVariableNames()
	detail.selectedIndex = detail.variableStartIndex()

	detail, _ = detail.Update(pipelineKey('x'))
	if _, exists := detail.overrides["ReadOnly"]; exists {
		t.Fatal("clearing a non-overridable variable should not create an override")
	}
	if detail.statusMessage != `Variable "ReadOnly" does not allow queue-time overrides` {
		t.Fatalf("status = %q, want rejection message", detail.statusMessage)
	}
}

func TestRunNewCanViewAndCopyDetailedQueueError(t *testing.T) {
	originalWriteClipboard := writeClipboard
	defer func() { writeClipboard = originalWriteClipboard }()
	var copied string
	writeClipboard = func(value string) error {
		copied = value
		return nil
	}

	details := "failed to queue YAML pipeline: bad request (HTTP 400): Value cannot be null. (Parameter 'runParameters')"
	model := newDefinitionDetailModel(nil, azdevops.PipelineDefinition{ID: 7, Name: "Deploy"}, styles.DefaultStyles())
	model.newRunMode = true
	model.lastError = details
	model.SetSize(60, 16)

	model, _ = model.Update(pipelineKey('D'))
	if !model.showErrorDetails || !strings.Contains(model.View(), "runParameters") {
		t.Fatalf("detailed error view does not show the queue error:\n%s", model.View())
	}
	foundCopy := false
	for _, item := range model.GetContextItems() {
		if item.Key == "c" && strings.Contains(item.Description, "copy") {
			foundCopy = true
		}
	}
	if !foundCopy {
		t.Fatal("detailed error view should advertise copying the error")
	}

	model, cmd := model.Update(pipelineKey('c'))
	if cmd == nil {
		t.Fatal("pressing c should issue a clipboard command")
	}
	result := cmd()
	model, _ = model.Update(result)
	if copied != details {
		t.Fatalf("copied error = %q, want %q", copied, details)
	}
	if model.statusMessage != "Pipeline error copied to clipboard" {
		t.Fatalf("copy status = %q, want copied confirmation", model.statusMessage)
	}
}

func TestNewRunCanResetCopiedParameterOverride(t *testing.T) {
	detail := newDefinitionDetailModel(nil, azdevops.PipelineDefinition{ID: 7, Name: "Deploy"}, styles.DefaultStyles())
	detail.loaded = true
	detail.newRunMode = true
	detail.parameters = []runtimeParameter{{Name: "environment", Type: "string", Default: "dev"}}
	detail.parameterOverrides = map[string]any{"environment": "prod"}
	detail.selectedIndex = 1

	foundReset := false
	for _, item := range detail.GetContextItems() {
		if item.Key == "d" && strings.Contains(item.Description, "reset") {
			foundReset = true
		}
	}
	if !foundReset {
		t.Fatal("Run New should advertise d when the selected row has an override")
	}

	detail, _ = detail.Update(pipelineKey('d'))
	if _, exists := detail.parameterOverrides["environment"]; exists {
		t.Fatal("d should remove the copied parameter override so the YAML default is used")
	}
	if !strings.Contains(detail.statusMessage, `Reset parameter "environment"`) {
		t.Fatalf("reset status = %q", detail.statusMessage)
	}
}

func TestDefinitionDetailZPShowsFocusedParameterList(t *testing.T) {
	detail := newDefinitionDetailModel(nil, azdevops.PipelineDefinition{
		ID: 12, Name: "Deploy",
		Process: azdevops.PipelineProcess{Type: 2, YAMLFileName: "/azure-pipelines.yml"},
	}, styles.DefaultStyles())
	detail.loaded = true
	detail.parameters = []runtimeParameter{{
		Name: "environment", Type: "string", Default: "dev", Values: []any{"dev", "prod"},
	}}

	detail, cmd := detail.Update(pipelineKey('z'))
	if cmd == nil || !detail.parameterPrefixPending {
		t.Fatal("z should start the zP key sequence and timeout")
	}
	detail, _ = detail.Update(pipelineKey('p'))
	if !detail.showParameterList {
		t.Fatal("zP should open the parameter list")
	}
	view := detail.View()
	if !strings.Contains(view, "YAML Parameters — Deploy") ||
		!strings.Contains(view, "environment (string)") ||
		!strings.Contains(view, "dev") || !strings.Contains(view, "prod") {
		t.Fatalf("focused parameter list missing content:\n%s", view)
	}
	detail, _ = detail.Update(pipelineKey('z'))
	detail, _ = detail.Update(pipelineKey('P'))
	if detail.showParameterList {
		t.Fatal("zP should close the parameter list")
	}
	detail.showParameterList = true
	detail, _ = detail.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if detail.showParameterList {
		t.Fatal("escape should close the focused parameter list")
	}
}

func TestDefinitionDetailLoadsAndShowsYAMLParametersFromRepository(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/build/definitions/12":
			_, _ = w.Write([]byte(`{
				"id":12,
				"name":"Deploy",
				"process":{"type":2,"yamlFilename":"/azure-pipelines.yml"},
				"repository":{"id":"repo-id","defaultBranch":"refs/heads/main"}
			}`))
		case "/git/repositories/repo-id/items":
			if r.URL.Query().Get("path") != "/azure-pipelines.yml" ||
				r.URL.Query().Get("versionDescriptor.version") != "main" {
				t.Errorf("unexpected YAML content request: %s", r.URL.String())
			}
			_, _ = w.Write([]byte(`{"content":"parameters:\n- name: target\n  type: string\n  default: test\n  values: [test, prod]\n"}`))
		default:
			t.Errorf("unexpected request: %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	client, err := azdevops.NewClient("org", "project", "test-pat")
	if err != nil {
		t.Fatal(err)
	}
	client.SetBaseURL(server.URL)
	detail := newDefinitionDetailModel(client, azdevops.PipelineDefinition{ID: 12}, styles.DefaultStyles())

	msg := detail.Init()()
	loaded, ok := msg.(definitionLoadedMsg)
	if !ok {
		t.Fatalf("Init() message = %T, want definitionLoadedMsg", msg)
	}
	if loaded.err != nil || loaded.parameterErr != nil {
		t.Fatalf("load error = %v / %v", loaded.err, loaded.parameterErr)
	}
	if len(loaded.parameters) != 1 || loaded.parameters[0].Name != "target" {
		t.Fatalf("loaded parameters = %+v", loaded.parameters)
	}
	detail, _ = detail.Update(loaded)
	view := detail.View()
	if !strings.Contains(view, "YAML Parameters (zP to view)") ||
		!strings.Contains(view, "target (string)") || !strings.Contains(view, "test") {
		t.Fatalf("pipeline detail does not display the loaded parameter:\n%s", view)
	}
}

func TestPipelineCreationSecretValueIsNeverRendered(t *testing.T) {
	creation := newPipelineCreationModel(nil, styles.DefaultStyles())
	creation.step = creationReview
	creation.name = "Deploy"
	creation.variables["TOKEN"] = azdevops.PipelineVariable{
		Value: "highly-sensitive-value", IsSecret: true,
	}
	if view := creation.View(); strings.Contains(view, "highly-sensitive-value") || !strings.Contains(view, "[secret]") {
		t.Fatalf("secret value leaked or secret marker missing: %q", view)
	}
	creation.step = creationVariableValue
	creation.pendingSecret = true
	creation.beginInput("Value: ", "highly-sensitive-value", true)
	if view := creation.View(); strings.Contains(view, "highly-sensitive-value") {
		t.Fatalf("secret input value rendered: %q", view)
	}
}
