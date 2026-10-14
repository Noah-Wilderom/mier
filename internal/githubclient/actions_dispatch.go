package githubclient

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

type WorkflowInput struct {
	Name        string   `json:"name"`
	Description string   `json:"description"`
	Type        string   `json:"type"`
	Required    bool     `json:"required"`
	Default     string   `json:"default"`
	Options     []string `json:"options"`
}
type WorkflowDispatch struct {
	Workflow Workflow        `json:"workflow"`
	Ref      string          `json:"ref"`
	Enabled  bool            `json:"enabled"`
	Inputs   []WorkflowInput `json:"inputs"`
}
type WorkflowDispatchRequest struct {
	WorkflowID int64             `json:"workflowId"`
	Ref        string            `json:"ref"`
	Inputs     map[string]string `json:"inputs"`
}

func yamlField(node *yaml.Node, key string) *yaml.Node {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil
	}
	for i := 0; i+1 < len(node.Content); i += 2 {
		if node.Content[i].Value == key {
			return node.Content[i+1]
		}
	}
	return nil
}
func parseDispatchInputs(data []byte) (bool, []WorkflowInput, error) {
	var document yaml.Node
	if err := yaml.Unmarshal(data, &document); err != nil || len(document.Content) != 1 {
		return false, nil, errors.New("Unable to read this workflow's dispatch inputs")
	}
	on := yamlField(document.Content[0], "on")
	if on == nil {
		return false, []WorkflowInput{}, nil
	}
	if on.Kind == yaml.ScalarNode {
		return on.Value == "workflow_dispatch", []WorkflowInput{}, nil
	}
	if on.Kind == yaml.SequenceNode {
		for _, item := range on.Content {
			if item.Value == "workflow_dispatch" {
				return true, []WorkflowInput{}, nil
			}
		}
		return false, []WorkflowInput{}, nil
	}
	dispatch := yamlField(on, "workflow_dispatch")
	if dispatch == nil {
		return false, []WorkflowInput{}, nil
	}
	inputs := yamlField(dispatch, "inputs")
	result := []WorkflowInput{}
	if inputs == nil {
		return true, result, nil
	}
	if inputs.Kind != yaml.MappingNode || len(inputs.Content) > 50 {
		return false, nil, errors.New("Unsupported workflow inputs; run this workflow on GitHub")
	}
	for i := 0; i+1 < len(inputs.Content); i += 2 {
		name, node := inputs.Content[i].Value, inputs.Content[i+1]
		if node.Kind != yaml.MappingNode {
			return false, nil, errors.New("Unsupported workflow input definition; run this workflow on GitHub")
		}
		input := WorkflowInput{Name: name, Type: "string", Options: []string{}}
		if value := yamlField(node, "description"); value != nil {
			input.Description = value.Value
		}
		if value := yamlField(node, "type"); value != nil {
			input.Type = value.Value
		}
		if value := yamlField(node, "required"); value != nil {
			input.Required = value.Value == "true"
		}
		if value := yamlField(node, "default"); value != nil {
			if value.Kind != yaml.ScalarNode {
				return false, nil, errors.New("Unsupported workflow input default")
			}
			input.Default = value.Value
		}
		if value := yamlField(node, "options"); value != nil {
			if value.Kind != yaml.SequenceNode {
				return false, nil, errors.New("Unsupported workflow input options")
			}
			for _, option := range value.Content {
				if option.Kind != yaml.ScalarNode {
					return false, nil, errors.New("Unsupported workflow input choice")
				}
				input.Options = append(input.Options, option.Value)
			}
		}
		switch input.Type {
		case "string", "boolean", "number", "choice", "environment":
		default:
			return false, nil, fmt.Errorf("Unsupported type for input %s; run this workflow on GitHub", name)
		}
		result = append(result, input)
	}
	return true, result, nil
}
func (c *Client) GetWorkflowDispatch(ctx context.Context, owner, repo string, workflowID int64, reference string) (WorkflowDispatch, error) {
	result := WorkflowDispatch{Inputs: []WorkflowInput{}}
	path, err := actionPath(owner, repo, "workflows", workflowID)
	if err != nil {
		return result, err
	}
	_, err = c.request(ctx, http.MethodGet, path, nil, &result.Workflow)
	if err != nil {
		return result, actionsError(err)
	}
	base, _ := repoPath(owner, repo)
	if reference == "" {
		var metadata struct {
			DefaultBranch string `json:"default_branch"`
		}
		_, err = c.request(ctx, http.MethodGet, base, nil, &metadata)
		if err != nil {
			return result, actionsError(err)
		}
		reference = metadata.DefaultBranch
	}
	if reference == "" || len(reference) > 1024 {
		return result, errors.New("Choose a branch or tag to run this workflow")
	}
	result.Ref = reference
	if result.Workflow.State != "active" {
		return result, nil
	}
	file := result.Workflow.Path
	if !strings.HasPrefix(file, ".github/workflows/") || strings.Contains(file, "..") || strings.ContainsAny(file, "?#\\") {
		return result, errors.New("This workflow definition is not available in the repository")
	}
	segments := strings.Split(file, "/")
	for i := range segments {
		segments[i] = url.PathEscape(segments[i])
	}
	var content struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
		Size     int    `json:"size"`
	}
	_, err = c.request(ctx, http.MethodGet, base+"/contents/"+strings.Join(segments, "/")+"?ref="+url.QueryEscape(reference), nil, &content)
	if err != nil {
		return result, actionsError(err)
	}
	if content.Encoding != "base64" || content.Size > 1024*1024 || len(content.Content) > 2*1024*1024 {
		return result, errors.New("Workflow definition is too large to read; run it on GitHub")
	}
	data, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(content.Content, "\n", ""))
	if err != nil {
		return result, errors.New("Unable to read the workflow definition")
	}
	result.Enabled, result.Inputs, err = parseDispatchInputs(data)
	return result, err
}
func validateDispatchInputs(schema []WorkflowInput, values map[string]string) (map[string]string, error) {
	result := map[string]string{}
	known := map[string]bool{}
	for _, input := range schema {
		known[input.Name] = true
		value, provided := values[input.Name]
		if !provided {
			value = input.Default
		}
		if input.Type == "boolean" && value == "" {
			value = "false"
		}
		if value == "" {
			if input.Required {
				return nil, fmt.Errorf("Input %s is required", input.Name)
			}
			continue
		}
		switch input.Type {
		case "boolean":
			if value != "true" && value != "false" {
				return nil, fmt.Errorf("Input %s must be true or false", input.Name)
			}
		case "number":
			number, err := strconv.ParseFloat(value, 64)
			if err != nil || math.IsNaN(number) || math.IsInf(number, 0) {
				return nil, fmt.Errorf("Input %s must be a number", input.Name)
			}
		case "choice":
			found := false
			for _, option := range input.Options {
				if value == option {
					found = true
				}
			}
			if !found {
				return nil, fmt.Errorf("Choose a valid option for %s", input.Name)
			}
		}
		result[input.Name] = value
	}
	for name := range values {
		if !known[name] {
			return nil, fmt.Errorf("The workflow inputs changed. Reload inputs before running it (%s)", name)
		}
	}
	data, _ := json.Marshal(result)
	if len(data) > 64000 {
		return nil, errors.New("Workflow inputs exceed GitHub's payload limit")
	}
	return result, nil
}
func (c *Client) DispatchWorkflow(ctx context.Context, owner, repo string, input WorkflowDispatchRequest) error {
	if strings.TrimSpace(input.Ref) == "" {
		return errors.New("Choose a branch or tag to run this workflow")
	}
	config, err := c.GetWorkflowDispatch(ctx, owner, repo, input.WorkflowID, input.Ref)
	if err != nil {
		return err
	}
	if !config.Enabled {
		return errors.New("This workflow is disabled or does not support manual runs on the selected branch/tag")
	}
	values, err := validateDispatchInputs(config.Inputs, input.Inputs)
	if err != nil {
		return err
	}
	path, _ := actionPath(owner, repo, "workflows", input.WorkflowID)
	_, err = c.request(ctx, http.MethodPost, path+"/dispatches", map[string]any{"ref": input.Ref, "inputs": values}, nil)
	return actionsError(err)
}
