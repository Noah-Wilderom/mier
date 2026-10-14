package lspclient

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWarmupSurvivesCancellationAndSharesInitialCapture(t *testing.T) {
	for _, method := range []string{"warmup", "hover"} {
		t.Run(method, func(t *testing.T) {
			m, root := managerHarness(t, map[string]string{"use.php": "<?php use Prepared;\n"})
			archive := m.options.Archive
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			var captures atomic.Int32
			m.options.Archive = func(ctx context.Context, request CodePositionRequest) (ArchiveResult, error) {
				captures.Add(1)
				once.Do(func() { close(started) })
				select {
				case <-release:
					return archive(ctx, request)
				case <-ctx.Done():
					return ArchiveResult{}, ctx.Err()
				}
			}
			request := requestFor(root, "use.php")
			ctx, cancel := context.WithCancel(context.Background())
			first := make(chan error, 1)
			go func() {
				if method == "warmup" {
					first <- m.Warmup(ctx, request)
				} else {
					_, err := m.Hover(ctx, request)
					first <- err
				}
			}()
			select {
			case <-started:
			case <-time.After(2 * time.Second):
				t.Fatal("Capture did not start")
			}
			cancel()
			if err := <-first; !errors.Is(err, context.Canceled) {
				t.Fatalf("Cancelled waiter returned %v", err)
			}
			second := make(chan error, 1)
			go func() { second <- m.Warmup(context.Background(), request) }()
			close(release)
			if err := <-second; err != nil {
				t.Fatal(err)
			}
			hover, err := m.Hover(context.Background(), request)
			if err != nil || hover.Contents == "" {
				t.Fatalf("Prepared hover failed: %+v %v", hover, err)
			}
			if captures.Load() != 1 {
				t.Fatalf("Cancelled startup was repeated %d times", captures.Load())
			}
		})
	}
}

func TestFailedWarmupCanRetryAndShutdownCancelsPreparation(t *testing.T) {
	m, root := managerHarness(t, map[string]string{"use.php": "<?php use Retry;\n"})
	archive := m.options.Archive
	m.options.Archive = func(context.Context, CodePositionRequest) (ArchiveResult, error) {
		return ArchiveResult{}, errors.New("Temporary fetch failure")
	}
	request := requestFor(root, "use.php")
	if err := m.Warmup(context.Background(), request); err == nil {
		t.Fatal("Warmup hid its capture failure")
	}
	m.options.Archive = archive
	if err := m.Warmup(context.Background(), request); err != nil {
		t.Fatal(err)
	}
	request.Revision = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	started := make(chan struct{})
	m.options.Archive = func(ctx context.Context, _ CodePositionRequest) (ArchiveResult, error) {
		close(started)
		<-ctx.Done()
		return ArchiveResult{}, ctx.Err()
	}
	result := make(chan error, 1)
	go func() { result <- m.Warmup(context.Background(), request) }()
	<-started
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("Shutdown did not cancel warmup")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown left preparation running")
	}
}

func TestPHPPreparationDisablesUnconsumedDiagnosticWork(t *testing.T) {
	m, root := managerHarness(t, map[string]string{"use.php": "<?php use Prepared;\n"})
	if err := m.Warmup(context.Background(), requestFor(root, "use.php")); err != nil {
		t.Fatal(err)
	}
	for _, w := range m.workspaces {
		var options map[string]any
		if err := w.sessions["php"].rpc.Request(context.Background(), "test/initializationOptions", nil, &options); err != nil {
			t.Fatal(err)
		}
		for _, key := range []string{"language_server.diagnostics_on_open", "language_server.diagnostics_on_update", "language_server.diagnostics_on_save", "language_server_highlight.enabled"} {
			if value, present := options[key]; !present || value != false {
				t.Fatalf("Unconsumed PHP work was enabled: %s = %v", key, value)
			}
		}
	}
}
