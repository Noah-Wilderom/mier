package lspclient

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const maxFrameBytes = 8 * 1024 * 1024

type RPCError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *RPCError) Error() string {
	return fmt.Sprintf("Language server error %d: %s", e.Code, e.Message)
}

type rpcMessage struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method,omitempty"`
	Params  json.RawMessage `json:"params,omitempty"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *RPCError       `json:"error,omitempty"`
}
type RPCHandlers struct {
	Request      func(context.Context, string, json.RawMessage) (any, *RPCError)
	Notification func(context.Context, string, json.RawMessage)
}
type ServerCommand struct {
	Command   string
	Args      []string
	Directory string
	Env       []string
}
type outgoingFrame struct {
	body []byte
	ack  chan error
}
type RPCClient struct {
	ctx      context.Context
	cancel   context.CancelFunc
	command  *exec.Cmd
	stdin    io.WriteCloser
	output   chan outgoingFrame
	done     chan struct{}
	waited   chan struct{}
	once     sync.Once
	mu       sync.Mutex
	err      error
	pending  map[string]chan rpcMessage
	sequence atomic.Int64
	handlers RPCHandlers
	workers  chan struct{}
}

func StartRPC(ctx context.Context, command ServerCommand, handlers RPCHandlers) (*RPCClient, error) {
	processCtx, cancel := context.WithCancel(ctx)
	cmd := exec.CommandContext(processCtx, command.Command, command.Args...)
	cmd.Dir = command.Directory
	cmd.Env = append(os.Environ(), command.Env...)
	cmd.WaitDelay = time.Second
	cleanup := configureProcess(cmd)
	stdin, err := cmd.StdinPipe()
	if err != nil {
		cancel()
		cleanup()
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		cancel()
		cleanup()
		return nil, err
	}
	// Diagnostic output is drained without being exposed to the UI. Servers
	// may include paths or provider data in stderr; it is not a safe error API.
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		_ = stdin.Close()
		cancel()
		cleanup()
		return nil, fmt.Errorf("Start language server: %w", err)
	}
	c := &RPCClient{ctx: processCtx, cancel: cancel, command: cmd, stdin: stdin, output: make(chan outgoingFrame, 64), done: make(chan struct{}), waited: make(chan struct{}), pending: map[string]chan rpcMessage{}, handlers: handlers, workers: make(chan struct{}, 64)}
	go c.writeLoop()
	go c.readLoop(stdout)
	go func() {
		err := cmd.Wait()
		cleanup()
		c.fail(fmt.Errorf("Language server stopped: %w", nonNilError(err)))
		close(c.waited)
	}()
	return c, nil
}
func nonNilError(err error) error {
	if err == nil {
		return io.EOF
	}
	return err
}
func (c *RPCClient) Alive() bool {
	select {
	case <-c.done:
		return false
	default:
		return true
	}
}
func (c *RPCClient) failure() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.err != nil {
		return c.err
	}
	return errors.New("Language server is closed")
}
func (c *RPCClient) fail(err error) {
	c.once.Do(func() {
		c.mu.Lock()
		c.err = err
		c.mu.Unlock()
		close(c.done)
		c.cancel()
		_ = c.stdin.Close()
	})
}

func readFrame(reader *bufio.Reader) ([]byte, error) {
	length := -1
	headers := 0
	for {
		line, err := reader.ReadSlice('\n')
		if err != nil {
			return nil, err
		}
		headers += len(line)
		if headers > 8192 {
			return nil, errors.New("LSP header exceeds size limit")
		}
		text := strings.TrimSuffix(strings.TrimSuffix(string(line), "\n"), "\r")
		if text == "" {
			break
		}
		key, value, ok := strings.Cut(text, ":")
		if !ok {
			return nil, errors.New("Malformed LSP header")
		}
		if strings.EqualFold(strings.TrimSpace(key), "Content-Length") {
			if length >= 0 {
				return nil, errors.New("Duplicate LSP Content-Length")
			}
			length, err = strconv.Atoi(strings.TrimSpace(value))
			if err != nil || length <= 0 || length > maxFrameBytes {
				return nil, errors.New("Invalid LSP Content-Length")
			}
		}
	}
	if length < 0 {
		return nil, errors.New("LSP frame has no Content-Length")
	}
	body := make([]byte, length)
	_, err := io.ReadFull(reader, body)
	return body, err
}
func (c *RPCClient) writeLoop() {
	for {
		select {
		case <-c.done:
			return
		case frame := <-c.output:
			_, err := fmt.Fprintf(c.stdin, "Content-Length: %d\r\n\r\n", len(frame.body))
			if err == nil {
				_, err = c.stdin.Write(frame.body)
			}
			frame.ack <- err
			if err != nil {
				c.fail(fmt.Errorf("Write to language server: %w", err))
				return
			}
		}
	}
}
func (c *RPCClient) send(ctx context.Context, message any) error {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	body, err := json.Marshal(message)
	if err != nil {
		return err
	}
	if len(body) > maxFrameBytes {
		return errors.New("LSP message exceeds size limit")
	}
	frame := outgoingFrame{body: body, ack: make(chan error, 1)}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-c.done:
		return c.failure()
	case c.output <- frame:
	}
	select {
	case err := <-frame.ack:
		return err
	case <-c.done:
		return c.failure()
	case <-ctx.Done():
		c.fail(errors.New("Language server did not consume its input"))
		return ctx.Err()
	}
}
func (c *RPCClient) Notify(ctx context.Context, method string, params any) error {
	return c.send(ctx, map[string]any{"jsonrpc": "2.0", "method": method, "params": params})
}
func (c *RPCClient) Request(ctx context.Context, method string, params any, result any) error {
	id := strconv.FormatInt(c.sequence.Add(1), 10)
	response := make(chan rpcMessage, 1)
	c.mu.Lock()
	c.pending[id] = response
	c.mu.Unlock()
	defer func() { c.mu.Lock(); delete(c.pending, id); c.mu.Unlock() }()
	if err := c.send(ctx, map[string]any{"jsonrpc": "2.0", "id": json.RawMessage(id), "method": method, "params": params}); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		go func() {
			cancelCtx, cancel := context.WithTimeout(c.ctx, 500*time.Millisecond)
			defer cancel()
			_ = c.Notify(cancelCtx, "$/cancelRequest", map[string]any{"id": json.RawMessage(id)})
		}()
		return ctx.Err()
	case <-c.done:
		return c.failure()
	case message := <-response:
		if message.Error != nil {
			return message.Error
		}
		if result != nil && len(message.Result) > 0 {
			return json.Unmarshal(message.Result, result)
		}
		return nil
	}
}
func (c *RPCClient) readLoop(stdout io.Reader) {
	reader := bufio.NewReaderSize(stdout, 8192)
	for {
		body, err := readFrame(reader)
		if err != nil {
			c.fail(fmt.Errorf("Read language server protocol: %w", err))
			return
		}
		var message rpcMessage
		if err := json.Unmarshal(body, &message); err != nil || message.JSONRPC != "2.0" {
			c.fail(errors.New("Language server returned invalid JSON-RPC"))
			return
		}
		if message.Method != "" {
			c.handle(message)
			continue
		}
		if len(message.ID) > 0 {
			c.mu.Lock()
			response := c.pending[string(message.ID)]
			c.mu.Unlock()
			if response != nil {
				select {
				case response <- message:
				default:
				}
			}
		}
	}
}
func (c *RPCClient) handle(message rpcMessage) {
	if len(message.ID) == 0 && (c.handlers.Notification == nil || message.Method != "tsserver/request") {
		return
	}
	select {
	case c.workers <- struct{}{}:
	default:
		// A saturated client must not spawn unbounded reply goroutines or drop
		// Vue broker notifications (which would leave server promises hanging).
		c.fail(errors.New("Language server exceeded the concurrent server-request limit"))
		return
	}
	go func() {
		defer func() { <-c.workers }()
		if len(message.ID) == 0 {
			c.handlers.Notification(c.ctx, message.Method, message.Params)
			return
		}
		var value any
		var failure *RPCError
		if c.handlers.Request != nil {
			value, failure = c.handlers.Request(c.ctx, message.Method, message.Params)
		} else {
			failure = &RPCError{Code: -32601, Message: "Unsupported server request"}
		}
		reply := rpcMessage{JSONRPC: "2.0", ID: message.ID, Error: failure}
		if failure == nil {
			reply.Result, _ = json.Marshal(value)
		}
		_ = c.send(c.ctx, reply)
	}()
}
func (c *RPCClient) Close() error {
	if c.Alive() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		_ = c.Request(ctx, "shutdown", nil, nil)
		_ = c.Notify(ctx, "exit", nil)
		cancel()
	}
	c.fail(errors.New("Language server shut down"))
	select {
	case <-c.waited:
		return nil
	case <-time.After(2 * time.Second):
		if c.command.Process != nil {
			_ = c.command.Process.Kill()
		}
		select {
		case <-c.waited:
			return nil
		case <-time.After(time.Second):
			return errors.New("Language server did not terminate")
		}
	}
}
