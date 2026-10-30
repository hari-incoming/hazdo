package azdevops

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/hari-incoming/hazdo/internal/provider"
)

func TestUpdatePullRequestDescription(t *testing.T) {
	var capturedDescription string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %q, want PATCH", r.Method)
		}
		if r.URL.Path != "/git/repositories/repo-123/pullrequests/42" || r.URL.Query().Get("api-version") != "7.1" {
			t.Errorf("request URL = %q", r.URL.String())
		}
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request body: %v", err)
		}
		var payload struct {
			Description string `json:"description"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		capturedDescription = payload.Description
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := &Client{baseURL: server.URL, httpClient: http.DefaultClient}
	description := "First line\nSecond line"
	if err := client.UpdatePullRequestDescription("repo-123", 42, description); err != nil {
		t.Fatalf("UpdatePullRequestDescription() error = %v", err)
	}
	if capturedDescription != description {
		t.Errorf("description = %q, want %q", capturedDescription, description)
	}
}

func TestUpdatePullRequestTitle(t *testing.T) {
	var capturedTitle string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch {
			t.Errorf("method = %q, want PATCH", r.Method)
		}
		var payload struct {
			Title string `json:"title"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request body: %v", err)
		}
		capturedTitle = payload.Title
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := &Client{baseURL: server.URL, httpClient: http.DefaultClient}
	if err := client.UpdatePullRequestTitle("repo-123", 42, "New summary"); err != nil {
		t.Fatalf("UpdatePullRequestTitle() error = %v", err)
	}
	if capturedTitle != "New summary" {
		t.Errorf("title = %q, want %q", capturedTitle, "New summary")
	}
}

func TestCompletePullRequestUsesExpectedMergeStrategy(t *testing.T) {
	for _, tc := range []struct {
		strategy provider.PRMergeStrategy
		wantWire string
	}{
		{strategy: provider.PRMergeFastForward, wantWire: "rebase"},
		{strategy: provider.PRMergeSquash, wantWire: "squash"},
	} {
		t.Run(string(tc.strategy), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/git/repositories/repo-123/pullRequests/42" ||
					r.URL.Query().Get("api-version") != "7.1" {
					t.Errorf("request URL = %q", r.URL.String())
				}
				switch r.Method {
				case http.MethodGet:
					_, _ = w.Write([]byte(`{"lastMergeSourceCommit":{"commitId":"source-head"}}`))
				case http.MethodPatch:
					var payload struct {
						Status                string `json:"status"`
						LastMergeSourceCommit struct {
							CommitID string `json:"commitId"`
						} `json:"lastMergeSourceCommit"`
						CompletionOptions struct {
							MergeStrategy       string `json:"mergeStrategy"`
							DeleteSourceBranch  bool   `json:"deleteSourceBranch"`
							TransitionWorkItems bool   `json:"transitionWorkItems"`
						} `json:"completionOptions"`
					}
					if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
						t.Errorf("decode completion request: %v", err)
					}
					if payload.Status != "completed" ||
						payload.LastMergeSourceCommit.CommitID != "source-head" ||
						payload.CompletionOptions.MergeStrategy != tc.wantWire ||
						payload.CompletionOptions.DeleteSourceBranch ||
						payload.CompletionOptions.TransitionWorkItems {
						t.Errorf("completion payload = %+v", payload)
					}
					_, _ = w.Write([]byte(`{}`))
				default:
					t.Errorf("unexpected HTTP method %s", r.Method)
				}
			}))
			defer server.Close()

			client := &Client{baseURL: server.URL, httpClient: http.DefaultClient}
			if err := client.CompletePullRequest("repo-123", 42, tc.strategy); err != nil {
				t.Fatalf("CompletePullRequest() error = %v", err)
			}
		})
	}
}

func TestCompletePullRequestRequiresSourceCommit(t *testing.T) {
	var patchCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPatch {
			patchCalled = true
			return
		}
		_, _ = w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := &Client{baseURL: server.URL, httpClient: http.DefaultClient}
	if err := client.CompletePullRequest("repo-123", 42, provider.PRMergeSquash); err == nil {
		t.Fatal("CompletePullRequest() succeeded without the source commit")
	}
	if patchCalled {
		t.Fatal("PATCH was sent without a source commit")
	}
}

func TestAbandonPullRequestSendsAbandonedStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPatch ||
			r.URL.Path != "/git/repositories/repo-123/pullRequests/42" ||
			r.URL.Query().Get("api-version") != "7.1" {
			t.Errorf("request = %s %q", r.Method, r.URL.String())
		}
		var payload struct {
			Status string `json:"status"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode abandonment request: %v", err)
		}
		if payload.Status != "abandoned" {
			t.Errorf("status = %q, want abandoned", payload.Status)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := &Client{baseURL: server.URL, httpClient: http.DefaultClient}
	if err := client.AbandonPullRequest("repo-123", 42); err != nil {
		t.Fatalf("AbandonPullRequest() error = %v", err)
	}
}

func TestAbandonPullRequestRejectsInvalidIdentity(t *testing.T) {
	client := &Client{}
	for _, tc := range []struct {
		repositoryID  string
		pullRequestID int
	}{
		{repositoryID: "", pullRequestID: 1},
		{repositoryID: "repo-123", pullRequestID: 0},
		{repositoryID: "repo-123", pullRequestID: -1},
	} {
		if err := client.AbandonPullRequest(tc.repositoryID, tc.pullRequestID); err == nil {
			t.Errorf("AbandonPullRequest(%q, %d) accepted invalid identity", tc.repositoryID, tc.pullRequestID)
		}
	}
}

func TestUpdatePullRequestDescriptionRejectsInvalidIdentity(t *testing.T) {
	client := &Client{}
	for _, tc := range []struct {
		repositoryID  string
		pullRequestID int
	}{
		{repositoryID: "", pullRequestID: 1},
		{repositoryID: "repo-123", pullRequestID: 0},
		{repositoryID: "repo-123", pullRequestID: -1},
	} {
		if err := client.UpdatePullRequestDescription(tc.repositoryID, tc.pullRequestID, "description"); err == nil {
			t.Errorf("UpdatePullRequestDescription(%q, %d) accepted invalid identity", tc.repositoryID, tc.pullRequestID)
		}
	}
}

func TestListPipelineDefinitionsFollowsContinuationToken(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		if r.URL.Path != "/build/definitions" || r.URL.Query().Get("api-version") != "7.1" {
			t.Errorf("unexpected request %s", r.URL.String())
		}
		switch requests {
		case 1:
			if r.URL.Query().Get("$top") != "1000" {
				t.Errorf("$top = %q, want 1000", r.URL.Query().Get("$top"))
			}
			w.Header().Set("x-ms-continuationtoken", "page + 2")
			_, _ = w.Write([]byte(`{"value":[{"id":1,"name":"Build"}]}`))
		case 2:
			if got := r.URL.Query().Get("continuationToken"); got != "page + 2" {
				t.Errorf("continuationToken = %q, want %q", got, "page + 2")
			}
			_, _ = w.Write([]byte(`{"value":[{"id":2,"name":"Deploy","path":"\\Release"}]}`))
		default:
			t.Errorf("unexpected request %d", requests)
		}
	}))
	defer server.Close()

	client, err := NewClient("org", "project", "pat")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL

	definitions, err := client.ListPipelineDefinitions()
	if err != nil {
		t.Fatalf("ListPipelineDefinitions() error = %v", err)
	}
	if requests != 2 || len(definitions) != 2 || definitions[1].Path != `\Release` {
		t.Fatalf("got %d requests and definitions %#v", requests, definitions)
	}
}

func TestQueuePipelineNormalizesBranchAndDecodesRun(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/build/builds" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
		}
		var payload struct {
			Definition   PipelineDefinition `json:"definition"`
			SourceBranch string             `json:"sourceBranch"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode queue request: %v", err)
		}
		if payload.Definition.ID != 12 || payload.SourceBranch != "refs/heads/feature/x" {
			t.Errorf("queue payload = %+v", payload)
		}
		_, _ = w.Write([]byte(`{"id":42,"buildNumber":"42","definition":{"id":12,"name":"CI"}}`))
	}))
	defer server.Close()

	client, err := NewClient("org", "project", "pat")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL

	run, err := client.QueuePipeline(12, "feature/x")
	if err != nil {
		t.Fatalf("QueuePipeline() error = %v", err)
	}
	if run.ID != 42 || run.Definition.Name != "CI" {
		t.Fatalf("run = %+v", run)
	}
}

func TestGetPipelineDefinitionReturnsVariablesAndDefaultBranch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/build/definitions/12" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
		}
		if r.URL.Query().Get("api-version") != "7.1" {
			t.Errorf("api-version = %q", r.URL.Query().Get("api-version"))
		}
		_, _ = w.Write([]byte(`{
			"id":12,"name":"CI","path":"\\Build",
			"repository":{"defaultBranch":"refs/heads/develop"},
			"variables":{"Configuration":{"value":"Release","allowOverride":true},"Token":{"isSecret":true}}
		}`))
	}))
	defer server.Close()

	client, err := NewClient("org", "project", "pat")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL

	definition, err := client.GetPipelineDefinition(12)
	if err != nil {
		t.Fatalf("GetPipelineDefinition() error = %v", err)
	}
	if definition.Name != "CI" || definition.Repository.DefaultBranch != "refs/heads/develop" {
		t.Fatalf("definition = %+v", definition)
	}
	if got := definition.Variables["Configuration"]; got.Value != "Release" || !got.AllowOverride {
		t.Errorf("Configuration variable = %+v", got)
	}
	if got := definition.Variables["Token"]; !got.IsSecret {
		t.Errorf("Token variable = %+v, want secret", got)
	}
}

func TestAddPipelineDefinitionVariablePreservesFieldsAndSavesSecret(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/build/definitions/12" || r.URL.Query().Get("api-version") != "7.1" {
			t.Errorf("unexpected request URL %s", r.URL.String())
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{
				"id":12,"name":"CI","path":"\\Build","revision":4,
				"process":{"type":2,"yamlFilename":"/ci.yml"},
				"variables":{"Configuration":{"value":"Release","allowOverride":true}}
			}`))
		case http.MethodPut:
			var payload map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode updated definition: %v", err)
				http.Error(w, "bad request", http.StatusBadRequest)
				return
			}
			var revision int
			if err := json.Unmarshal(payload["revision"], &revision); err != nil || revision != 4 {
				t.Errorf("revision = %d, err = %v; want preserved revision 4", revision, err)
			}
			var process PipelineProcess
			if err := json.Unmarshal(payload["process"], &process); err != nil || process.YAMLFileName != "/ci.yml" {
				t.Errorf("process = %+v, err = %v; want original YAML process", process, err)
			}
			variables := make(map[string]PipelineVariable)
			if err := json.Unmarshal(payload["variables"], &variables); err != nil {
				t.Errorf("decode variables: %v", err)
			}
			if got := variables["Configuration"]; got.Value != "Release" || !got.AllowOverride {
				t.Errorf("existing variable was not preserved: %+v", got)
			}
			if got := variables["DeployKey"]; got.Value != "secret-value" || !got.IsSecret || !got.AllowOverride {
				t.Errorf("new variable = %+v, want secret and queue-time overridable", got)
			}
			_, _ = w.Write([]byte(`{
				"id":12,"name":"CI","path":"\\Build","revision":5,
				"variables":{
					"Configuration":{"value":"Release","allowOverride":true},
					"DeployKey":{"isSecret":true,"allowOverride":true}
				}
			}`))
		default:
			t.Errorf("unexpected request method %s", r.Method)
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	client, err := NewClient("org", "project", "pat")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	definition, err := client.AddPipelineDefinitionVariable(12, " DeployKey ", "secret-value", true)
	if err != nil {
		t.Fatalf("AddPipelineDefinitionVariable() error = %v", err)
	}
	if definition.Variables["DeployKey"].Value != "" || !definition.Variables["DeployKey"].IsSecret {
		t.Fatalf("saved secret variable = %+v", definition.Variables["DeployKey"])
	}
}

func TestAddPipelineDefinitionVariableRejectsDuplicateCaseInsensitive(t *testing.T) {
	var putCalled bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			putCalled = true
			t.Error("duplicate variable should not issue a PUT")
			return
		}
		_, _ = w.Write([]byte(`{"id":12,"variables":{"DeployKey":{"isSecret":true}}}`))
	}))
	defer server.Close()

	client, err := NewClient("org", "project", "pat")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	_, err = client.AddPipelineDefinitionVariable(12, "deploykey", "unused", false)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("duplicate variable error = %v, want already exists", err)
	}
	if putCalled {
		t.Fatal("duplicate variable triggered a definition update")
	}
}

func TestAddPipelineDefinitionSecretVariableHidesHTTPErrorDetails(t *testing.T) {
	const secretValue = "do-not-leak-this-secret"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			_, _ = w.Write([]byte(`{"id":12,"variables":{}}`))
			return
		}
		http.Error(w, secretValue, http.StatusBadRequest)
	}))
	defer server.Close()

	client, err := NewClient("org", "project", "pat")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	_, err = client.AddPipelineDefinitionVariable(12, "Token", secretValue, true)
	if err == nil || strings.Contains(err.Error(), secretValue) {
		t.Fatalf("secret update error = %v, want a redacted error", err)
	}
}

func TestRenamePipelineDefinitionPreservesDefinitionFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/build/definitions/7" || r.URL.Query().Get("api-version") != "7.1" {
			t.Errorf("unexpected definition endpoint %s", r.URL.String())
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{
				"id":7,
				"name":"Old name",
				"path":"\\Automation",
				"revision":3,
				"type":"build",
				"queueStatus":"enabled",
				"process":{"type":2,"yamlFilename":"/ci/pipeline.yml"},
				"repository":{"id":"repo-1","defaultBranch":"refs/heads/main"},
				"variables":{"Mode":{"value":"Release"}},
				"triggers":[{"triggerType":"continuousIntegration"}]
			}`))
		case http.MethodPut:
			var payload map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode rename request: %v", err)
			}
			var name string
			var revision int
			if err := json.Unmarshal(payload["name"], &name); err != nil {
				t.Errorf("decode renamed name: %v", err)
			}
			if err := json.Unmarshal(payload["revision"], &revision); err != nil {
				t.Errorf("decode preserved revision: %v", err)
			}
			if name != "New name" || revision != 3 || len(payload["triggers"]) == 0 ||
				len(payload["repository"]) == 0 || len(payload["variables"]) == 0 {
				t.Errorf("rename payload did not preserve the definition: %+v", payload)
			}
			_, _ = w.Write([]byte(`{"id":7,"name":"New name","path":"\\Automation"}`))
		default:
			t.Errorf("unexpected method %s", r.Method)
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	client, err := NewClient("org", "project", "pat")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	definition, err := client.RenamePipelineDefinition(7, " New name ")
	if err != nil {
		t.Fatalf("RenamePipelineDefinition() error = %v", err)
	}
	if definition.ID != 7 || definition.Name != "New name" || definition.Path != `\Automation` {
		t.Fatalf("renamed definition = %+v", definition)
	}
}

func TestMovePipelineDefinitionPreservesDefinitionFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/build/definitions/7" || r.URL.Query().Get("api-version") != "7.1" {
			t.Errorf("unexpected definition endpoint %s", r.URL.String())
		}
		switch r.Method {
		case http.MethodGet:
			_, _ = w.Write([]byte(`{
				"id":7,"name":"Build","path":"\\Automation","revision":3,
				"process":{"type":2,"yamlFilename":"/ci/pipeline.yml"},
				"repository":{"id":"repo-1","defaultBranch":"refs/heads/main"},
				"variables":{"Mode":{"value":"Release"}}
			}`))
		case http.MethodPut:
			var payload map[string]json.RawMessage
			if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
				t.Errorf("decode move request: %v", err)
			}
			var name, definitionPath string
			var revision int
			if err := json.Unmarshal(payload["name"], &name); err != nil {
				t.Errorf("decode preserved name: %v", err)
			}
			if err := json.Unmarshal(payload["path"], &definitionPath); err != nil {
				t.Errorf("decode moved path: %v", err)
			}
			if err := json.Unmarshal(payload["revision"], &revision); err != nil {
				t.Errorf("decode preserved revision: %v", err)
			}
			if name != "Build" || definitionPath != `\Deploy\Backend` || revision != 3 ||
				len(payload["process"]) == 0 || len(payload["repository"]) == 0 || len(payload["variables"]) == 0 {
				t.Errorf("move payload did not preserve the definition: %+v", payload)
			}
			_, _ = w.Write([]byte(`{"id":7,"name":"Build","path":"\\Deploy\\Backend"}`))
		default:
			t.Errorf("unexpected method %s", r.Method)
			http.Error(w, "unexpected method", http.StatusMethodNotAllowed)
		}
	}))
	defer server.Close()

	client, err := NewClient("org", "project", "pat")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	definition, err := client.MovePipelineDefinition(7, "Deploy/Backend")
	if err != nil {
		t.Fatalf("MovePipelineDefinition() error = %v", err)
	}
	if definition.ID != 7 || definition.Name != "Build" || definition.Path != `\Deploy\Backend` {
		t.Fatalf("moved definition = %+v", definition)
	}
}

func TestNormalizePipelineDefinitionPath(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    string
		wantErr bool
	}{
		{name: "project root", input: `\`, want: `\`},
		{name: "nested path", input: " Deploy/Backend ", want: `\Deploy\Backend`},
		{name: "absolute path", input: `\Automation`, want: `\Automation`},
		{name: "empty", input: " ", wantErr: true},
		{name: "parent traversal", input: `\..\Other`, wantErr: true},
		{name: "empty segment", input: `\Deploy\\Backend`, wantErr: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := normalizePipelineDefinitionPath(test.input)
			if (err != nil) != test.wantErr {
				t.Fatalf("normalizePipelineDefinitionPath(%q) error = %v, wantErr %t", test.input, err, test.wantErr)
			}
			if err == nil && got != test.want {
				t.Errorf("normalizePipelineDefinitionPath(%q) = %q, want %q", test.input, got, test.want)
			}
		})
	}
}

func TestQueuePipelineWithVariablesEncodesOverrides(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Definition   PipelineDefinition          `json:"definition"`
			SourceBranch string                      `json:"sourceBranch"`
			Variables    map[string]PipelineVariable `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode queue request: %v", err)
		}
		if payload.Definition.ID != 12 || payload.SourceBranch != "refs/heads/release" {
			t.Errorf("queue payload = %+v", payload)
		}
		if got := payload.Variables["Configuration"]; got.Value != "Debug" {
			t.Errorf("Configuration override = %+v", got)
		}
		if got := payload.Variables["Token"]; got.Value != "new-secret" || !got.IsSecret {
			t.Errorf("Token override = %+v", got)
		}
		_, _ = w.Write([]byte(`{"id":45,"buildNumber":"45"}`))
	}))
	defer server.Close()

	client, err := NewClient("org", "project", "pat")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL

	_, err = client.QueuePipelineWithVariables(12, "release", map[string]PipelineVariable{
		"Configuration": {Value: "Debug"},
		"Token":         {Value: "new-secret", IsSecret: true},
	})
	if err != nil {
		t.Fatalf("QueuePipelineWithVariables() error = %v", err)
	}
}

func TestQueueYAMLPipelineRunEncodesParametersVariablesAndBranch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/pipelines/12/runs" || r.URL.Query().Get("api-version") != "7.1" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
		}
		var payload struct {
			Resources struct {
				Repositories map[string]struct {
					RefName string `json:"refName"`
				} `json:"repositories"`
			} `json:"resources"`
			TemplateParameters map[string]any              `json:"templateParameters"`
			Variables          map[string]PipelineVariable `json:"variables"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode run request: %v", err)
		}
		if payload.Resources.Repositories["self"].RefName != "refs/heads/release" ||
			payload.TemplateParameters["count"] != float64(3) ||
			payload.TemplateParameters["deploy"] != true ||
			payload.TemplateParameters["configuration"] != "region: east" ||
			payload.TemplateParameters["steps"] != "- deploy\n- verify" {
			t.Errorf("run request = %+v", payload)
		}
		if got := payload.Variables["Token"]; got.Value != "hidden" || !got.IsSecret {
			t.Errorf("secret variable = %+v", got)
		}
		_, _ = w.Write([]byte(`{"id":45,"name":"2026.04.01.1"}`))
	}))
	defer server.Close()

	client, err := NewClient("org", "project", "pat")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	run, err := client.QueueYAMLPipelineRun(12, "release", map[string]any{
		"count":  3,
		"deploy": true,
		"configuration": map[string]any{
			"region": "east",
		},
		"steps": []any{"deploy", "verify"},
	}, map[string]PipelineVariable{"Token": {Value: "hidden", IsSecret: true}})
	if err != nil {
		t.Fatalf("QueueYAMLPipelineRun() error = %v", err)
	}
	if run.ID != 45 || run.BuildNumber != "2026.04.01.1" || run.SourceBranch != "refs/heads/release" {
		t.Fatalf("run = %+v", run)
	}
}

func TestQueueYAMLPipelineRunSendsEmptyTemplateParametersObject(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode run request: %v", err)
		}
		if got := string(payload["templateParameters"]); got != "{}" {
			t.Errorf("templateParameters = %q, want explicit empty object", got)
		}
		_, _ = w.Write([]byte(`{"id":46,"name":"2026.04.01.2"}`))
	}))
	defer server.Close()

	client, err := NewClient("org", "project", "pat")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	if _, err := client.QueueYAMLPipelineRun(12, "main", nil, nil); err != nil {
		t.Fatalf("QueueYAMLPipelineRun() error = %v", err)
	}
}

func TestQueueYAMLPipelineRunErrorIncludesRedactedRequestDiagnostic(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/pipelines/12" {
			_, _ = w.Write([]byte(`{
				"id":12,
				"name":"Deploy",
				"configuration":{
					"type":"yaml",
					"path":"/azure-pipelines.yml",
					"repository":{"id":"repo-1","type":"azureReposGit"}
				}
			}`))
			return
		}
		if r.Method != http.MethodPost || r.URL.Path != "/pipelines/12/runs" {
			t.Errorf("unexpected diagnostic request %s %s", r.Method, r.URL.String())
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Errorf("decode run request: %v", err)
		}
		if len(body) == 0 {
			t.Error("server received an empty request body")
		}
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"message":"Value cannot be null.","typeKey":"ArgumentNullException"}`))
	}))
	defer server.Close()

	client, err := NewClient("org", "project", "pat")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	_, err = client.QueueYAMLPipelineRun(
		12,
		"release",
		map[string]any{"environment": "sensitive-parameter"},
		map[string]PipelineVariable{"Token": {Value: "sensitive-variable", IsSecret: true}},
	)
	if err == nil {
		t.Fatal("QueueYAMLPipelineRun() should return the HTTP 400")
	}
	for _, want := range []string{
		"failed to queue YAML pipeline",
		"POST /pipelines/12/runs?api-version=7.1",
		"Content-Type: application/json",
		`"refName":"refs/heads/release"`,
		`"environment":"REDACTED"`,
		`"Token":{"value":"REDACTED","isSecret":true}`,
		"typeKey=ArgumentNullException",
		`GET /pipelines/12?api-version=7.1 returned pipeline id=12 name="Deploy"`,
		`configuration.type="yaml"`,
		`configuration.path="/azure-pipelines.yml"`,
		`repository.type="azureReposGit"`,
	} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("queue error %q does not contain diagnostic %q", err, want)
		}
	}
	for _, secret := range []string{"sensitive-parameter", "sensitive-variable"} {
		if strings.Contains(err.Error(), secret) {
			t.Errorf("queue error exposed request value %q: %v", secret, err)
		}
	}
}

func TestGetYAMLPipelineRunParametersReturnsCapturedValues(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/pipelines/12/runs/45" || r.URL.Query().Get("api-version") != "7.1" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
		}
		_, _ = w.Write([]byte(`{"templateParameters":{"environment":"production","replicas":3}}`))
	}))
	defer server.Close()

	client, err := NewClient("org", "project", "pat")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL

	parameters, err := client.GetYAMLPipelineRunParameters(12, 45)
	if err != nil {
		t.Fatalf("GetYAMLPipelineRunParameters() error = %v", err)
	}
	if parameters["environment"] != "production" || parameters["replicas"] != float64(3) {
		t.Fatalf("parameters = %#v", parameters)
	}
}

func TestCompositeProviderGetsPipelineRunParameters(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/build/definitions/12":
			_, _ = w.Write([]byte(`{"id":12,"process":{"type":2,"yamlFilename":"/azure-pipelines.yml"}}`))
		case "/pipelines/12/runs/45":
			_, _ = w.Write([]byte(`{"templateParameters":{"environment":"production"}}`))
		default:
			t.Errorf("unexpected request %s", r.URL.String())
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	mc := newMultiClientWithServers(t, map[string]*httptest.Server{"project": server})
	composite := provider.NewCompositeProvider(NewAdapter(mc))
	parameters, err := composite.GetPipelineRunParameters("project", 12, 45)
	if err != nil {
		t.Fatalf("GetPipelineRunParameters() error = %v", err)
	}
	if parameters["environment"] != "production" {
		t.Fatalf("parameters = %#v", parameters)
	}
}

func TestListYAMLPipelineFilesFiltersAndUsesBranch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/git/repositories/repo-id/items" ||
			r.URL.Query().Get("versionDescriptor.version") != "main" ||
			r.URL.Query().Get("versionDescriptor.versionType") != "branch" ||
			r.URL.Query().Get("recursionLevel") != "Full" {
			t.Errorf("unexpected YAML file request %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"value":[
			{"path":"/azure-pipelines.yml","gitObjectType":"blob"},
			{"path":"/nested/build.yaml","gitObjectType":"blob"},
			{"path":"/readme.md","gitObjectType":"blob"},
			{"path":"/nested","gitObjectType":"tree","isFolder":true}
		]}`))
	}))
	defer server.Close()
	client, err := NewClient("org", "project", "pat")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	files, err := client.ListYAMLPipelineFiles("repo-id", "refs/heads/main")
	if err != nil {
		t.Fatalf("ListYAMLPipelineFiles() error = %v", err)
	}
	if len(files) != 2 || files[0] != "/azure-pipelines.yml" || files[1] != "/nested/build.yaml" {
		t.Fatalf("files = %v", files)
	}
}

func TestListAgentQueuesReturnsUsableQueuesSortedByName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/distributedtask/queues" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
		}
		if r.URL.Query().Get("actionFilter") != "use" || r.URL.Query().Get("api-version") != "7.1" {
			t.Errorf("unexpected queue query %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"value":[
			{"id":4,"name":"Linux","pool":{"id":2,"name":"Self hosted"}},
			{"id":2,"name":"Azure Pipelines","pool":{"id":1,"name":"Azure Pipelines"}}
		]}`))
	}))
	defer server.Close()
	client, err := NewClient("org", "project", "pat")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL

	queues, err := client.ListAgentQueues()
	if err != nil {
		t.Fatalf("ListAgentQueues() error = %v", err)
	}
	want := []AgentPoolQueue{
		{ID: 2, Name: "Azure Pipelines", Pool: AgentPoolReference{ID: 1, Name: "Azure Pipelines"}},
		{ID: 4, Name: "Linux", Pool: AgentPoolReference{ID: 2, Name: "Self hosted"}},
	}
	if !reflect.DeepEqual(queues, want) {
		t.Fatalf("ListAgentQueues() = %#v, want %#v", queues, want)
	}
}

func TestListBranchesReturnsSortedShortNames(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != "/git/repositories/repo-id/refs" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
		}
		if got := r.URL.Query().Get("filter"); got != "heads/" {
			t.Errorf("filter = %q, want heads/", got)
		}
		if got := r.URL.Query().Get("api-version"); got != "7.1" {
			t.Errorf("api-version = %q, want 7.1", got)
		}
		_, _ = w.Write([]byte(`{"value":[
			{"name":"refs/heads/z-feature"},
			{"name":"refs/heads/main"},
			{"name":"refs/tags/v1.0"},
			{"name":"refs/heads/feature/nested"}
		]}`))
	}))
	defer server.Close()

	client, err := NewClient("org", "project", "pat")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL

	branches, err := client.ListBranches("repo-id")
	if err != nil {
		t.Fatalf("ListBranches() error = %v", err)
	}
	want := []string{"feature/nested", "main", "z-feature"}
	if !reflect.DeepEqual(branches, want) {
		t.Fatalf("ListBranches() = %#v, want %#v", branches, want)
	}
}

func TestGetRepositoryFileReadsContentAtBranch(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("path") != "/azure-pipelines.yml" ||
			r.URL.Query().Get("includeContent") != "true" ||
			r.URL.Query().Get("versionDescriptor.version") != "main" {
			t.Errorf("unexpected file request %s", r.URL.String())
		}
		_, _ = w.Write([]byte(`{"content":"parameters:\n- name: deploy\n  type: boolean\n  default: false\n"}`))
	}))
	defer server.Close()
	client, err := NewClient("org", "project", "pat")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	content, err := client.GetRepositoryFile("repo-id", "/azure-pipelines.yml", "main")
	if err != nil {
		t.Fatalf("GetRepositoryFile() error = %v", err)
	}
	if content != "parameters:\n- name: deploy\n  type: boolean\n  default: false\n" {
		t.Fatalf("content = %q", content)
	}
}

func TestCreateYAMLPipelineDefinitionIncludesSecretVariables(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/build/definitions" || r.URL.Query().Get("api-version") != "7.1" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
		}
		var payload struct {
			Name        string                      `json:"name"`
			Path        string                      `json:"path"`
			QueueStatus string                      `json:"queueStatus"`
			Process     PipelineProcess             `json:"process"`
			Repository  PipelineRepository          `json:"repository"`
			Variables   map[string]PipelineVariable `json:"variables"`
			Queue       *AgentPoolQueue             `json:"queue"`
		}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode create request: %v", err)
		}
		if payload.Name != "Deploy" || payload.Path != `\Automation` || payload.QueueStatus != "enabled" ||
			payload.Process.Type != 2 || payload.Process.YAMLFileName != "/ci/main.yml" ||
			payload.Repository.ID != "repo-id" || payload.Repository.DefaultBranch != "refs/heads/main" {
			t.Errorf("create payload = %+v", payload)
		}
		if got := payload.Variables["Token"]; got.Value != "not-for-display" || !got.IsSecret || !got.AllowOverride {
			t.Errorf("secret variable = %+v", got)
		}
		if payload.Queue == nil || payload.Queue.ID != 4 || payload.Queue.Name != "Linux" ||
			payload.Queue.Pool.ID != 2 || payload.Queue.Pool.Name != "Self hosted" {
			t.Errorf("agent queue = %+v", payload.Queue)
		}
		_, _ = w.Write([]byte(`{"id":99,"name":"Deploy"}`))
	}))
	defer server.Close()
	client, err := NewClient("org", "project", "pat")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL
	definition, err := client.CreateYAMLPipelineDefinition("Deploy", Repository{
		ID: "repo-id", Name: "repo", Type: "TfsGit", DefaultBranch: "main",
		RemoteURL: "https://dev.azure.com/org/project/_git/repo",
	}, "/ci/main.yml", map[string]PipelineVariable{
		"Token": {Value: "not-for-display", IsSecret: true, AllowOverride: true},
	}, `\Automation`, &AgentPoolQueue{
		ID: 4, Name: "Linux", Pool: AgentPoolReference{ID: 2, Name: "Self hosted"},
	})
	if err != nil {
		t.Fatalf("CreateYAMLPipelineDefinition() error = %v", err)
	}
	if definition.ID != 99 || definition.Name != "Deploy" {
		t.Fatalf("definition = %+v", definition)
	}
}

func TestCreatePullRequestEncodesBranchesAndDescription(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/git/repositories/repo-id/pullrequests" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.String())
		}
		if r.URL.Query().Get("api-version") != "7.1" {
			t.Errorf("api-version = %q", r.URL.Query().Get("api-version"))
		}
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode PR request: %v", err)
		}
		if payload["sourceRefName"] != "refs/heads/feature/x" ||
			payload["targetRefName"] != "refs/heads/main" ||
			payload["title"] != "Add feature" ||
			payload["description"] != "Details" ||
			payload["isDraft"] != true {
			t.Errorf("PR payload = %#v", payload)
		}
		_, _ = w.Write([]byte(`{"pullRequestId":9,"title":"Add feature","status":"active"}`))
	}))
	defer server.Close()

	client, err := NewClient("org", "project", "pat")
	if err != nil {
		t.Fatal(err)
	}
	client.baseURL = server.URL

	pr, err := client.CreatePullRequest("repo-id", "Add feature", "Details", "feature/x", "main", true)
	if err != nil {
		t.Fatalf("CreatePullRequest() error = %v", err)
	}
	if pr.ID != 9 || pr.Title != "Add feature" {
		t.Fatalf("PR = %+v", pr)
	}
}

func TestBranchRefRejectsNonHeadRef(t *testing.T) {
	for _, branch := range []string{"", "refs/tags/v1"} {
		if _, err := branchRef(branch); err == nil {
			t.Errorf("branchRef(%q) expected error", branch)
		}
	}
	if got, err := branchRef("refs/heads/main"); err != nil || got != "refs/heads/main" {
		t.Errorf("branchRef() = %q, %v", got, err)
	}
	if got, err := branchRef("main"); err != nil || got != "refs/heads/main" {
		t.Errorf("branchRef() = %q, %v", got, err)
	}
}
