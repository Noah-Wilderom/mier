package main

import (
	"context"
	"errors"
	"io"
	"mag-ik-een-review/internal/gitclient"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func commandFixture(t *testing.T) *CommandService {
	t.Helper()
	service := NewCommandService(&RepositoryService{})
	service.repositories = editorRepository{root: filepath.Join(t.TempDir(), "literal $(repo) with spaces")}
	service.configPath = filepath.Join(t.TempDir(), "commands.json")
	t.Cleanup(func() { service.ServiceShutdown() })
	return service
}
func awaitCommand(t *testing.T, s *CommandService, id string) CommandRun {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		job, err := s.Run(id)
		if err != nil {
			t.Fatal(err)
		}
		if job.Status != "running" {
			return job
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("command did not stop")
	return CommandRun{}
}
func TestCustomCommandsPersistAndStreamThenCancel(t *testing.T) {
	s := commandFixture(t)
	definition, err := s.SaveCommand(CustomCommand{Name: "Tests", Executable: "go", Arguments: []string{"test", "$(literal)", "a b"}, TimeoutSeconds: 30})
	if err != nil {
		t.Fatal(err)
	}
	reopened := commandFixture(t)
	reopened.configPath = s.configPath
	items, err := reopened.Commands()
	if err != nil || len(items) != 1 || !reflect.DeepEqual(items[0], definition) {
		t.Fatal(items, err)
	}
	ready := make(chan struct{})
	s.run = func(ctx context.Context, root string, request gitclient.UserCommandRequest, stdout, stderr io.Writer) (int, error) {
		if root != s.repositories.(editorRepository).root || !reflect.DeepEqual(request.Arguments, definition.Arguments) {
			return -1, errors.New("arguments changed")
		}
		stdout.Write([]byte("tests started\n"))
		stderr.Write([]byte("progress\n"))
		close(ready)
		<-ctx.Done()
		return -1, ctx.Err()
	}
	job, err := s.Start(context.Background(), "selected", definition.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	<-ready
	latest, err := s.Latest(job.Root)
	if err != nil || latest == nil || !strings.Contains(latest.Stdout, "tests started") || latest.Stderr != "progress\n" {
		t.Fatal(latest, err)
	}
	if _, err = s.Start(context.Background(), "selected", definition.ID, "", ""); err == nil {
		t.Fatal("concurrent command accepted")
	}
	if err = s.Cancel(job.ID); err != nil {
		t.Fatal(err)
	}
	if result := awaitCommand(t, s, job.ID); result.Status != "canceled" {
		t.Fatal(result)
	}
	if err = s.DeleteCommand(definition.ID); err != nil {
		t.Fatal(err)
	}
	if _, err = s.Start(context.Background(), "selected", definition.ID, "", ""); err == nil {
		t.Fatal("deleted command ran")
	}
}
func TestCommandOutputLimitDoesNotShortWriteOrStopExecution(t *testing.T) {
	s := commandFixture(t)
	definition, _ := s.SaveCommand(CustomCommand{Name: "Output", Executable: "tool", Arguments: []string{}, TimeoutSeconds: 1})
	s.run = func(ctx context.Context, root string, r gitclient.UserCommandRequest, stdout, stderr io.Writer) (int, error) {
		payload := strings.Repeat("x", 600*1024)
		n, err := stdout.Write([]byte(payload))
		if n != len(payload) || err != nil {
			return 1, errors.New("short write")
		}
		stderr.Write([]byte("finished"))
		return 0, nil
	}
	job, err := s.Start(context.Background(), "selected", definition.ID, "", "")
	if err != nil {
		t.Fatal(err)
	}
	result := awaitCommand(t, s, job.ID)
	if result.Status != "completed" || len(result.Stdout) != 512*1024 || !result.Truncated || result.Stderr != "finished" {
		t.Fatal(result.Status, len(result.Stdout), result.Truncated, result.Error)
	}
}
func TestCommandValidationAndTerminalPathsRemainLiteral(t *testing.T) {
	s := commandFixture(t)
	if _, err := s.SaveCommand(CustomCommand{Name: "Bad", Executable: "git", Arguments: []string{"bad\x00arg"}, TimeoutSeconds: 1}); err == nil {
		t.Fatal("NUL accepted")
	}
	calls := 0
	s.lookPath = func(name string) (string, error) { return "/tools/" + name, nil }
	s.launch = func(command string, args []string, root string) error {
		calls++
		if command != "/tools/gnome-terminal" || !reflect.DeepEqual(args, []string{"--working-directory=" + root}) || root != s.repositories.(editorRepository).root {
			return errors.New("terminal changed a literal path")
		}
		return nil
	}
	if err := s.OpenTerminal(context.Background(), "selected", "gnome-terminal"); err != nil {
		t.Fatal(err)
	}
	if err := s.OpenTerminal(context.Background(), "selected", "arbitrary-shell"); err == nil {
		t.Fatal("unknown terminal accepted")
	}
	if calls != 1 {
		t.Fatal(calls)
	}
}
