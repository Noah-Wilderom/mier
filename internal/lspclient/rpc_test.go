package lspclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestLSPRPCChild(t *testing.T) {
	if os.Getenv("MIER_LSP_TEST_CHILD") != "1" {
		return
	}
	reader := bufio.NewReaderSize(os.Stdin, 8192)
	var writeMu sync.Mutex
	write := func(message any) {
		body, _ := json.Marshal(message)
		writeMu.Lock()
		defer writeMu.Unlock()
		fmt.Fprintf(os.Stdout, "Content-Length: %d\r\n\r\n", len(body))
		os.Stdout.Write(body)
	}
	documents := map[string]string{}
	cancelled := 0
	var initializationID json.RawMessage
	waiting := 0
	vue := false
	for _, arg := range os.Args {
		if arg == "--fake-vue" {
			vue = true
		}
	}
	var hoverID json.RawMessage
	for {
		body, err := readFrame(reader)
		if err != nil {
			os.Exit(0)
		}
		var message rpcMessage
		if json.Unmarshal(body, &message) != nil {
			os.Exit(2)
		}
		reply := func(value any) { write(map[string]any{"jsonrpc": "2.0", "id": message.ID, "result": value}) }
		if message.Method == "" && len(message.ID) > 0 {
			waiting--
			if waiting == 0 && initializationID != nil {
				write(map[string]any{"jsonrpc": "2.0", "id": initializationID, "result": map[string]any{"capabilities": map[string]any{"positionEncoding": "utf-16", "hoverProvider": true, "definitionProvider": true, "executeCommandProvider": map[string]any{"commands": []string{"typescript.tsserverRequest"}}}}})
				initializationID = nil
			}
			continue
		}
		switch message.Method {
		case "initialize":
			initializationID = message.ID
			methods := []string{"workspace/configuration", "workspace/workspaceFolders", "client/registerCapability", "window/workDoneProgress/create", "workspace/applyEdit"}
			waiting = len(methods)
			for index, method := range methods {
				params := any(map[string]any{})
				if method == "workspace/configuration" {
					params = map[string]any{"items": []any{map[string]any{"section": "typescript"}}}
				}
				write(map[string]any{"jsonrpc": "2.0", "id": fmt.Sprintf("server-%d", index), "method": method, "params": params})
			}
		case "initialized":
		case "textDocument/didOpen":
			var params struct {
				TextDocument struct{ URI, Text string } `json:"textDocument"`
			}
			json.Unmarshal(message.Params, &params)
			documents[params.TextDocument.URI] = params.TextDocument.Text
		case "textDocument/didChange":
			var params struct {
				TextDocument   struct{ URI string }    `json:"textDocument"`
				ContentChanges []struct{ Text string } `json:"contentChanges"`
			}
			json.Unmarshal(message.Params, &params)
			if len(params.ContentChanges) > 0 {
				documents[params.TextDocument.URI] = params.ContentChanges[0].Text
			}
		case "textDocument/hover":
			if vue {
				hoverID = message.ID
				write(map[string]any{"jsonrpc": "2.0", "method": "tsserver/request", "params": []any{77, "_vue:quickinfo", map[string]any{"file": "fixture.vue", "line": 1, "offset": 1}}})
				continue
			}
			var params struct {
				TextDocument struct{ URI string } `json:"textDocument"`
			}
			json.Unmarshal(message.Params, &params)
			reply(map[string]any{"contents": []any{map[string]any{"language": "php", "value": documents[params.TextDocument.URI]}, "From the selected revision"}})
		case "tsserver/response":
			var values []json.RawMessage
			json.Unmarshal(message.Params, &values)
			if len(values) == 2 {
				var body map[string]any
				json.Unmarshal(values[1], &body)
				write(map[string]any{"jsonrpc": "2.0", "id": hoverID, "result": map[string]any{"contents": map[string]any{"kind": "markdown", "value": body["displayString"]}}})
			}
		case "workspace/executeCommand":
			reply(map[string]any{"body": map[string]any{"displayString": "Vue broker reached the peer TypeScript server"}})
		case "textDocument/definition":
			var params struct {
				TextDocument struct{ URI string } `json:"textDocument"`
			}
			json.Unmarshal(message.Params, &params)
			target := os.Getenv("MIER_LSP_DEFINITION_URI")
			if target == "" {
				target = strings.Replace(params.TextDocument.URI, "use.php", "definition.php", 1)
			}
			reply([]any{map[string]any{"targetUri": target, "targetRange": Range{Start: Position{0, 0}, End: Position{0, 5}}, "targetSelectionRange": Range{Start: Position{0, 0}, End: Position{0, 5}}}})
		case "test/hang":
		case "$/cancelRequest":
			cancelled++
		case "test/cancellations":
			reply(cancelled)
		case "test/echo":
			var value struct {
				Value int `json:"value"`
			}
			json.Unmarshal(message.Params, &value)
			id := append(json.RawMessage{}, message.ID...)
			go func() {
				time.Sleep(time.Duration(16-value.Value%16) * time.Millisecond)
				write(map[string]any{"jsonrpc": "2.0", "id": id, "result": value.Value})
			}()
		case "shutdown":
			reply(nil)
		case "exit":
			os.Exit(0)
		default:
			if len(message.ID) > 0 {
				write(rpcMessage{JSONRPC: "2.0", ID: message.ID, Error: &RPCError{Code: -32601, Message: "Unknown fake method"}})
			}
		}
	}
}
func fakeCommand(t *testing.T, vue bool) ServerCommand {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	args := []string{"-test.run=^TestLSPRPCChild$", "--"}
	if vue {
		args = append(args, "--fake-vue")
	}
	return ServerCommand{Command: executable, Args: args, Env: []string{"MIER_LSP_TEST_CHILD=1"}}
}
func TestRPCResponseCorrelationCancellationAndBoundedShutdown(t *testing.T) {
	client, err := StartRPC(context.Background(), fakeCommand(t, false), RPCHandlers{Request: func(context.Context, string, json.RawMessage) (any, *RPCError) { return nil, nil }})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { client.Close() })
	var workers sync.WaitGroup
	for index := 0; index < 32; index++ {
		workers.Add(1)
		go func(value int) {
			defer workers.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			defer cancel()
			var result int
			if err := client.Request(ctx, "test/echo", map[string]any{"value": value}, &result); err != nil {
				t.Error(err)
			} else if result != value {
				t.Errorf("Correlation returned %d for %d", result, value)
			}
		}(index)
	}
	workers.Wait()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := client.Request(ctx, "test/hang", nil, nil); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Expected timeout, got %v", err)
	}
	deadline := time.Now().Add(time.Second)
	for {
		var cancellations int
		probe, stop := context.WithTimeout(context.Background(), time.Second)
		err := client.Request(probe, "test/cancellations", nil, &cancellations)
		stop()
		if err != nil {
			t.Fatal(err)
		}
		if cancellations > 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("Cancellation notification did not reach server")
		}
		time.Sleep(time.Millisecond)
	}
	if err := client.Request(context.Background(), "unknown", nil, nil); err == nil {
		t.Fatal("RPC error was swallowed")
	}
	started := time.Now()
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 2*time.Second {
		t.Fatal("Shutdown was unbounded")
	}
}
func TestRPCRejectsInvalidAndOversizedFrames(t *testing.T) {
	for _, frame := range []string{"Content-Length: -1\r\n\r\n", "Content-Length: 999999999\r\n\r\n", "X-Test: value\r\n\r\n", "Content-Length: 2\r\nContent-Length: 2\r\n\r\n{}", "Content-Length: 4\r\n\r\n{}"} {
		if _, err := readFrame(bufio.NewReader(strings.NewReader(frame))); err == nil {
			t.Fatalf("Malformed frame accepted: %q", frame)
		}
	}
	body, err := readFrame(bufio.NewReader(strings.NewReader("Content-Length: 2\r\nContent-Type: application/vscode-jsonrpc; charset=utf-8\r\n\r\n{}")))
	if err != nil || string(body) != "{}" {
		t.Fatal("Valid frame rejected")
	}
	if _, err = readFrame(bufio.NewReader(strings.NewReader("Content-Length: 2\r\n\r\n"))); !errors.Is(err, io.EOF) {
		t.Fatalf("Truncated frame error = %v", err)
	}
}
