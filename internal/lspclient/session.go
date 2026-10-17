package lspclient

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type openedDocument struct {
	hash    [32]byte
	version int
}
type session struct {
	rpc          *RPCClient
	peer         *session
	gate         chan struct{}
	docMu        sync.Mutex
	opened       map[string]openedDocument
	capabilities struct {
		PositionEncoding       string `json:"positionEncoding"`
		ExecuteCommandProvider struct {
			Commands []string `json:"commands"`
		} `json:"executeCommandProvider"`
	}
}

func startSession(ctx, requestCtx context.Context, w *workspace, status ServerStatus, peer *session) (*session, error) {
	s := &session{peer: peer, gate: make(chan struct{}, 1), opened: map[string]openedDocument{}}
	s.gate <- struct{}{}
	args := append([]string{}, status.Args...)
	if status.Language == "vue" {
		args = append(args, "--tsdk="+status.Configuration.TypeScriptSDKPath)
	}
	rpc, err := StartRPC(ctx, ServerCommand{Command: status.Command, Args: args, Directory: w.directory, Env: status.Environment}, RPCHandlers{Request: func(_ context.Context, method string, params json.RawMessage) (any, *RPCError) {
		switch method {
		case "workspace/configuration":
			var request struct {
				Items []struct {
					Section string `json:"section"`
				} `json:"items"`
			}
			if json.Unmarshal(params, &request) != nil {
				return nil, &RPCError{Code: -32602, Message: "Invalid configuration request"}
			}
			result := make([]any, len(request.Items))
			for index, item := range request.Items {
				result[index] = configurationSection(status.Language, item.Section)
			}
			return result, nil
		case "workspace/workspaceFolders":
			return []any{map[string]any{"uri": fileURI(w.directory), "name": filepath.Base(w.root) + " @ " + w.revision[:7]}}, nil
		case "client/registerCapability", "client/unregisterCapability", "window/workDoneProgress/create":
			return nil, nil
		case "workspace/applyEdit":
			return map[string]any{"applied": false, "failureReason": "M.I.E.R. code review workspaces are read-only"}, nil
		case "window/showMessageRequest":
			return nil, nil
		default:
			return nil, &RPCError{Code: -32601, Message: "Server request is not supported in a read-only review"}
		}
	}, Notification: func(ctx context.Context, method string, params json.RawMessage) {
		if method == "tsserver/request" && peer != nil {
			s.broker(ctx, params)
		}
	}})
	if err != nil {
		return nil, err
	}
	s.rpc = rpc
	initialization := map[string]any{}
	if definition := catalogByID[status.Language]; definition != nil && definition.InitializationOptions != nil {
		for key, value := range definition.InitializationOptions {
			initialization[key] = value
		}
	}
	if status.Language == "astro" || status.Language == "mdx" {
		initialization["typescript"] = map[string]any{"tsdk": status.Configuration.TypeScriptSDKPath, "enabled": true}
	}
	if status.Language == "php" {
		// This client exposes symbol inspection, not diagnostic squiggles or
		// document highlights. Phpactor otherwise computes them on didOpen,
		// competing with the first hover on large files.
		initialization = map[string]any{
			"language_server.diagnostics_on_open":   false,
			"language_server.diagnostics_on_update": false,
			"language_server.diagnostics_on_save":   false,
			"language_server_highlight.enabled":     false,
		}
	}
	if status.Language == "typescript" {
		initialization = map[string]any{"hostInfo": "M.I.E.R.", "disableAutomaticTypingAcquisition": true, "tsserver": map[string]any{"path": status.Configuration.TypeScriptSDKPath, "logVerbosity": "off", "useSyntaxServer": "never"}}
		if status.Configuration.VuePluginPath != "" {
			initialization["plugins"] = []any{map[string]any{"name": "@vue/typescript-plugin", "location": status.Configuration.VuePluginPath, "languages": []string{"vue"}}}
		}
	}
	params := map[string]any{"processId": os.Getpid(), "clientInfo": map[string]any{"name": "M.I.E.R.", "version": "0.1.0"}, "rootUri": fileURI(w.directory), "rootPath": w.directory, "workspaceFolders": []any{map[string]any{"uri": fileURI(w.directory), "name": filepath.Base(w.root)}}, "capabilities": map[string]any{"general": map[string]any{"positionEncodings": []string{"utf-16"}}, "workspace": map[string]any{"configuration": true, "workspaceFolders": true, "applyEdit": false, "symbol": map[string]any{"dynamicRegistration": false}}, "textDocument": map[string]any{"hover": map[string]any{"contentFormat": []string{"markdown", "plaintext"}}, "definition": map[string]any{"linkSupport": true}, "references": map[string]any{"dynamicRegistration": false}, "documentSymbol": map[string]any{"hierarchicalDocumentSymbolSupport": true}, "synchronization": map[string]any{"dynamicRegistration": true, "didSave": false, "willSave": false}}}, "initializationOptions": initialization}
	initializeCtx, cancel := context.WithTimeout(requestCtx, 20*time.Second)
	defer cancel()
	var result struct {
		Capabilities json.RawMessage `json:"capabilities"`
	}
	if err = rpc.Request(initializeCtx, "initialize", params, &result); err != nil {
		rpc.Close()
		return nil, fmt.Errorf("Initialize %s: %w", status.Name, err)
	}
	if err = json.Unmarshal(result.Capabilities, &s.capabilities); err != nil {
		rpc.Close()
		return nil, errors.New("Language server returned invalid initialization capabilities")
	}
	if s.capabilities.PositionEncoding != "" && s.capabilities.PositionEncoding != "utf-16" {
		rpc.Close()
		return nil, errors.New("Language server selected an unsupported position encoding; UTF-16 is required")
	}
	if err = rpc.Notify(initializeCtx, "initialized", map[string]any{}); err != nil {
		rpc.Close()
		return nil, err
	}
	if definition := catalogByID[status.Language]; definition != nil && len(definition.Settings) > 0 {
		if err = rpc.Notify(initializeCtx, "workspace/didChangeConfiguration", map[string]any{"settings": definition.Settings}); err != nil {
			rpc.Close()
			return nil, err
		}
	}
	return s, nil
}

func configurationSection(language, section string) any {
	definition := catalogByID[language]
	if definition == nil || definition.Settings == nil {
		return map[string]any{}
	}
	if section == "" {
		return definition.Settings
	}
	var value any = definition.Settings
	for _, part := range strings.Split(section, ".") {
		object, ok := value.(map[string]any)
		if !ok {
			return map[string]any{}
		}
		value = object[part]
	}
	if value == nil {
		return map[string]any{}
	}
	return value
}
func (s *session) close() error { return s.rpc.Close() }
func (s *session) lock(ctx context.Context) (func(), error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-s.gate:
		return func() { s.gate <- struct{}{} }, nil
	}
}
func (s *session) open(ctx context.Context, path, language, text string) error {
	s.docMu.Lock()
	defer s.docMu.Unlock()
	uri := fileURI(path)
	hash := sha256.Sum256([]byte(text))
	previous, exists := s.opened[uri]
	if exists && previous.hash == hash {
		return nil
	}
	version := previous.version + 1
	var err error
	if !exists {
		err = s.rpc.Notify(ctx, "textDocument/didOpen", map[string]any{"textDocument": map[string]any{"uri": uri, "languageId": language, "version": version, "text": text}})
	} else {
		err = s.rpc.Notify(ctx, "textDocument/didChange", map[string]any{"textDocument": map[string]any{"uri": uri, "version": version}, "contentChanges": []any{map[string]any{"text": text}}})
	}
	if err == nil {
		s.opened[uri] = openedDocument{hash: hash, version: version}
	}
	return err
}
func (s *session) broker(ctx context.Context, params json.RawMessage) {
	var values []json.RawMessage
	if json.Unmarshal(params, &values) != nil {
		return
	}
	nested := false
	if len(values) > 0 && len(values[0]) > 0 && values[0][0] == '[' {
		nested = true
	} else {
		values = []json.RawMessage{params}
	}
	for _, value := range values {
		var request []json.RawMessage
		if json.Unmarshal(value, &request) != nil || len(request) != 3 {
			continue
		}
		var command string
		if json.Unmarshal(request[1], &command) != nil {
			continue
		}
		var args any
		if json.Unmarshal(request[2], &args) != nil {
			continue
		}
		requestCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		var response json.RawMessage
		err := s.peer.rpc.Request(requestCtx, "workspace/executeCommand", map[string]any{"command": "typescript.tsserverRequest", "arguments": []any{command, args, map[string]any{"isAsync": true, "lowPriority": true}}}, &response)
		cancel()
		body := json.RawMessage("null")
		if err == nil {
			var envelope map[string]json.RawMessage
			if json.Unmarshal(response, &envelope) == nil {
				if value, exists := envelope["body"]; exists {
					body = value
				}
			}
		}
		payload := []any{request[0], body}
		var reply any = payload
		if nested {
			reply = []any{payload}
		}
		notifyCtx, stop := context.WithTimeout(ctx, time.Second)
		_ = s.rpc.Notify(notifyCtx, "tsserver/response", reply)
		stop()
	}
}
func (s *session) supportsVueBroker() bool {
	for _, command := range s.capabilities.ExecuteCommandProvider.Commands {
		if command == "typescript.tsserverRequest" {
			return true
		}
	}
	return false
}
func hoverKind(raw json.RawMessage) string {
	var markup struct {
		Kind string `json:"kind"`
	}
	if json.Unmarshal(raw, &markup) == nil && markup.Kind == "plaintext" {
		return "plaintext"
	}
	return "markdown"
}

func hoverContents(raw json.RawMessage) string {
	if len(raw) == 0 || string(raw) == "null" {
		return ""
	}
	var text string
	if json.Unmarshal(raw, &text) == nil {
		return text
	}
	var markup struct {
		Kind     string `json:"kind"`
		Value    string `json:"value"`
		Language string `json:"language"`
	}
	if json.Unmarshal(raw, &markup) == nil && markup.Value != "" {
		if markup.Language != "" {
			fence := "```"
			for strings.Contains(markup.Value, fence) {
				fence += "`"
			}
			return fence + markup.Language + "\n" + markup.Value + "\n" + fence
		}
		return markup.Value
	}
	var values []json.RawMessage
	if json.Unmarshal(raw, &values) == nil {
		pieces := []string{}
		for _, value := range values {
			if piece := hoverContents(value); piece != "" {
				pieces = append(pieces, piece)
			}
		}
		return strings.Join(pieces, "\n\n")
	}
	return ""
}
