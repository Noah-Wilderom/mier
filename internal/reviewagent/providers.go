package reviewagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"net/http"
	"os/exec"
	"sync"
	"time"

	copilot "github.com/github/copilot-sdk/go"
	"github.com/github/copilot-sdk/go/rpc"
)

type Model struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Default          bool     `json:"default"`
	ReasoningEfforts []string `json:"reasoningEfforts,omitempty"`
}
type ProviderStatus struct {
	Provider      string  `json:"provider"`
	Installed     bool    `json:"installed"`
	Executable    string  `json:"executable"`
	Authenticated bool    `json:"authenticated"`
	Account       string  `json:"account"`
	Plan          string  `json:"plan"`
	Models        []Model `json:"models"`
	Error         string  `json:"error"`
}

func CodexStatus(ctx context.Context, client *Codex) (ProviderStatus, error) {
	result := ProviderStatus{Provider: "codex", Installed: true, Models: []Model{}}
	var response struct {
		Account *struct {
			Type  string `json:"type"`
			Email string `json:"email"`
			Plan  string `json:"planType"`
		} `json:"account"`
	}
	if err := client.Call(ctx, "account/read", map[string]any{"refreshToken": false}, &response); err != nil {
		return result, err
	}
	if response.Account != nil {
		result.Authenticated = response.Account.Type == "chatgpt"
		result.Account = response.Account.Email
		result.Plan = response.Account.Plan
		if !result.Authenticated {
			result.Error = "Sign in with ChatGPT in Codex to use your subscription for reviews."
		}
	}
	if !result.Authenticated {
		return result, nil
	}
	cursor := ""
	for page := 0; page < 5; page++ {
		var models struct {
			Data []struct {
				ID, Model, DisplayName    string
				IsDefault                 bool
				SupportedReasoningEfforts []struct {
					ReasoningEffort string `json:"reasoningEffort"`
				} `json:"supportedReasoningEfforts"`
			} `json:"data"`
			NextCursor *string `json:"nextCursor"`
		}
		params := map[string]any{"limit": 100}
		if cursor != "" {
			params["cursor"] = cursor
		}
		if err := client.Call(ctx, "model/list", params, &models); err != nil {
			return result, err
		}
		for _, model := range models.Data {
			id := model.Model
			if id == "" {
				id = model.ID
			}
			if id != "" {
				efforts := []string{}
				for _, effort := range model.SupportedReasoningEfforts {
					efforts = append(efforts, effort.ReasoningEffort)
				}
				result.Models = append(result.Models, Model{ID: id, Name: model.DisplayName, Default: model.IsDefault, ReasoningEfforts: efforts})
			}
		}
		if models.NextCursor == nil || *models.NextCursor == "" {
			break
		}
		cursor = *models.NextCursor
	}
	return result, nil
}

// User MCP entries merge with thread config rather than being replaced. Disable
// them by name so a code review cannot inherit unrelated tools or credentials.
func CodexDisabledServers(ctx context.Context, binary, directory string) (map[string]any, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "-c", "features.plugins=false", "-c", "features.apps=false", "mcp", "list", "--json")
	command.Dir = directory
	command.Stderr = nil
	data, err := command.Output()
	if err != nil {
		return nil, errors.New("Could not inspect Codex MCP configuration. Update Codex and retry")
	}
	if len(data) > 1024*1024 {
		return nil, errors.New("Codex MCP configuration exceeds the supported size")
	}
	var entries []struct {
		Name string `json:"name"`
	}
	if json.Unmarshal(data, &entries) != nil {
		return nil, errors.New("Codex returned an unsupported MCP configuration")
	}
	servers := map[string]any{}
	for _, entry := range entries {
		if entry.Name != "" {
			servers[entry.Name] = map[string]bool{"enabled": false}
		}
	}
	return servers, nil
}

func RunCodex(ctx context.Context, binary, directory, version, model, prompt string, bridge *Bridge, run *Run) error {
	servers, err := CodexDisabledServers(ctx, binary, directory)
	if err != nil {
		return err
	}
	// Clicking Start review authorizes only these additive in-app draft tools.
	// All command/file approvals remain disabled and unrelated MCP entries off.
	toolPolicy := map[string]any{}
	for _, tool := range ToolNames {
		toolPolicy[tool] = map[string]string{"approval_mode": "approve"}
	}
	servers["mier"] = map[string]any{"enabled": true, "required": true, "url": bridge.URL, "http_headers": map[string]string{"Authorization": "Bearer " + bridge.Token}, "enabled_tools": ToolNames, "tools": toolPolicy, "startup_timeout_sec": 15, "tool_timeout_sec": 40}
	completion := make(chan struct {
		Status string
		Error  *struct{ Message string }
	}, 1)
	var textMu sync.Mutex
	finalText := ""
	reviewThread := ""
	client, err := NewCodex(ctx, binary, directory, version, func(method string, params json.RawMessage) {
		switch method {
		case "thread/tokenUsage/updated":
			textMu.Lock()
			id := reviewThread
			textMu.Unlock()
			run.recordCodexUsage(params, id)
		case "turn/completed":
			var event struct {
				Turn struct {
					Status string
					Error  *struct{ Message string }
				}
			}
			if json.Unmarshal(params, &event) == nil {
				select {
				case completion <- event.Turn:
				default:
				}
			}
		case "item/completed":
			var event struct{ Item struct{ Type, Text string } }
			if json.Unmarshal(params, &event) == nil && event.Item.Type == "agentMessage" {
				textMu.Lock()
				finalText = bounded(event.Item.Text, 32000)
				textMu.Unlock()
			}
		}
	})
	if err != nil {
		return err
	}
	defer client.Close()
	status, err := CodexStatus(ctx, client)
	if err != nil {
		return err
	}
	if !status.Authenticated {
		return errors.New("Sign in with ChatGPT in AI settings before starting a Codex review")
	}
	params := map[string]any{"cwd": directory, "approvalPolicy": "never", "sandbox": "read-only", "ephemeral": true, "developerInstructions": Instructions, "config": map[string]any{"mcp_servers": servers, "mcp_optional_startup_grace_ms": 15000}}
	if model != "" {
		params["model"] = model
	}
	var thread struct{ Thread struct{ ID string } }
	if err := client.Call(ctx, "thread/start", params, &thread); err != nil {
		return err
	}
	if thread.Thread.ID == "" {
		return errors.New("Codex did not create a review thread")
	}
	textMu.Lock()
	reviewThread = thread.Thread.ID
	textMu.Unlock()
	run.Progress("Reviewing captured pull request changes")
	var turn struct{ Turn struct{ ID string } }
	turnParams := map[string]any{"threadId": thread.Thread.ID, "input": []map[string]string{{"type": "text", "text": prompt}}}
	if effort := ModelEffort(status.Models, model, run.Snapshot().Intensity); effort != "" {
		turnParams["effort"] = effort
	}
	if err := client.Call(ctx, "turn/start", turnParams, &turn); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		interrupt, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		_ = client.Call(interrupt, "turn/interrupt", map[string]string{"threadId": thread.Thread.ID, "turnId": turn.Turn.ID}, nil)
		return ctx.Err()
	case <-client.done:
		return errors.New("Codex exited before completing the review")
	case result := <-completion:
		if result.Status != "completed" {
			if result.Error != nil {
				return fmt.Errorf("Codex review failed: %s", result.Error.Message)
			}
			return fmt.Errorf("Codex review ended with status %s", result.Status)
		}
		if run.Snapshot().Summary == "" {
			textMu.Lock()
			defer textMu.Unlock()
			_ = run.Summary(finalText)
		}
		return nil
	}
}

func NewCopilot(binary, directory, token string) *copilot.Client {
	return copilot.NewClient(&copilot.ClientOptions{Connection: copilot.StdioConnection{Path: binary, Args: []string{"--disable-builtin-mcps", "--no-remote", "--no-remote-export", "--log-level", "none"}}, WorkingDirectory: directory, BaseDirectory: directory, Mode: copilot.ModeEmpty, GitHubToken: token, UseLoggedInUser: copilot.Bool(false)})
}
func CopilotStatus(ctx context.Context, client *copilot.Client) (ProviderStatus, error) {
	result := ProviderStatus{Provider: "copilot", Installed: true, Models: []Model{}}
	if err := client.Start(ctx); err != nil {
		return result, err
	}
	account, err := client.GetAuthStatus(ctx)
	if err != nil {
		return result, err
	}
	result.Authenticated = account.IsAuthenticated
	if account.Login != nil {
		result.Account = *account.Login
	}
	if !result.Authenticated {
		return result, nil
	}
	models, err := client.ListModels(ctx)
	if err != nil {
		return result, err
	}
	for _, model := range models {
		result.Models = append(result.Models, Model{ID: model.ID, Name: model.Name, ReasoningEfforts: model.SupportedReasoningEfforts})
	}
	return result, nil
}
func RunCopilot(ctx context.Context, binary, directory, token, model, prompt string, bridge *Bridge, run *Run) error {
	client := NewCopilot(binary, directory, token)
	if err := client.Start(ctx); err != nil {
		return err
	}
	defer client.Stop()
	// Register SDK tools backed by our MCP client. This keeps the provider's
	// built-in servers off and avoids inheriting runtime MCP configuration.
	tools, reviewSession, err := copilotReviewTools(ctx, bridge)
	if err != nil {
		return err
	}
	defer reviewSession.Close()
	allowed := copilot.NewToolSet()
	for _, tool := range tools {
		allowed.AddCustom(tool.Name)
	}
	models, err := client.ListModels(ctx)
	if err != nil {
		return err
	}
	catalog := []Model{}
	for _, value := range models {
		catalog = append(catalog, Model{ID: value.ID, ReasoningEfforts: value.SupportedReasoningEfforts})
	}
	session, err := client.CreateSession(ctx, &copilot.SessionConfig{
		ClientName: "mier", Model: model, WorkingDirectory: directory,
		ReasoningEffort: ModelEffort(catalog, model, run.Snapshot().Intensity),
		AvailableTools:  allowed.ToSlice(), Tools: tools,
		SystemMessage:          &copilot.SystemMessageConfig{Mode: "replace", Content: Instructions},
		SkipCustomInstructions: copilot.Bool(true), CoauthorEnabled: copilot.Bool(false), ManageScheduleEnabled: copilot.Bool(false),
		LargeOutput:         &copilot.LargeToolOutputConfig{Enabled: copilot.Bool(false)},
		OnPermissionRequest: rejectReviewPermission,
	})
	if err != nil {
		return err
	}
	defer session.Disconnect()
	unsubscribe := session.On(run.recordCopilotEvent)
	defer unsubscribe()
	defer func() {
		// Experimental on some CLI versions. Missing metrics must not turn a
		// successful review into a failure or fabricate a zero cost.
		if ctx.Err() != nil {
			return
		}
		usageCtx, stop := context.WithTimeout(ctx, 2*time.Second)
		defer stop()
		if metrics, err := session.RPC.Usage.GetMetrics(usageCtx); err == nil {
			run.recordCopilotMetrics(metrics)
		}
	}()
	run.Progress("Reviewing captured pull request changes")
	response, err := session.SendAndWait(ctx, copilot.MessageOptions{Prompt: prompt})
	if ctx.Err() != nil {
		abort, stop := context.WithTimeout(context.Background(), 2*time.Second)
		defer stop()
		_ = session.Abort(abort)
		return ctx.Err()
	}
	if err != nil {
		return err
	}
	if run.Snapshot().Summary == "" && response != nil {
		if message, ok := response.Data.(*copilot.AssistantMessageData); ok {
			_ = run.Summary(message.Content)
		}
	}
	return nil
}

// The only permitted writes are explicit additive draft tools registered above.
// Built-in command, file, network, and unrelated MCP permissions are rejected.
func rejectReviewPermission(copilot.PermissionRequest, copilot.PermissionInvocation) (rpc.PermissionDecision, error) {
	feedback := "This review only allows M.I.E.R.'s scoped review tools"
	return &rpc.PermissionDecisionReject{Feedback: &feedback}, nil
}

type bridgeTransport struct{ token string }

func (t bridgeTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	clone := request.Clone(request.Context())
	clone.Header.Set("Authorization", "Bearer "+t.token)
	return http.DefaultTransport.RoundTrip(clone)
}
func copilotReviewTools(ctx context.Context, bridge *Bridge) ([]copilot.Tool, *mcp.ClientSession, error) {
	client := mcp.NewClient(&mcp.Implementation{Name: "mier-copilot", Version: "1.0.0"}, nil)
	session, err := client.Connect(ctx, &mcp.StreamableClientTransport{Endpoint: bridge.URL, HTTPClient: &http.Client{Transport: bridgeTransport{bridge.Token}}}, nil)
	if err != nil {
		return nil, nil, err
	}
	list, err := session.ListTools(ctx, &mcp.ListToolsParams{})
	if err != nil {
		session.Close()
		return nil, nil, err
	}
	tools := []copilot.Tool{}
	for _, name := range ToolNames {
		var definition *mcp.Tool
		for _, tool := range list.Tools {
			if tool.Name == name {
				definition = tool
				break
			}
		}
		if definition == nil {
			session.Close()
			return nil, nil, fmt.Errorf("Review tool %s is unavailable", name)
		}
		data, err := json.Marshal(definition.InputSchema)
		if err != nil {
			session.Close()
			return nil, nil, err
		}
		schema := map[string]any{}
		if err := json.Unmarshal(data, &schema); err != nil {
			session.Close()
			return nil, nil, err
		}
		tools = append(tools, copilot.Tool{Name: name, Description: definition.Description, Parameters: schema, SkipPermission: true, Defer: copilot.ToolDeferNever, Handler: func(inv copilot.ToolInvocation) (copilot.ToolResult, error) {
			request, stop := context.WithTimeout(ctx, 45*time.Second)
			defer stop()
			result, err := session.CallTool(request, &mcp.CallToolParams{Name: name, Arguments: inv.Arguments})
			if err != nil {
				return copilot.ToolResult{}, err
			}
			converted, ok := copilot.ConvertMCPCallToolResult(result)
			if !ok {
				return copilot.ToolResult{}, errors.New("Unsupported review tool response")
			}
			return converted, nil
		}})
	}
	return tools, session, nil
}
