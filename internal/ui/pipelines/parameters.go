package pipelines

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

type runtimeParameter struct {
	Name    string `yaml:"name"`
	Type    string `yaml:"type"`
	Default any    `yaml:"default"`
	Values  []any  `yaml:"values"`
}

func parseRuntimeParameters(contents string) ([]runtimeParameter, error) {
	var document struct {
		Parameters []runtimeParameter `yaml:"parameters"`
	}
	if err := yaml.Unmarshal([]byte(contents), &document); err != nil {
		return nil, fmt.Errorf("parse YAML runtime parameters: %w", err)
	}
	seen := make(map[string]struct{}, len(document.Parameters))
	parameters := make([]runtimeParameter, 0, len(document.Parameters))
	for _, parameter := range document.Parameters {
		parameter.Name = strings.TrimSpace(parameter.Name)
		if parameter.Name == "" {
			return nil, fmt.Errorf("YAML runtime parameter is missing its name")
		}
		if _, exists := seen[parameter.Name]; exists {
			return nil, fmt.Errorf("duplicate YAML runtime parameter %q", parameter.Name)
		}
		seen[parameter.Name] = struct{}{}
		if parameter.Type == "" {
			parameter.Type = "string"
		}
		parameters = append(parameters, parameter)
	}
	return parameters, nil
}

func parseYAMLVariableNames(contents string) ([]string, error) {
	var document struct {
		Variables any `yaml:"variables"`
	}
	if err := yaml.Unmarshal([]byte(contents), &document); err != nil {
		return nil, fmt.Errorf("parse YAML variables: %w", err)
	}

	names := make(map[string]string)
	addName := func(name string) {
		name = strings.TrimSpace(name)
		if name != "" {
			names[strings.ToLower(name)] = name
		}
	}
	switch variables := document.Variables.(type) {
	case nil:
	case map[string]any:
		for name := range variables {
			addName(name)
		}
	case []any:
		for _, item := range variables {
			entry, ok := item.(map[string]any)
			if !ok {
				continue
			}
			if _, isGroup := entry["group"]; isGroup {
				continue
			}
			if _, isTemplate := entry["template"]; isTemplate {
				continue
			}
			if name, ok := entry["name"].(string); ok {
				addName(name)
			}
		}
	default:
		return nil, fmt.Errorf("YAML variables must be a mapping or list")
	}

	result := make([]string, 0, len(names))
	for _, name := range names {
		result = append(result, name)
	}
	sort.Slice(result, func(i, j int) bool {
		return strings.ToLower(result[i]) < strings.ToLower(result[j])
	})
	return result, nil
}

func parameterText(value any, parameterType string) string {
	if value == nil {
		return ""
	}
	if parameterType == "string" {
		if text, ok := value.(string); ok {
			return text
		}
	}
	contents, err := yaml.Marshal(value)
	if err != nil {
		return fmt.Sprint(value)
	}
	return strings.TrimSpace(string(contents))
}

func parseParameterOverride(text string, parameter runtimeParameter) (any, error) {
	var value any
	if parameter.Type == "string" {
		value = text
	} else if err := yaml.Unmarshal([]byte(text), &value); err != nil {
		return nil, fmt.Errorf("invalid %s value: %w", parameter.Type, err)
	}
	if !parameterValueMatchesType(value, parameter.Type) {
		return nil, fmt.Errorf("value must be %s", parameter.Type)
	}
	if len(parameter.Values) > 0 {
		allowed := false
		for _, candidate := range parameter.Values {
			if reflect.DeepEqual(value, candidate) {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, fmt.Errorf("value must be one of: %s", joinParameterValues(parameter.Values, parameter.Type))
		}
	}
	return value, nil
}

func parameterValueMatchesType(value any, parameterType string) bool {
	switch parameterType {
	case "string":
		_, ok := value.(string)
		return ok
	case "boolean":
		_, ok := value.(bool)
		return ok
	case "number":
		switch value.(type) {
		case int, int64, float64:
			return true
		default:
			return false
		}
	case "object", "step", "stepList", "job", "jobList", "deployment", "deploymentList", "stage", "stageList":
		switch value.(type) {
		case map[string]any, []any:
			return true
		default:
			return false
		}
	default:
		return true
	}
}

func joinParameterValues(values []any, parameterType string) string {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, parameterText(value, parameterType))
	}
	return strings.Join(parts, ", ")
}

func pipelineParameterMap(parameters []runtimeParameter, overrides map[string]any) map[string]any {
	result := make(map[string]any, len(overrides))
	for _, parameter := range parameters {
		if value, exists := overrides[parameter.Name]; exists {
			result[parameter.Name] = value
		}
	}
	return result
}
