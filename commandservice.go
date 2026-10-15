package main

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mag-ik-een-review/internal/gitclient"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

type CustomCommand struct {
	ID             string   `json:"id"`
	Name           string   `json:"name"`
	Executable     string   `json:"executable"`
	Arguments      []string `json:"arguments"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
}
type CommandRun struct {
	ID         string `json:"id"`
	Root       string `json:"root"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	Truncated  bool   `json:"truncated"`
	ExitCode   int    `json:"exitCode"`
	Error      string `json:"error"`
	StartedAt  string `json:"startedAt"`
	FinishedAt string `json:"finishedAt"`
}
type commandJob struct {
	run    CommandRun
	cancel context.CancelFunc
}
type Terminal struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Available bool   `json:"available"`
}
type CommandService struct {
	repositories repositoryReader
	configPath   string
	configErr    error
	mu           sync.Mutex
	jobs         map[string]*commandJob
	latest       map[string]string
	closed       bool
	wg           sync.WaitGroup
	lookPath     func(string) (string, error)
	launch       func(string, []string, string) error
	run          func(context.Context, string, gitclient.UserCommandRequest, io.Writer, io.Writer) (int, error)
}

func NewCommandService(repositories *RepositoryService) *CommandService {
	directory, err := os.UserConfigDir()
	service := &CommandService{repositories: repositories, configPath: filepath.Join(directory, "mag-ik-een-review", "commands.json"), configErr: err, jobs: map[string]*commandJob{}, latest: map[string]string{}, lookPath: exec.LookPath, launch: launchTerminal}
	service.run = func(ctx context.Context, root string, request gitclient.UserCommandRequest, stdout, stderr io.Writer) (int, error) {
		if repositories.initErr != nil {
			return -1, repositories.initErr
		}
		return repositories.client.RunUserCommand(ctx, root, request, stdout, stderr)
	}
	return service
}
func validateCustomCommand(command CustomCommand) error {
	if strings.TrimSpace(command.Name) == "" || len(command.Name) > 100 || strings.ContainsRune(command.Name, 0) {
		return errors.New("Choose a command name of at most 100 characters")
	}
	if strings.TrimSpace(command.Executable) == "" || len(command.Executable) > 4096 || strings.ContainsAny(command.Executable, "\x00\r\n") {
		return errors.New("Choose an executable path or command name")
	}
	if len(command.Arguments) > 128 {
		return errors.New("A command supports at most 128 arguments")
	}
	total := 0
	for _, arg := range command.Arguments {
		if strings.ContainsRune(arg, 0) {
			return errors.New("Arguments cannot contain NUL")
		}
		total += len(arg)
	}
	if total > 64*1024 {
		return errors.New("Command arguments exceed 64 KiB")
	}
	if command.TimeoutSeconds < 1 || command.TimeoutSeconds > 900 {
		return errors.New("Choose a timeout between 1 and 900 seconds")
	}
	return nil
}
func (s *CommandService) readCommands() ([]CustomCommand, error) {
	items := []CustomCommand{}
	if s.configErr != nil {
		return nil, s.configErr
	}
	data, err := os.ReadFile(s.configPath)
	if errors.Is(err, os.ErrNotExist) {
		return items, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) > 512*1024 {
		return nil, errors.New("The command catalog exceeds 512 KiB")
	}
	if err = json.Unmarshal(data, &items); err != nil {
		return nil, errors.New("The saved command catalog is unreadable")
	}
	return items, nil
}
func (s *CommandService) writeCommands(items []CustomCommand) error {
	if err := os.MkdirAll(filepath.Dir(s.configPath), 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(s.configPath), ".commands-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	defer f.Close()
	if err = f.Chmod(0600); err != nil {
		return err
	}
	if err = json.NewEncoder(f).Encode(items); err != nil {
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), s.configPath)
}
func (s *CommandService) Commands() ([]CustomCommand, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.readCommands()
}
func (s *CommandService) SaveCommand(command CustomCommand) (CustomCommand, error) {
	if err := validateCustomCommand(command); err != nil {
		return CustomCommand{}, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.readCommands()
	if err != nil {
		return CustomCommand{}, err
	}
	if command.Arguments == nil {
		command.Arguments = []string{}
	}
	if command.ID == "" {
		if len(items) >= 50 {
			return CustomCommand{}, errors.New("Save at most 50 custom commands")
		}
		command.ID = rand.Text()
		items = append(items, command)
	} else {
		found := false
		for i, item := range items {
			if item.ID == command.ID {
				items[i] = command
				found = true
				break
			}
		}
		if !found {
			return CustomCommand{}, errors.New("This command no longer exists")
		}
	}
	if err = s.writeCommands(items); err != nil {
		return CustomCommand{}, err
	}
	return command, nil
}
func (s *CommandService) DeleteCommand(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	items, err := s.readCommands()
	if err != nil {
		return err
	}
	kept := []CustomCommand{}
	for _, item := range items {
		if item.ID != id {
			kept = append(kept, item)
		}
	}
	return s.writeCommands(kept)
}
func (s *CommandService) Start(ctx context.Context, path, id, expectedHead, expectedBranch string) (CommandRun, error) {
	if err := ctx.Err(); err != nil {
		return CommandRun{}, err
	}
	state, err := s.repositories.Snapshot(ctx, path)
	if err != nil {
		return CommandRun{}, err
	}
	if state.HeadSHA != expectedHead || state.CurrentBranch != expectedBranch {
		return CommandRun{}, errors.New("Review the current checkout before running this command")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return CommandRun{}, errors.New("The command service is shutting down")
	}
	if old := s.jobs[s.latest[state.Root]]; old != nil && old.run.Status == "running" {
		return CommandRun{}, errors.New("A custom command is already running for this repository")
	}
	items, err := s.readCommands()
	if err != nil {
		return CommandRun{}, err
	}
	var definition *CustomCommand
	for _, item := range items {
		if item.ID == id {
			definition = &item
			break
		}
	}
	if definition == nil {
		return CommandRun{}, errors.New("Save and select a command before running it")
	}
	if err = validateCustomCommand(*definition); err != nil {
		return CommandRun{}, err
	}
	execution, cancel := context.WithCancel(context.Background())
	job := &commandJob{run: CommandRun{ID: rand.Text(), Root: state.Root, Name: definition.Name, Status: "running", ExitCode: -1, StartedAt: time.Now().UTC().Format(time.RFC3339)}, cancel: cancel}
	s.jobs[job.run.ID] = job
	s.latest[state.Root] = job.run.ID
	for len(s.jobs) > 20 {
		oldest := ""
		for key, old := range s.jobs {
			if old.run.Status != "running" && (oldest == "" || old.run.StartedAt < s.jobs[oldest].run.StartedAt) {
				oldest = key
			}
		}
		if oldest == "" {
			break
		}
		old := s.jobs[oldest]
		if s.latest[old.run.Root] == oldest {
			delete(s.latest, old.run.Root)
		}
		delete(s.jobs, oldest)
	}
	request := gitclient.UserCommandRequest{Executable: definition.Executable, Arguments: append([]string{}, definition.Arguments...), TimeoutSeconds: definition.TimeoutSeconds, ExpectedHead: expectedHead, ExpectedBranch: expectedBranch}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		defer cancel()
		code, err := s.run(execution, state.Root, request, commandOutput{service: s, id: job.run.ID}, commandOutput{service: s, id: job.run.ID, stderr: true})
		s.mu.Lock()
		defer s.mu.Unlock()
		job.run.ExitCode = code
		job.run.Status = "completed"
		if err != nil {
			job.run.Status = "failed"
			job.run.Error = err.Error()
		}
		if errors.Is(execution.Err(), context.Canceled) {
			job.run.Status = "canceled"
		}
		job.run.FinishedAt = time.Now().UTC().Format(time.RFC3339)
	}()
	return job.run, nil
}

type commandOutput struct {
	service *CommandService
	id      string
	stderr  bool
}

func (w commandOutput) Write(data []byte) (int, error) {
	count := len(data)
	w.service.mu.Lock()
	defer w.service.mu.Unlock()
	job := w.service.jobs[w.id]
	if job == nil {
		return len(data), nil
	}
	output := &job.run.Stdout
	if w.stderr {
		output = &job.run.Stderr
	}
	remaining := 512*1024 - len(*output)
	if len(data) > remaining {
		job.run.Truncated = true
		data = data[:remaining]
	}
	*output += string(data)
	return count, nil
}
func (s *CommandService) Run(id string) (CommandRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[id]
	if job == nil {
		return CommandRun{}, errors.New("This command run is no longer available")
	}
	return job.run, nil
}
func (s *CommandService) Latest(path string) (*CommandRun, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[s.latest[path]]
	if job == nil {
		return nil, nil
	}
	copy := job.run
	return &copy, nil
}
func (s *CommandService) Cancel(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[id]
	if job == nil {
		return errors.New("This command run is no longer available")
	}
	if job.run.Status == "running" {
		job.cancel()
	}
	return nil
}
func (s *CommandService) ServiceShutdown() error {
	s.mu.Lock()
	s.closed = true
	for _, job := range s.jobs {
		if job.run.Status == "running" {
			job.cancel()
		}
	}
	s.mu.Unlock()
	s.wg.Wait()
	return nil
}

func terminalIDs() [][2]string {
	switch runtime.GOOS {
	case "darwin":
		return [][2]string{{"terminal", "Terminal"}, {"iterm", "iTerm"}, {"ghostty", "Ghostty"}, {"kitty", "Kitty"}, {"wezterm", "WezTerm"}}
	case "windows":
		return [][2]string{{"windows-terminal", "Windows Terminal"}, {"powershell", "PowerShell"}, {"cmd", "Command Prompt"}}
	}
	return [][2]string{{"ghostty", "Ghostty"}, {"kitty", "Kitty"}, {"alacritty", "Alacritty"}, {"wezterm", "WezTerm"}, {"gnome-terminal", "GNOME Terminal"}, {"konsole", "Konsole"}, {"xfce4-terminal", "Xfce Terminal"}, {"xterm", "XTerm"}, {"x-terminal-emulator", "Default terminal"}}
}
func (s *CommandService) terminal(id, root string) (string, []string, error) {
	command := id
	args := []string{}
	switch id {
	case "terminal", "iterm":
		command = "open"
		name := "Terminal"
		if id == "iterm" {
			name = "iTerm"
		}
		if runtime.GOOS != "darwin" {
			return "", nil, errors.New("This terminal is only available on macOS")
		}
		home, _ := os.UserHomeDir()
		found := false
		for _, dir := range []string{"/System/Applications/Utilities", "/Applications/Utilities", "/Applications", filepath.Join(home, "Applications")} {
			if info, err := os.Stat(filepath.Join(dir, name+".app")); err == nil && info.IsDir() {
				found = true
				break
			}
		}
		if !found {
			return "", nil, errors.New("Terminal application not found")
		}
		args = []string{"-a", name, root}
	case "windows-terminal":
		command = "wt.exe"
		args = []string{"-d", root}
	case "powershell":
		command = "pwsh.exe"
		if _, err := s.lookPath(command); err != nil {
			command = "powershell.exe"
		}
		args = []string{"-NoExit"}
	case "cmd":
		command = "cmd.exe"
		args = []string{"/K"}
	case "ghostty", "gnome-terminal", "xfce4-terminal":
		args = []string{"--working-directory=" + root}
	case "kitty":
		args = []string{"--directory", root}
	case "alacritty":
		args = []string{"--working-directory", root}
	case "wezterm":
		args = []string{"start", "--cwd", root}
	case "konsole":
		args = []string{"--workdir", root}
	case "xterm", "x-terminal-emulator":
	default:
		return "", nil, errors.New("Choose a supported installed terminal")
	}
	path, err := s.lookPath(command)
	if err != nil {
		return "", nil, fmt.Errorf("%s is not available on PATH", command)
	}
	return path, args, nil
}
func (s *CommandService) Terminals() []Terminal {
	items := []Terminal{}
	for _, entry := range terminalIDs() {
		_, _, err := s.terminal(entry[0], os.TempDir())
		items = append(items, Terminal{ID: entry[0], Name: entry[1], Available: err == nil})
	}
	return items
}
func (s *CommandService) OpenTerminal(ctx context.Context, path, id string) error {
	state, err := s.repositories.Snapshot(ctx, path)
	if err != nil {
		return err
	}
	command, args, err := s.terminal(id, state.Root)
	if err != nil {
		return err
	}
	if err = ctx.Err(); err != nil {
		return err
	}
	return s.launch(command, args, state.Root)
}
func launchTerminal(command string, args []string, root string) error {
	cmd := exec.Command(command, args...)
	cmd.Dir = root
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	if err := cmd.Start(); err != nil {
		return err
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	timer := time.NewTimer(400 * time.Millisecond)
	defer timer.Stop()
	select {
	case err := <-done:
		return err
	case <-timer.C:
		return nil
	}
}
