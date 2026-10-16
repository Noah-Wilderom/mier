package reviewagent

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mag-ik-een-review/internal/desktopruntime"
	"os/exec"
	"sync"
	"sync/atomic"
	"time"
)

type rpcMessage struct {
	ID     json.RawMessage `json:"id,omitempty"`
	Method string          `json:"method,omitempty"`
	Params json.RawMessage `json:"params,omitempty"`
	Result json.RawMessage `json:"result,omitempty"`
	Error  *rpcError       `json:"error,omitempty"`
}
type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}
type Codex struct {
	command *exec.Cmd
	stdin   io.WriteCloser
	cancel  context.CancelFunc
	mu      sync.Mutex
	writeMu sync.Mutex
	pending map[string]chan rpcMessage
	next    atomic.Int64
	closed  bool
	done    chan struct{}
	notify  func(string, json.RawMessage)
}

func NewCodex(parent context.Context, binary, directory, version string, notify func(string, json.RawMessage)) (*Codex, error) {
	ctx, cancel := context.WithCancel(parent)
	args := []string{"-c", "features.shell_tool=false", "-c", "features.unified_exec=false", "-c", "features.multi_agent=false", "-c", "features.plugins=false", "-c", "features.apps=false", "-c", "features.browser_use=false", "-c", "features.computer_use=false", "-c", "features.image_generation=false", "-c", "features.view_image=false", "-c", "features.hooks=false", "-c", "web_search=\"disabled\"", "-c", "project_doc_max_bytes=0", "app-server"}
	command := exec.CommandContext(ctx, binary, args...)
	command.Dir = directory
	command.Env = desktopruntime.CLIEnvironment(ctx, binary)
	command.WaitDelay = 2 * time.Second
	configureProcess(command)
	stdout, err := command.StdoutPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	stdin, err := command.StdinPipe()
	if err != nil {
		cancel()
		return nil, err
	}
	// Provider errors arrive as typed RPC errors. Never forward a raw credential
	// process log to the webview or application log.
	command.Stderr = io.Discard
	if err := command.Start(); err != nil {
		cancel()
		return nil, fmt.Errorf("Start Codex: %w", err)
	}
	client := &Codex{command: command, stdin: stdin, cancel: cancel, pending: map[string]chan rpcMessage{}, done: make(chan struct{}), notify: notify}
	go func() { client.read(stdout); _ = command.Wait(); client.fail(); close(client.done) }()
	ready, stop := context.WithTimeout(parent, 15*time.Second)
	defer stop()
	if err := client.Call(ready, "initialize", map[string]any{"clientInfo": map[string]string{"name": "mier", "title": "M.I.E.R.", "version": version}}, nil); err != nil {
		client.Close()
		return nil, err
	}
	if err := client.send(map[string]any{"method": "initialized", "params": map[string]any{}}); err != nil {
		client.Close()
		return nil, err
	}
	return client, nil
}
func (c *Codex) send(value any) error {
	data, err := json.Marshal(value)
	if err != nil {
		return err
	}
	c.writeMu.Lock()
	defer c.writeMu.Unlock()
	_, err = c.stdin.Write(append(data, '\n'))
	return err
}
func (c *Codex) Call(ctx context.Context, method string, params any, result any) error {
	id := c.next.Add(1)
	key := fmt.Sprint(id)
	response := make(chan rpcMessage, 1)
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return errors.New("Codex is not running. Reconnect and retry")
	}
	c.pending[key] = response
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, key); c.mu.Unlock() }()
	if err := c.send(map[string]any{"id": id, "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case value, ok := <-response:
		if !ok {
			return errors.New("Codex exited before completing the request")
		}
		if value.Error != nil {
			return fmt.Errorf("Codex %s: %s", method, value.Error.Message)
		}
		if result != nil {
			return json.Unmarshal(value.Result, result)
		}
		return nil
	}
}
func (c *Codex) read(reader io.Reader) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 64*1024), 8*1024*1024)
	for scanner.Scan() {
		var message rpcMessage
		if json.Unmarshal(scanner.Bytes(), &message) != nil {
			c.cancel()
			return
		}
		if message.Method != "" {
			if len(message.ID) > 0 {
				if message.Method == "item/commandExecution/requestApproval" || message.Method == "item/fileChange/requestApproval" {
					_ = c.send(map[string]any{"id": json.RawMessage(message.ID), "result": map[string]string{"decision": "decline"}})
				} else {
					_ = c.send(map[string]any{"id": json.RawMessage(message.ID), "error": rpcError{Code: -32601, Message: "M.I.E.R. reviews only allow the supplied review MCP tools"}})
				}
			} else if c.notify != nil {
				c.notify(message.Method, message.Params)
			}
			continue
		}
		c.mu.Lock()
		if pending := c.pending[string(message.ID)]; pending != nil {
			pending <- message
			delete(c.pending, string(message.ID))
		}
		c.mu.Unlock()
	}
	if scanner.Err() != nil {
		c.cancel()
	}
}
func (c *Codex) fail() {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return
	}
	c.closed = true
	for key, pending := range c.pending {
		close(pending)
		delete(c.pending, key)
	}
}
func (c *Codex) Close() {
	c.cancel()
	_ = c.stdin.Close()
	select {
	case <-c.done:
	case <-time.After(3 * time.Second):
	}
}
