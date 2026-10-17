package lspclient

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
)

type SymbolRequest struct {
	CodePositionRequest
	Query string `json:"query"`
}
type SymbolTarget struct {
	DefinitionTarget
	Name      string `json:"name"`
	Kind      int    `json:"kind"`
	Depth     int    `json:"depth"`
	Container string `json:"container"`
}

func (m *Manager) navigation(ctx context.Context, request CodePositionRequest, method string, params map[string]any) (*workspace, string, json.RawMessage, func(), error) {
	w, s, path, text, language, done, err := m.prepare(ctx, request)
	if err != nil {
		return nil, "", nil, nil, err
	}
	unlock, err := s.lock(ctx)
	if err != nil {
		done()
		return nil, "", nil, nil, err
	}
	defer unlock()
	if s.peer != nil {
		err = s.peer.open(ctx, path, language, text)
	}
	if err == nil {
		err = s.open(ctx, path, language, text)
	}
	if err != nil {
		done()
		return nil, "", nil, nil, err
	}
	if method != "workspace/symbol" {
		params["textDocument"] = map[string]any{"uri": fileURI(path)}
	}
	var raw json.RawMessage
	err = s.rpc.Request(ctx, method, params, &raw)
	if isUnsupportedMethod(err) && s.peer != nil {
		err = s.peer.rpc.Request(ctx, method, params, &raw)
	}
	if err != nil {
		done()
		if isUnsupportedMethod(err) {
			err = fmt.Errorf("This language server does not support %s", method)
		}
		return nil, "", nil, nil, err
	}
	return w, path, raw, done, nil
}
func (m *Manager) References(ctx context.Context, request CodePositionRequest) ([]DefinitionTarget, error) {
	ctx, cancel := m.requestContext(ctx)
	defer cancel()
	w, _, raw, done, err := m.navigation(ctx, request, "textDocument/references", map[string]any{"position": Position{request.Line, request.Character}, "context": map[string]any{"includeDeclaration": true}})
	if err != nil {
		return nil, err
	}
	defer done()
	locations, err := parseLocations(raw, 128)
	if err != nil {
		return nil, err
	}
	return m.grantLocations(ctx, w, locations)
}
func (m *Manager) DocumentSymbols(ctx context.Context, request CodePositionRequest) ([]SymbolTarget, error) {
	return m.symbols(ctx, SymbolRequest{CodePositionRequest: request}, false)
}
func (m *Manager) WorkspaceSymbols(ctx context.Context, request SymbolRequest) ([]SymbolTarget, error) {
	return m.symbols(ctx, request, true)
}
func (m *Manager) symbols(ctx context.Context, request SymbolRequest, workspaceSymbols bool) ([]SymbolTarget, error) {
	ctx, cancel := m.requestContext(ctx)
	defer cancel()
	method := "textDocument/documentSymbol"
	params := map[string]any{}
	if workspaceSymbols {
		method = "workspace/symbol"
		if len(request.Query) > 200 || strings.ContainsRune(request.Query, 0) {
			return nil, errors.New("Symbol queries must be at most 200 characters")
		}
		params["query"] = strings.TrimSpace(request.Query)
	}
	w, path, raw, done, err := m.navigation(ctx, request.CodePositionRequest, method, params)
	if err != nil {
		return nil, err
	}
	defer done()
	var items []json.RawMessage
	if len(raw) != 0 && string(raw) != "null" {
		if err = json.Unmarshal(raw, &items); err != nil {
			return nil, err
		}
	}
	type symbol struct {
		Name      string
		Kind      int
		Depth     int
		Container string
		Location  definitionLocation
	}
	var symbols []symbol
	var flatten func([]json.RawMessage, int, string) error
	flatten = func(items []json.RawMessage, depth int, parent string) error {
		if depth > 32 {
			return errors.New("Language server symbol nesting exceeds 32 levels")
		}
		for _, item := range items {
			if len(symbols) >= 128 {
				return nil
			}
			var value struct {
				Name      string `json:"name"`
				Kind      int    `json:"kind"`
				Container string `json:"containerName"`
				Selection *Range `json:"selectionRange"`
				Range     *Range `json:"range"`
				Location  *struct {
					URI   string `json:"uri"`
					Range *Range `json:"range"`
				} `json:"location"`
				Children []json.RawMessage `json:"children"`
			}
			if err := json.Unmarshal(item, &value); err != nil {
				return err
			}
			location := definitionLocation{URI: fileURI(path)}
			valid := false
			if value.Location != nil && value.Location.Range != nil {
				location = definitionLocation{value.Location.URI, *value.Location.Range}
				valid = true
			} else if !workspaceSymbols && value.Selection != nil {
				location.Range = *value.Selection
				valid = true
			} else if !workspaceSymbols && value.Range != nil {
				location.Range = *value.Range
				valid = true
			}
			if valid && strings.TrimSpace(value.Name) != "" {
				container := value.Container
				if container == "" {
					container = parent
				}
				symbols = append(symbols, symbol{value.Name, value.Kind, depth, container, location})
			}
			if err := flatten(value.Children, depth+1, value.Name); err != nil {
				return err
			}
		}
		return nil
	}
	if err = flatten(items, 0, ""); err != nil {
		return nil, err
	}
	locations := make([]definitionLocation, len(symbols))
	for i, symbol := range symbols {
		locations[i] = symbol.Location
	}
	targets, err := m.grantLocations(ctx, w, locations)
	if err != nil {
		return nil, err
	}
	result := []SymbolTarget{}
	for _, target := range targets {
		symbol := symbols[target.locationIndex]
		result = append(result, SymbolTarget{target, symbol.Name, symbol.Kind, symbol.Depth, symbol.Container})
	}

	return result, nil
}
