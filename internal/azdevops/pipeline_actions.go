package azdevops

import (
	"encoding/json"
	"fmt"
	"net/url"
	"path"
	"sort"
	"strconv"
	"strings"

	"github.com/hari-incoming/hazdo/internal/provider"
	"gopkg.in/yaml.v3"
)

const pipelineDefinitionsPageSize = 1000

type pipelineDefinitionsResponse struct {
	Value []PipelineDefinition `json:"value"`
}

// ListPipelineDefinitions returns every pipeline definition in the project.
// Azure DevOps paginates definition lists with x-ms-continuationtoken.
func (c *Client) ListPipelineDefinitions() ([]PipelineDefinition, error) {
	var definitions []PipelineDefinition
	seenTokens := make(map[string]struct{})
	continuationToken := ""

	for {
		path := fmt.Sprintf("/build/definitions?api-version=7.1&$top=%d", pipelineDefinitionsPageSize)
		if continuationToken != "" {
			path += "&continuationToken=" + url.QueryEscape(continuationToken)
		}

		body, headers, err := c.getWithHeaders(path)
		if err != nil {
			return nil, fmt.Errorf("failed to list pipeline definitions: %w", err)
		}

		var response pipelineDefinitionsResponse
		if err := json.Unmarshal(body, &response); err != nil {
			return nil, fmt.Errorf("failed to parse Azure DevOps pipeline definitions: %w", err)
		}
		definitions = append(definitions, response.Value...)

		nextToken := headers.Get("x-ms-continuationtoken")
		if nextToken == "" {
			return definitions, nil
		}
		if _, ok := seenTokens[nextToken]; ok {
			return nil, fmt.Errorf("Azure DevOps repeated pipeline definition continuation token")
		}
		seenTokens[nextToken] = struct{}{}
		continuationToken = nextToken
	}
}

// GetPipelineDefinition returns the full definition, including its variables
// and repository default branch.
func (c *Client) GetPipelineDefinition(definitionID int) (*PipelineDefinition, error) {
	if definitionID <= 0 {
		return nil, fmt.Errorf("pipeline definition ID must be positive")
	}
	path := fmt.Sprintf("/build/definitions/%d?api-version=7.1", definitionID)
	body, err := c.get(path)
	if err != nil {
		return nil, fmt.Errorf("failed to get pipeline definition %d: %w", definitionID, err)
	}
	var definition PipelineDefinition
	if err := json.Unmarshal(body, &definition); err != nil {
		return nil, fmt.Errorf("failed to parse Azure DevOps pipeline definition: %w", err)
	}
	return &definition, nil
}

// RenamePipelineDefinition updates a pipeline definition's name while
// preserving all fields returned by Azure DevOps.
func (c *Client) RenamePipelineDefinition(definitionID int, name string) (*PipelineDefinition, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("pipeline name is required")
	}
	return c.updatePipelineDefinitionProperty(definitionID, "name", name)
}

// MovePipelineDefinition moves a pipeline definition to another project folder.
func (c *Client) MovePipelineDefinition(definitionID int, definitionPath string) (*PipelineDefinition, error) {
	definitionPath, err := normalizePipelineDefinitionPath(definitionPath)
	if err != nil {
		return nil, err
	}
	return c.updatePipelineDefinitionProperty(definitionID, "path", definitionPath)
}

// AddPipelineDefinitionVariable saves a variable in the pipeline definition.
// Existing definition fields are preserved, and the variable is queue-time
// overridable for runs that need a temporary value.
func (c *Client) AddPipelineDefinitionVariable(definitionID int, name, value string, isSecret bool) (*PipelineDefinition, error) {
	if definitionID <= 0 {
		return nil, fmt.Errorf("pipeline definition ID must be positive")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, fmt.Errorf("pipeline variable name is required")
	}

	endpoint := fmt.Sprintf("/build/definitions/%d?api-version=7.1", definitionID)
	body, err := c.get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get pipeline definition %d before adding a variable: %w", definitionID, err)
	}
	var definition map[string]json.RawMessage
	if err := json.Unmarshal(body, &definition); err != nil {
		return nil, fmt.Errorf("failed to parse pipeline definition %d before adding a variable: %w", definitionID, err)
	}
	if definition == nil {
		return nil, fmt.Errorf("pipeline definition %d is empty", definitionID)
	}

	variables := make(map[string]json.RawMessage)
	if rawVariables := definition["variables"]; len(rawVariables) > 0 {
		if err := json.Unmarshal(rawVariables, &variables); err != nil {
			return nil, fmt.Errorf("failed to parse pipeline variables before adding a variable: %w", err)
		}
	}
	if variables == nil {
		variables = make(map[string]json.RawMessage)
	}
	for existing := range variables {
		if strings.EqualFold(existing, name) {
			return nil, fmt.Errorf("pipeline variable %q already exists", name)
		}
	}

	variable, err := json.Marshal(PipelineVariable{
		Value:         value,
		IsSecret:      isSecret,
		AllowOverride: true,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to encode pipeline variable: %w", err)
	}
	variables[name] = variable
	definition["variables"], err = json.Marshal(variables)
	if err != nil {
		return nil, fmt.Errorf("failed to encode pipeline variables: %w", err)
	}
	payload, err := json.Marshal(definition)
	if err != nil {
		return nil, fmt.Errorf("failed to encode pipeline definition: %w", err)
	}
	updatedBody, err := c.put(endpoint, strings.NewReader(string(payload)))
	if err != nil {
		if isSecret {
			return nil, fmt.Errorf("failed to save secret pipeline variable; error details were hidden")
		}
		return nil, fmt.Errorf("failed to save pipeline variable: %w", err)
	}

	var updated PipelineDefinition
	if err := json.Unmarshal(updatedBody, &updated); err != nil {
		if isSecret {
			return nil, fmt.Errorf("failed to parse updated pipeline definition; error details were hidden")
		}
		return nil, fmt.Errorf("failed to parse updated pipeline definition: %w", err)
	}
	if updated.ID != definitionID {
		return nil, fmt.Errorf("Azure DevOps returned pipeline definition %d after updating definition %d",
			updated.ID, definitionID)
	}
	var saved *PipelineVariable
	for variableName, variable := range updated.Variables {
		if strings.EqualFold(variableName, name) {
			saved = &variable
			break
		}
	}
	if saved == nil || saved.IsSecret != isSecret || (!isSecret && saved.Value != value) {
		return nil, fmt.Errorf("Azure DevOps did not confirm saving pipeline variable %q", name)
	}
	return &updated, nil
}

func (c *Client) updatePipelineDefinitionProperty(definitionID int, property, value string) (*PipelineDefinition, error) {
	if definitionID <= 0 {
		return nil, fmt.Errorf("pipeline definition ID must be positive")
	}
	endpoint := fmt.Sprintf("/build/definitions/%d?api-version=7.1", definitionID)
	body, err := c.get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get pipeline definition %d before updating it: %w", definitionID, err)
	}
	var definition map[string]json.RawMessage
	if err := json.Unmarshal(body, &definition); err != nil {
		return nil, fmt.Errorf("failed to parse pipeline definition %d before updating it: %w", definitionID, err)
	}
	if definition == nil {
		return nil, fmt.Errorf("pipeline definition %d is empty", definitionID)
	}
	encodedValue, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("failed to encode pipeline definition property: %w", err)
	}
	definition[property] = encodedValue
	payload, err := json.Marshal(definition)
	if err != nil {
		return nil, fmt.Errorf("failed to encode updated pipeline definition: %w", err)
	}
	updatedBody, err := c.put(endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return nil, fmt.Errorf("failed to update pipeline definition %d: %w", definitionID, err)
	}
	var updated PipelineDefinition
	if err := json.Unmarshal(updatedBody, &updated); err != nil {
		return nil, fmt.Errorf("failed to parse updated pipeline definition: %w", err)
	}
	if updated.ID != definitionID {
		return nil, fmt.Errorf("Azure DevOps returned pipeline definition %d after updating definition %d",
			updated.ID, definitionID)
	}
	switch property {
	case "name":
		if updated.Name != value {
			return nil, fmt.Errorf("Azure DevOps returned pipeline name %q after renaming it to %q",
				updated.Name, value)
		}
	case "path":
		if updated.Path != value {
			return nil, fmt.Errorf("Azure DevOps returned pipeline path %q after moving it to %q",
				updated.Path, value)
		}
	}
	return &updated, nil
}

func normalizePipelineDefinitionPath(value string) (string, error) {
	value = strings.ReplaceAll(strings.TrimSpace(value), "/", `\`)
	if value == "" {
		return "", fmt.Errorf("pipeline folder path is required; use \\ for the project root")
	}
	if value == `\` {
		return value, nil
	}
	value = strings.Trim(value, `\`)
	for _, segment := range strings.Split(value, `\`) {
		if segment == "" || segment == "." || segment == ".." {
			return "", fmt.Errorf("pipeline folder path contains an invalid segment")
		}
	}
	return `\` + value, nil
}

// QueuePipeline starts a run of a definition on the requested branch.
func (c *Client) QueuePipeline(definitionID int, branch string) (*PipelineRun, error) {
	return c.QueuePipelineWithVariables(definitionID, branch, nil)
}

// QueuePipelineWithVariables starts a run with branch and variable overrides.
func (c *Client) QueuePipelineWithVariables(definitionID int, branch string, variables map[string]PipelineVariable) (*PipelineRun, error) {
	if definitionID <= 0 {
		return nil, fmt.Errorf("pipeline definition ID must be positive")
	}
	sourceBranch, err := branchRef(branch)
	if err != nil {
		return nil, err
	}

	payload, err := json.Marshal(struct {
		Definition struct {
			ID int `json:"id"`
		} `json:"definition"`
		SourceBranch string                      `json:"sourceBranch"`
		Variables    map[string]PipelineVariable `json:"variables,omitempty"`
	}{
		Definition: struct {
			ID int `json:"id"`
		}{ID: definitionID},
		SourceBranch: sourceBranch,
		Variables:    variables,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to encode pipeline run request: %w", err)
	}

	body, err := c.post("/build/builds?api-version=7.1", strings.NewReader(string(payload)))
	if err != nil {
		return nil, fmt.Errorf("failed to queue pipeline: %w", err)
	}

	var run PipelineRun
	if err := json.Unmarshal(body, &run); err != nil {
		return nil, fmt.Errorf("failed to parse queued pipeline run: %w", err)
	}
	return &run, nil
}

// QueueYAMLPipelineRun starts a YAML pipeline with runtime parameters and
// variable overrides using the Pipelines Runs API.
func (c *Client) QueueYAMLPipelineRun(pipelineID int, branch string, parameters map[string]any, variables map[string]PipelineVariable) (*PipelineRun, error) {
	if pipelineID <= 0 {
		return nil, fmt.Errorf("pipeline ID must be positive")
	}
	sourceBranch, err := branchRef(branch)
	if err != nil {
		return nil, err
	}
	if parameters == nil {
		parameters = make(map[string]any)
	}
	templateParameters, err := encodeTemplateParameters(parameters)
	if err != nil {
		return nil, fmt.Errorf("failed to encode YAML template parameters: %w", err)
	}
	type runRequest struct {
		Resources struct {
			Repositories map[string]struct {
				RefName string `json:"refName"`
			} `json:"repositories"`
		} `json:"resources"`
		TemplateParameters map[string]any              `json:"templateParameters"`
		Variables          map[string]PipelineVariable `json:"variables,omitempty"`
	}
	request := runRequest{TemplateParameters: templateParameters, Variables: variables}
	request.Resources.Repositories = map[string]struct {
		RefName string `json:"refName"`
	}{"self": {RefName: sourceBranch}}
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, fmt.Errorf("failed to encode pipeline run request: %w", err)
	}
	redactedRequest := request
	redactedRequest.TemplateParameters = make(map[string]any, len(parameters))
	for name := range parameters {
		redactedRequest.TemplateParameters[name] = "REDACTED"
	}
	if variables != nil {
		redactedRequest.Variables = make(map[string]PipelineVariable, len(variables))
		for name, variable := range variables {
			variable.Value = "REDACTED"
			redactedRequest.Variables[name] = variable
		}
	}
	redactedPayload, err := json.Marshal(redactedRequest)
	if err != nil {
		return nil, fmt.Errorf("failed to encode redacted pipeline run request: %w", err)
	}

	endpoint := fmt.Sprintf("/pipelines/%d/runs?api-version=7.1", pipelineID)
	body, err := c.post(endpoint, strings.NewReader(string(payload)))
	if err != nil {
		return nil, fmt.Errorf(
			"failed to queue YAML pipeline: %w\nRequest: POST %s (Content-Type: application/json, body %d bytes)\nRedacted request body: %s\nPipeline metadata check: %s",
			err, endpoint, len(payload), redactedPayload, c.pipelineRunMetadataDiagnostic(pipelineID),
		)
	}
	var response struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse queued YAML pipeline run: %w", err)
	}
	return &PipelineRun{ID: response.ID, BuildNumber: response.Name, SourceBranch: sourceBranch}, nil
}

func encodeTemplateParameters(parameters map[string]any) (map[string]any, error) {
	encoded := make(map[string]any, len(parameters))
	for name, value := range parameters {
		switch value.(type) {
		case map[string]any, []any:
			contents, err := yaml.Marshal(value)
			if err != nil {
				return nil, fmt.Errorf("parameter %q: %w", name, err)
			}
			encoded[name] = strings.TrimSpace(string(contents))
		default:
			encoded[name] = value
		}
	}
	return encoded, nil
}

func (c *Client) pipelineRunMetadataDiagnostic(pipelineID int) string {
	endpoint := fmt.Sprintf("/pipelines/%d?api-version=7.1", pipelineID)
	body, err := c.get(endpoint)
	if err != nil {
		return fmt.Sprintf("GET %s failed: %v", endpoint, err)
	}
	var response struct {
		ID            int    `json:"id"`
		Name          string `json:"name"`
		Configuration *struct {
			Type       string `json:"type"`
			Path       string `json:"path"`
			Repository struct {
				ID   string `json:"id"`
				Type string `json:"type"`
			} `json:"repository"`
		} `json:"configuration"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return fmt.Sprintf("GET %s returned unparseable metadata: %v", endpoint, err)
	}
	if response.Configuration == nil {
		return fmt.Sprintf("GET %s returned pipeline id=%d name=%q with no configuration", endpoint, response.ID, response.Name)
	}
	return fmt.Sprintf(
		"GET %s returned pipeline id=%d name=%q configuration.type=%q configuration.path=%q repository.id=%q repository.type=%q",
		endpoint,
		response.ID,
		response.Name,
		response.Configuration.Type,
		response.Configuration.Path,
		response.Configuration.Repository.ID,
		response.Configuration.Repository.Type,
	)
}

// GetYAMLPipelineRunParameters returns the template parameters captured by a
// YAML pipeline run.
func (c *Client) GetYAMLPipelineRunParameters(pipelineID, runID int) (map[string]any, error) {
	if pipelineID <= 0 || runID <= 0 {
		return nil, fmt.Errorf("pipeline and run IDs must be positive")
	}
	endpoint := fmt.Sprintf("/pipelines/%d/runs/%d?api-version=7.1", pipelineID, runID)
	body, err := c.get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to get YAML pipeline run %d parameters: %w", runID, err)
	}
	var response struct {
		TemplateParameters map[string]any `json:"templateParameters"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse YAML pipeline run parameters: %w", err)
	}
	if response.TemplateParameters == nil {
		response.TemplateParameters = make(map[string]any)
	}
	return response.TemplateParameters, nil
}

type repositoriesResponse struct {
	Value []Repository `json:"value"`
}

// ListRepositories returns the Git repositories in the project.
func (c *Client) ListRepositories() ([]Repository, error) {
	body, err := c.get("/git/repositories?api-version=7.1")
	if err != nil {
		return nil, fmt.Errorf("failed to list repositories: %w", err)
	}

	var response repositoriesResponse
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse Azure DevOps repositories: %w", err)
	}
	return response.Value, nil
}

// ListAgentQueues returns queues the user can use in the project.
func (c *Client) ListAgentQueues() ([]AgentPoolQueue, error) {
	body, err := c.get("/distributedtask/queues?actionFilter=use&api-version=7.1")
	if err != nil {
		return nil, fmt.Errorf("failed to list agent queues: %w", err)
	}
	var response struct {
		Value []AgentPoolQueue `json:"value"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse agent queues: %w", err)
	}
	for i, queue := range response.Value {
		if queue.ID <= 0 || strings.TrimSpace(queue.Name) == "" || queue.Pool.ID <= 0 {
			return nil, fmt.Errorf("agent queue %d is missing its ID, name, or pool", i)
		}
	}
	sort.Slice(response.Value, func(i, j int) bool {
		if response.Value[i].Name != response.Value[j].Name {
			return strings.ToLower(response.Value[i].Name) < strings.ToLower(response.Value[j].Name)
		}
		return strings.ToLower(response.Value[i].Pool.Name) < strings.ToLower(response.Value[j].Pool.Name)
	})
	return response.Value, nil
}

// ListBranches returns branch names in a repository without the refs/heads/ prefix.
func (c *Client) ListBranches(repositoryID string) ([]string, error) {
	if strings.TrimSpace(repositoryID) == "" {
		return nil, fmt.Errorf("repository is required")
	}
	endpoint := "/git/repositories/" + url.PathEscape(repositoryID) + "/refs?filter=heads/&$top=1000&api-version=7.1"
	body, err := c.get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to list repository branches: %w", err)
	}
	var response struct {
		Value []struct {
			Name string `json:"name"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse repository branches: %w", err)
	}

	branches := make([]string, 0, len(response.Value))
	for _, ref := range response.Value {
		name := strings.TrimPrefix(ref.Name, "refs/heads/")
		if name != ref.Name && strings.TrimSpace(name) != "" {
			branches = append(branches, name)
		}
	}
	sort.Strings(branches)
	return branches, nil
}

// ListYAMLPipelineFiles returns YAML files found recursively in a repository
// at the requested branch.
func (c *Client) ListYAMLPipelineFiles(repositoryID, branch string) ([]string, error) {
	if strings.TrimSpace(repositoryID) == "" {
		return nil, fmt.Errorf("repository is required")
	}
	branchName := strings.TrimPrefix(strings.TrimSpace(branch), "refs/heads/")
	if branchName == "" {
		return nil, fmt.Errorf("repository default branch is unavailable")
	}
	query := url.Values{}
	query.Set("scopePath", "/")
	query.Set("recursionLevel", "Full")
	query.Set("versionDescriptor.version", branchName)
	query.Set("versionDescriptor.versionType", "branch")
	query.Set("api-version", "7.1")
	endpoint := "/git/repositories/" + url.PathEscape(repositoryID) + "/items?" + query.Encode()
	body, err := c.get(endpoint)
	if err != nil {
		return nil, fmt.Errorf("failed to list repository YAML files: %w", err)
	}
	var response struct {
		Value []struct {
			Path          string `json:"path"`
			IsFolder      bool   `json:"isFolder"`
			GitObjectType string `json:"gitObjectType"`
		} `json:"value"`
	}
	if err := json.Unmarshal(body, &response); err != nil {
		return nil, fmt.Errorf("failed to parse repository items: %w", err)
	}
	var files []string
	for _, item := range response.Value {
		if item.IsFolder || (item.GitObjectType != "" && item.GitObjectType != "blob") {
			continue
		}
		extension := strings.ToLower(path.Ext(item.Path))
		if extension == ".yml" || extension == ".yaml" {
			files = append(files, item.Path)
		}
	}
	sort.Strings(files)
	return files, nil
}

// GetRepositoryFile reads one repository file at a branch using the Git Items API.
func (c *Client) GetRepositoryFile(repositoryID, filePath, branch string) (string, error) {
	if strings.TrimSpace(repositoryID) == "" || strings.TrimSpace(filePath) == "" {
		return "", fmt.Errorf("repository and file path are required")
	}
	branchName := strings.TrimPrefix(strings.TrimSpace(branch), "refs/heads/")
	if branchName == "" {
		return "", fmt.Errorf("repository branch is required")
	}
	query := url.Values{}
	query.Set("path", filePath)
	query.Set("includeContent", "true")
	query.Set("versionDescriptor.version", branchName)
	query.Set("versionDescriptor.versionType", "branch")
	query.Set("api-version", "7.1")
	query.Set("$format", "json")
	endpoint := "/git/repositories/" + url.PathEscape(repositoryID) + "/items?" + query.Encode()
	body, err := c.get(endpoint)
	if err != nil {
		return "", fmt.Errorf("failed to read repository file: %w", err)
	}
	var item struct {
		Content string `json:"content"`
	}
	if err := json.Unmarshal(body, &item); err != nil {
		return "", fmt.Errorf("failed to parse repository file response: %w", err)
	}
	return item.Content, nil
}

// CreateYAMLPipelineDefinition creates a YAML build definition with its
// initial variables and optional agent queue in one Azure DevOps request.
func (c *Client) CreateYAMLPipelineDefinition(name string, repository Repository, yamlPath string, variables map[string]PipelineVariable, definitionPath string, queue *AgentPoolQueue) (*PipelineDefinition, error) {
	name = strings.TrimSpace(name)
	yamlPath = strings.TrimSpace(yamlPath)
	definitionPath = strings.TrimSpace(definitionPath)
	if name == "" {
		return nil, fmt.Errorf("pipeline name is required")
	}
	if strings.TrimSpace(repository.ID) == "" || strings.TrimSpace(repository.Name) == "" {
		return nil, fmt.Errorf("repository ID and name are required")
	}
	if yamlPath == "" || (strings.ToLower(path.Ext(yamlPath)) != ".yml" && strings.ToLower(path.Ext(yamlPath)) != ".yaml") {
		return nil, fmt.Errorf("pipeline YAML path must end in .yml or .yaml")
	}
	if definitionPath == "" {
		definitionPath = `\`
	}
	if queue != nil && (queue.ID <= 0 || strings.TrimSpace(queue.Name) == "" || queue.Pool.ID <= 0) {
		return nil, fmt.Errorf("agent queue ID, name, and pool ID are required")
	}
	branch, err := branchRef(repository.DefaultBranch)
	if err != nil {
		return nil, fmt.Errorf("repository default branch: %w", err)
	}
	if repository.Type == "" {
		repository.Type = "TfsGit"
	}
	repositoryURL := repository.RemoteURL
	if repositoryURL == "" {
		repositoryURL = strings.TrimSuffix(c.baseURL, "/_apis") + "/_git/" + url.PathEscape(repository.Name)
	}
	payload, err := json.Marshal(struct {
		Name        string                      `json:"name"`
		Path        string                      `json:"path"`
		Type        string                      `json:"type"`
		QueueStatus string                      `json:"queueStatus"`
		Process     PipelineProcess             `json:"process"`
		Repository  PipelineRepository          `json:"repository"`
		Variables   map[string]PipelineVariable `json:"variables,omitempty"`
		Queue       *AgentPoolQueue             `json:"queue,omitempty"`
	}{
		Name:        name,
		Path:        definitionPath,
		Type:        "build",
		QueueStatus: "enabled",
		Process:     PipelineProcess{Type: 2, YAMLFileName: yamlPath},
		Repository: PipelineRepository{
			ID: repository.ID, Name: repository.Name, Type: repository.Type, DefaultBranch: branch,
			URL: repositoryURL, Clean: "true",
		},
		Variables: variables,
		Queue:     queue,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to encode pipeline definition: %w", err)
	}
	body, err := c.post("/build/definitions?api-version=7.1", strings.NewReader(string(payload)))
	if err != nil {
		return nil, fmt.Errorf("failed to create YAML pipeline: %w", err)
	}
	var definition PipelineDefinition
	if err := json.Unmarshal(body, &definition); err != nil {
		return nil, fmt.Errorf("failed to parse created pipeline definition: %w", err)
	}
	return &definition, nil
}

// CompletePullRequest completes an Azure DevOps pull request with the selected
// merge strategy. Secret policy bypass and source-branch deletion are disabled.
func (c *Client) CompletePullRequest(repositoryID string, pullRequestID int, strategy provider.PRMergeStrategy) error {
	if strings.TrimSpace(repositoryID) == "" || pullRequestID <= 0 {
		return fmt.Errorf("repository and positive pull request ID are required")
	}

	var mergeStrategy string
	switch strategy {
	case provider.PRMergeFastForward:
		mergeStrategy = "rebase"
	case provider.PRMergeSquash:
		mergeStrategy = "squash"
	default:
		return fmt.Errorf("unsupported pull request merge strategy %q", strategy)
	}

	endpoint := fmt.Sprintf("/git/repositories/%s/pullRequests/%d?api-version=7.1",
		url.PathEscape(repositoryID), pullRequestID)
	body, err := c.get(endpoint)
	if err != nil {
		return fmt.Errorf("failed to load pull request before completion: %w", err)
	}
	var pullRequest PullRequest
	if err := json.Unmarshal(body, &pullRequest); err != nil {
		return fmt.Errorf("failed to parse pull request before completion: %w", err)
	}
	if pullRequest.LastMergeSourceCommit == nil || strings.TrimSpace(pullRequest.LastMergeSourceCommit.CommitID) == "" {
		return fmt.Errorf("pull request has no source commit to complete")
	}

	payload, err := json.Marshal(struct {
		Status                string `json:"status"`
		LastMergeSourceCommit struct {
			CommitID string `json:"commitId"`
		} `json:"lastMergeSourceCommit"`
		CompletionOptions struct {
			MergeStrategy       string `json:"mergeStrategy"`
			DeleteSourceBranch  bool   `json:"deleteSourceBranch"`
			TransitionWorkItems bool   `json:"transitionWorkItems"`
		} `json:"completionOptions"`
	}{
		Status: "completed",
		LastMergeSourceCommit: struct {
			CommitID string `json:"commitId"`
		}{CommitID: pullRequest.LastMergeSourceCommit.CommitID},
		CompletionOptions: struct {
			MergeStrategy       string `json:"mergeStrategy"`
			DeleteSourceBranch  bool   `json:"deleteSourceBranch"`
			TransitionWorkItems bool   `json:"transitionWorkItems"`
		}{MergeStrategy: mergeStrategy},
	})
	if err != nil {
		return fmt.Errorf("failed to encode pull request completion: %w", err)
	}
	if _, err := c.patch(endpoint, strings.NewReader(string(payload))); err != nil {
		return fmt.Errorf("failed to complete pull request: %w", err)
	}
	return nil
}

// AbandonPullRequest abandons an Azure DevOps pull request.
func (c *Client) AbandonPullRequest(repositoryID string, pullRequestID int) error {
	if strings.TrimSpace(repositoryID) == "" || pullRequestID <= 0 {
		return fmt.Errorf("repository and positive pull request ID are required")
	}

	endpoint := fmt.Sprintf("/git/repositories/%s/pullRequests/%d?api-version=7.1",
		url.PathEscape(repositoryID), pullRequestID)
	payload, err := json.Marshal(struct {
		Status string `json:"status"`
	}{Status: "abandoned"})
	if err != nil {
		return fmt.Errorf("failed to encode pull request abandonment: %w", err)
	}
	if _, err := c.patch(endpoint, strings.NewReader(string(payload))); err != nil {
		return fmt.Errorf("failed to abandon pull request: %w", err)
	}
	return nil
}

// CreatePullRequest creates a pull request for the given repository and branches.
func (c *Client) CreatePullRequest(repositoryID, title, description, sourceBranch, targetBranch string, draft bool) (*PullRequest, error) {
	if strings.TrimSpace(repositoryID) == "" {
		return nil, fmt.Errorf("repository is required")
	}
	if strings.TrimSpace(title) == "" {
		return nil, fmt.Errorf("pull request title is required")
	}
	sourceRef, err := branchRef(sourceBranch)
	if err != nil {
		return nil, fmt.Errorf("source branch: %w", err)
	}
	targetRef, err := branchRef(targetBranch)
	if err != nil {
		return nil, fmt.Errorf("target branch: %w", err)
	}

	payload, err := json.Marshal(struct {
		SourceRefName string `json:"sourceRefName"`
		TargetRefName string `json:"targetRefName"`
		Title         string `json:"title"`
		Description   string `json:"description,omitempty"`
		IsDraft       bool   `json:"isDraft"`
	}{
		SourceRefName: sourceRef,
		TargetRefName: targetRef,
		Title:         title,
		Description:   description,
		IsDraft:       draft,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to encode pull request: %w", err)
	}

	path := "/git/repositories/" + url.PathEscape(repositoryID) + "/pullrequests?api-version=7.1"
	body, err := c.post(path, strings.NewReader(string(payload)))
	if err != nil {
		return nil, fmt.Errorf("failed to create pull request: %w", err)
	}

	var pr PullRequest
	if err := json.Unmarshal(body, &pr); err != nil {
		return nil, fmt.Errorf("failed to parse created pull request: %w", err)
	}
	return &pr, nil
}

// UpdatePullRequestDescription updates the description of an existing pull request.
func (c *Client) UpdatePullRequestDescription(repositoryID string, pullRequestID int, description string) error {
	return c.updatePullRequestField(repositoryID, pullRequestID, struct {
		Description string `json:"description"`
	}{Description: description}, "description")
}

// UpdatePullRequestTitle updates the title (summary) of an existing pull request.
func (c *Client) UpdatePullRequestTitle(repositoryID string, pullRequestID int, title string) error {
	if strings.TrimSpace(title) == "" {
		return fmt.Errorf("pull request title is required")
	}
	return c.updatePullRequestField(repositoryID, pullRequestID, struct {
		Title string `json:"title"`
	}{Title: title}, "title")
}

func (c *Client) updatePullRequestField(repositoryID string, pullRequestID int, payload any, field string) error {
	if strings.TrimSpace(repositoryID) == "" {
		return fmt.Errorf("repository is required")
	}
	if pullRequestID <= 0 {
		return fmt.Errorf("pull request ID must be positive")
	}

	encoded, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to encode pull request %s: %w", field, err)
	}
	path := "/git/repositories/" + url.PathEscape(repositoryID) +
		"/pullrequests/" + strconv.Itoa(pullRequestID) + "?api-version=7.1"
	if _, err := c.patch(path, strings.NewReader(string(encoded))); err != nil {
		return fmt.Errorf("failed to update pull request %s: %w", field, err)
	}
	return nil
}

func branchRef(branch string) (string, error) {
	branch = strings.TrimSpace(branch)
	if branch == "" {
		return "", fmt.Errorf("branch is required")
	}
	if strings.HasPrefix(branch, "refs/heads/") {
		if strings.TrimSpace(strings.TrimPrefix(branch, "refs/heads/")) == "" {
			return "", fmt.Errorf("branch name cannot be empty")
		}
		return branch, nil
	}
	if strings.HasPrefix(branch, "refs/") {
		return "", fmt.Errorf("branch must be a branch name or refs/heads/... value")
	}
	return "refs/heads/" + branch, nil
}
