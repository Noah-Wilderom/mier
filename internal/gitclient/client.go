package gitclient

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

type Options struct {
	GitPath            string
	LocalTimeout       time.Duration
	RemoteTimeout      time.Duration
	CloneTimeout       time.Duration
	MaxOutputBytes     int
	MaxFileBytes       int64
	CredentialProvider func(context.Context, string) (string, error)
}

type Client struct {
	gitPath string
	options Options
	locksMu sync.Mutex
	locks   map[string]chan struct{}
}

type CommandError struct {
	Operation string
	Detail    string
	ExitCode  int
	cause     error
}

func (e *CommandError) Error() string { return "git " + e.Operation + ": " + e.Detail }
func (e *CommandError) Unwrap() error { return e.cause }

func New(options Options) (*Client, error) {
	gitPath := options.GitPath
	if gitPath == "" {
		var err error
		gitPath, err = exec.LookPath("git")
		if err != nil {
			return nil, errors.New("Git is not installed or is not available on PATH")
		}
	}
	resolved, err := filepath.Abs(gitPath)
	if err != nil {
		return nil, fmt.Errorf("locate Git: %w", err)
	}
	if options.LocalTimeout <= 0 {
		options.LocalTimeout = 15 * time.Second
	}
	if options.RemoteTimeout <= 0 {
		options.RemoteTimeout = 2 * time.Minute
	}
	if options.CloneTimeout <= 0 {
		options.CloneTimeout = 5 * time.Minute
	}
	if options.MaxOutputBytes <= 0 {
		options.MaxOutputBytes = 16 * 1024 * 1024
	}
	if options.MaxFileBytes <= 0 {
		options.MaxFileBytes = 2 * 1024 * 1024
	}
	client := &Client{gitPath: resolved, options: options, locks: make(map[string]chan struct{})}
	if _, err := client.run(context.Background(), "", options.LocalTimeout, "--version"); err != nil {
		return nil, err
	}
	return client, nil
}

type cappedBuffer struct {
	bytes.Buffer
	limit    int
	overflow bool
}

func (b *cappedBuffer) Write(p []byte) (int, error) {
	length := len(p)
	remaining := b.limit - b.Len()
	if remaining < length {
		b.overflow = true
		if remaining > 0 {
			_, _ = b.Buffer.Write(p[:remaining])
		}
	} else {
		_, _ = b.Buffer.Write(p)
	}
	return length, nil
}

var credentialURL = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s'"<>]+`)

func redactCredentials(value string) string {
	return credentialURL.ReplaceAllStringFunc(value, func(raw string) string {
		parsed, err := url.Parse(raw)
		if err != nil {
			return raw
		}
		if parsed.User != nil {
			parsed.User = nil
		}
		parsed.RawQuery, parsed.Fragment = "", ""
		parsed.ForceQuery = false
		return parsed.String()
	})
}

func (c *Client) run(ctx context.Context, root string, timeout time.Duration, args ...string) ([]byte, error) {
	return c.execute(ctx, root, timeout, nil, nil, args...)
}

func (c *Client) execute(ctx context.Context, root string, timeout time.Duration, extraEnv, secrets []string, args ...string) ([]byte, error) {
	return c.executeInput(ctx, root, timeout, extraEnv, secrets, nil, args...)
}

func (c *Client) executeInput(ctx context.Context, root string, timeout time.Duration, extraEnv, secrets []string, input []byte, args ...string) ([]byte, error) {
	output, err := c.executeInputResult(ctx, root, timeout, extraEnv, secrets, input, args...)
	if err != nil {
		return nil, err
	}
	return output, nil
}

// Some Git commands (merge-tree, bisect) return useful structured stdout with
// a documented non-zero exit code. Normal calls retain their existing contract.
func (c *Client) executeInputResult(ctx context.Context, root string, timeout time.Duration, extraEnv, secrets []string, input []byte, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	global := []string{"--no-pager", "--literal-pathspecs", "-c", "core.quotepath=false", "-c", "color.ui=false", "-c", "protocol.ext.allow=never"}
	if root != "" {
		global = append(global, "-C", root)
	}
	global = append(global, args...)
	command := exec.CommandContext(ctx, c.gitPath, global...)
	if input != nil {
		command.Stdin = bytes.NewReader(input)
	}
	command.Env = os.Environ()
	filtered := command.Env[:0]
	for _, entry := range command.Env {
		if strings.HasPrefix(entry, "GIT_TRACE") || strings.HasPrefix(entry, "GIT_CURL_VERBOSE=") {
			continue
		}
		filtered = append(filtered, entry)
	}
	command.Env = filtered
	command.Env = append(command.Env, "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=Never", "LC_ALL=C")
	command.Env = append(command.Env, extraEnv...)
	command.WaitDelay = 2 * time.Second
	cleanupProcess := configureCommand(command)
	defer cleanupProcess()
	stdout := &cappedBuffer{limit: c.options.MaxOutputBytes}
	stderr := &cappedBuffer{limit: 64 * 1024}
	command.Stdout, command.Stderr = stdout, stderr
	err := command.Run()
	operation := "command"
	if len(args) > 0 {
		operation = args[0]
	}
	if ctx.Err() != nil {
		return nil, &CommandError{Operation: operation, Detail: ctx.Err().Error(), ExitCode: -1, cause: ctx.Err()}
	}
	if stdout.overflow {
		return nil, &CommandError{Operation: operation, Detail: "output exceeds the supported size limit", ExitCode: -1}
	}
	if err != nil {
		code := -1
		var exitError *exec.ExitError
		if errors.As(err, &exitError) {
			code = exitError.ExitCode()
		}
		detail := strings.TrimSpace(redactCredentials(stderr.String()))
		for _, secret := range secrets {
			if secret != "" {
				detail = strings.ReplaceAll(detail, secret, "[redacted]")
			}
		}
		if detail == "" {
			detail = err.Error()
		}
		return stdout.Bytes(), &CommandError{Operation: operation, Detail: detail, ExitCode: code, cause: err}
	}
	return stdout.Bytes(), nil
}

func (c *Client) resolveRoot(ctx context.Context, path string) (string, error) {
	if path == "" || strings.IndexByte(path, 0) >= 0 {
		return "", errors.New("Choose a repository directory")
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return "", fmt.Errorf("repository directory: %w", err)
	}
	info, err := os.Stat(absolute)
	if err != nil {
		return "", fmt.Errorf("repository directory: %w", err)
	}
	if !info.IsDir() {
		return "", errors.New("The repository path must be a directory")
	}
	output, err := c.run(ctx, absolute, c.options.LocalTimeout, "rev-parse", "--show-toplevel")
	if err != nil {
		return "", err
	}
	root := strings.TrimSuffix(string(output), "\n")
	root = strings.TrimSuffix(root, "\r")
	root, err = filepath.EvalSymlinks(root)
	if err != nil {
		return "", fmt.Errorf("resolve repository directory: %w", err)
	}
	return filepath.Clean(root), nil
}

func (c *Client) acquire(ctx context.Context, root string) (func(), error) {
	c.locksMu.Lock()
	lock := c.locks[root]
	if lock == nil {
		lock = make(chan struct{}, 1)
		lock <- struct{}{}
		c.locks[root] = lock
	}
	c.locksMu.Unlock()
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case <-lock:
		return func() { lock <- struct{}{} }, nil
	}
}

func (c *Client) withRepository(ctx context.Context, path string, timeout time.Duration, action func(context.Context, string) error) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	root, err := c.resolveRoot(ctx, path)
	if err != nil {
		return err
	}
	release, err := c.acquire(ctx, root)
	if err != nil {
		return err
	}
	defer release()
	return action(ctx, root)
}

func inside(root, path string) bool {
	relative, err := filepath.Rel(root, path)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func validateFile(root, path string) (string, error) {
	if path == "" || strings.IndexByte(path, 0) >= 0 || filepath.IsAbs(path) {
		return "", errors.New("File paths must be relative to the repository")
	}
	cleaned := filepath.Clean(filepath.FromSlash(path))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, ".."+string(filepath.Separator)) {
		return "", errors.New("File paths cannot leave the repository")
	}
	for _, component := range strings.Split(cleaned, string(filepath.Separator)) {
		if strings.EqualFold(component, ".git") {
			return "", errors.New("Git internal files cannot be selected")
		}
	}
	target := filepath.Join(root, cleaned)
	parent := filepath.Dir(target)
	for {
		resolved, err := filepath.EvalSymlinks(parent)
		if err == nil {
			if !inside(root, resolved) {
				return "", errors.New("File paths cannot follow a directory outside the repository")
			}
			break
		}
		if !errors.Is(err, os.ErrNotExist) {
			return "", fmt.Errorf("validate file path: %w", err)
		}
		next := filepath.Dir(parent)
		if next == parent {
			return "", errors.New("Cannot resolve the file's parent directory")
		}
		parent = next
	}
	info, err := os.Lstat(target)
	if err == nil && info.IsDir() {
		return "", errors.New("Select individual files rather than a directory")
	}
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return "", fmt.Errorf("validate file path: %w", err)
	}
	return filepath.ToSlash(cleaned), nil
}

func validateFiles(root string, paths []string) ([]string, error) {
	if len(paths) == 0 {
		return nil, errors.New("Select at least one file")
	}
	if len(paths) > 10000 {
		return nil, errors.New("Too many files selected in one operation")
	}
	result := make([]string, 0, len(paths))
	seen := make(map[string]bool, len(paths))
	for _, path := range paths {
		validated, err := validateFile(root, path)
		if err != nil {
			return nil, err
		}
		if !seen[validated] {
			result = append(result, validated)
			seen[validated] = true
		}
	}
	return result, nil
}

func (c *Client) validateBranch(ctx context.Context, root, branch string) error {
	if branch == "" || strings.HasPrefix(branch, "-") || strings.IndexByte(branch, 0) >= 0 || strings.Contains(branch, "@{") || branch == "HEAD" {
		return errors.New("Enter a valid branch name")
	}
	_, err := c.run(ctx, root, c.options.LocalTimeout, "check-ref-format", "--branch", branch)
	return err
}
