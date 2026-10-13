package main

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

type desktopSDKFake struct {
	mu       sync.Mutex
	calls    []string
	options  []notifications.NotificationOptions
	startup  func(context.Context, application.ServiceOptions) error
	shutdown func() error
	request  func() (bool, error)
	check    func() (bool, error)
	send     func(notifications.NotificationOptions) error
}

func (f *desktopSDKFake) record(call string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, call)
}
func (f *desktopSDKFake) recorded() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string{}, f.calls...)
}
func (f *desktopSDKFake) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	f.record("startup")
	if f.startup != nil {
		return f.startup(ctx, options)
	}
	return nil
}
func (f *desktopSDKFake) ServiceShutdown() error {
	f.record("shutdown")
	if f.shutdown != nil {
		return f.shutdown()
	}
	return nil
}
func (f *desktopSDKFake) RequestNotificationAuthorization() (bool, error) {
	f.record("request")
	if f.request != nil {
		return f.request()
	}
	return true, nil
}
func (f *desktopSDKFake) CheckNotificationAuthorization() (bool, error) {
	f.record("check")
	if f.check != nil {
		return f.check()
	}
	return true, nil
}
func (f *desktopSDKFake) SendNotification(options notifications.NotificationOptions) error {
	f.record("send")
	f.mu.Lock()
	f.options = append(f.options, options)
	f.mu.Unlock()
	if f.send != nil {
		return f.send(options)
	}
	return nil
}

func desktopHarness(t *testing.T, fake *desktopSDKFake) (*DesktopNotificationService, context.CancelFunc) {
	t.Helper()
	s := newDesktopNotificationService(fake)
	ctx, cancel := context.WithCancel(context.Background())
	if err := s.ServiceStartup(ctx, application.ServiceOptions{Name: "test-desktop"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cancel(); _ = s.ServiceShutdown() })
	return s, cancel
}
func desktopEvent() PRNotificationEvent {
	return PRNotificationEvent{ID: "pr-event-142", Key: "github.example/mier/client/142", Host: "github.example", Owner: "mier", Repo: "client", Number: 142, Type: "reviewRequested", Title: "Add keyboard navigation", URL: "https://github.example/mier/client/pull/142", Actor: "elena", Desktop: true}
}

func TestDesktopNotificationsOffByDefaultHasNoNativeLifecycle(t *testing.T) {
	fake := &desktopSDKFake{}
	s, _ := desktopHarness(t, fake)
	event := desktopEvent()
	event.Desktop = false
	if err := s.send(event); err != nil {
		t.Fatal(err)
	}
	if err := s.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	if got := fake.recorded(); len(got) != 0 {
		t.Fatalf("Disabled desktop notifications touched native SDK: %v", got)
	}
}

func TestDesktopPermissionInitializesOnceAndPassesLifecycle(t *testing.T) {
	fake := &desktopSDKFake{startup: func(ctx context.Context, options application.ServiceOptions) error {
		if ctx == nil || ctx.Err() != nil || options.Name != "test-desktop" {
			t.Error("SDK did not receive captured application lifecycle")
		}
		return nil
	}}
	s, _ := desktopHarness(t, fake)
	if err := s.requestPermission(); err != nil {
		t.Fatal(err)
	}
	if err := s.requestPermission(); err != nil {
		t.Fatal(err)
	}
	if err := s.send(desktopEvent()); err != nil {
		t.Fatal(err)
	}
	if err := s.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	if err := s.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	want := []string{"startup", "request", "request", "check", "send", "shutdown"}
	if got := fake.recorded(); !reflect.DeepEqual(got, want) {
		t.Fatalf("SDK lifecycle = %v, want %v", got, want)
	}
}

func TestDesktopPermissionDeniedAndSDKFailuresAreReported(t *testing.T) {
	sentinel := errors.New("native service unavailable")
	for _, scenario := range []struct {
		name      string
		fake      *desktopSDKFake
		operation string
		message   string
	}{
		{"denied request", &desktopSDKFake{request: func() (bool, error) { return false, nil }}, "request", "permission was denied"},
		{"request error", &desktopSDKFake{request: func() (bool, error) { return false, sentinel }}, "request", "Could not request"},
		{"denied send", &desktopSDKFake{check: func() (bool, error) { return false, nil }}, "send", "blocked by your system"},
		{"check error", &desktopSDKFake{check: func() (bool, error) { return false, sentinel }}, "send", "Could not check"},
		{"send error", &desktopSDKFake{send: func(notifications.NotificationOptions) error { return sentinel }}, "send", "Could not send"},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			s, _ := desktopHarness(t, scenario.fake)
			var err error
			if scenario.operation == "request" {
				err = s.requestPermission()
			} else {
				err = s.send(desktopEvent())
			}
			if err == nil || !strings.Contains(err.Error(), scenario.message) {
				t.Fatalf("Error = %v, want %q", err, scenario.message)
			}
			if scenario.name != "send error" && strings.Contains(strings.Join(scenario.fake.recorded(), ","), "send") {
				t.Fatal("Notification sent despite permission failure")
			}
		})
	}
}

func TestDesktopStartupFailureDoesNotFailApplicationStartupAndCanRetry(t *testing.T) {
	attempts := 0
	fake := &desktopSDKFake{startup: func(context.Context, application.ServiceOptions) error {
		attempts++
		if attempts == 1 {
			return errors.New("session bus unavailable")
		}
		return nil
	}}
	s, _ := desktopHarness(t, fake)
	if len(fake.recorded()) != 0 {
		t.Fatal("Native startup ran during app startup")
	}
	if err := s.requestPermission(); err == nil || !strings.Contains(err.Error(), "session bus unavailable") {
		t.Fatalf("Missing native startup error: %v", err)
	}
	if err := s.requestPermission(); err != nil {
		t.Fatal(err)
	}
	if got, want := fake.recorded(), []string{"startup", "shutdown", "startup", "request"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("Partial startup cleanup/retry = %v, want %v", got, want)
	}
}

func TestDesktopSendPreservesEventIdentityAndActualContent(t *testing.T) {
	fake := &desktopSDKFake{}
	s, _ := desktopHarness(t, fake)
	event := desktopEvent()
	if err := s.send(event); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	options := fake.options[0]
	fake.mu.Unlock()
	if options.ID != event.ID || options.Title != "Review requested" || !strings.Contains(options.Body, "Add keyboard navigation") || !strings.Contains(options.Body, "github.example/mier/client #142") || !strings.Contains(options.Body, "elena") {
		t.Fatalf("Notification lost event content: %#v", options)
	}
	if options.Data["url"] != event.URL || options.Data["number"] != 142 || options.ThreadID != "github.example/mier/client/142" {
		t.Fatalf("Notification lost routing/grouping identity: %#v", options)
	}
}

func TestDesktopEventTypesAndLinuxProviderText(t *testing.T) {
	for eventType, title := range map[string]string{
		"opened": "Pull request opened", "closed": "Pull request closed",
		"merged": "Pull request merged", "reviewRequested": "Review requested",
	} {
		event := desktopEvent()
		event.Type = eventType
		event.Actor = ""
		options := nativeNotificationOptions(event)
		if options.Title != title || strings.HasPrefix(options.Body, "elena") {
			t.Fatalf("Action title or unknown actor attribution is wrong: %#v", options)
		}
	}
	fake := &desktopSDKFake{}
	s, _ := desktopHarness(t, fake)
	event := desktopEvent()
	event.Title = "Use <button> & preserve focus"
	if err := s.send(event); err != nil {
		t.Fatal(err)
	}
	fake.mu.Lock()
	body := fake.options[0].Body
	fake.mu.Unlock()
	if runtime.GOOS == "linux" {
		if !strings.Contains(body, "Use &lt;button&gt; &amp; preserve focus") {
			t.Fatalf("Linux notification markup was not escaped: %q", body)
		}
	} else if !strings.Contains(body, event.Title) {
		t.Fatalf("Plain notification lost original text: %q", body)
	}
}

func TestDesktopCancelledLifecycleNeverInitializesOrSends(t *testing.T) {
	fake := &desktopSDKFake{}
	s, cancel := desktopHarness(t, fake)
	cancel()
	if err := s.requestPermission(); !errors.Is(err, context.Canceled) {
		t.Fatalf("Permission error = %v, want cancellation", err)
	}
	if err := s.send(desktopEvent()); !errors.Is(err, context.Canceled) {
		t.Fatalf("Send error = %v, want cancellation", err)
	}
	if len(fake.recorded()) != 0 {
		t.Fatal("Cancelled lifecycle touched SDK")
	}
}

func TestDesktopCancellationDuringAuthorizationIsPromptAndShutdownWaitsSafely(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	closed := make(chan struct{})
	fake := &desktopSDKFake{check: func() (bool, error) {
		close(entered)
		<-release
		return true, nil
	}, shutdown: func() error { close(closed); return nil }}
	s, cancel := desktopHarness(t, fake)
	result := make(chan error, 1)
	go func() { result <- s.send(desktopEvent()) }()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("SDK authorization did not start")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("Expected cancellation, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("Lifecycle cancellation did not release caller")
	}
	if err := s.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-closed:
		t.Fatal("SDK closed while native authorization was still running")
	default:
	}
	close(release)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("SDK did not close after authorization settled")
	}
	if got := fake.recorded(); !reflect.DeepEqual(got, []string{"startup", "check", "shutdown"}) {
		t.Fatalf("Cancelled authorization led to send or unsafe lifecycle: %v", got)
	}
}

func TestDesktopConcurrentEventsUseOneSerializedSDKStartup(t *testing.T) {
	fake := &desktopSDKFake{}
	s, _ := desktopHarness(t, fake)
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func() {
			defer workers.Done()
			if err := s.send(desktopEvent()); err != nil {
				t.Error(err)
			}
		}()
	}
	workers.Wait()
	starts, sends := 0, 0
	for _, call := range fake.recorded() {
		if call == "startup" {
			starts++
		}
		if call == "send" {
			sends++
		}
	}
	if starts != 1 || sends != 20 {
		t.Fatalf("Starts=%d sends=%d", starts, sends)
	}
}

func TestDesktopShutdownErrorsAndPostShutdownCalls(t *testing.T) {
	sentinel := errors.New("native close failed")
	fake := &desktopSDKFake{shutdown: func() error { return sentinel }}
	s, _ := desktopHarness(t, fake)
	if err := s.requestPermission(); err != nil {
		t.Fatal(err)
	}
	if err := s.ServiceShutdown(); !errors.Is(err, sentinel) {
		t.Fatalf("Shutdown error = %v", err)
	}
	if err := s.requestPermission(); !errors.Is(err, errDesktopNotificationsClosed) {
		t.Fatalf("Post-shutdown permission = %v", err)
	}
	if err := s.send(desktopEvent()); !errors.Is(err, errDesktopNotificationsClosed) {
		t.Fatalf("Post-shutdown send = %v", err)
	}
	if err := s.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
}

func TestDesktopInvalidEventsAndUnstartedWrapperNeverCallSDK(t *testing.T) {
	fake := &desktopSDKFake{}
	s := newDesktopNotificationService(fake)
	if err := s.requestPermission(); err == nil {
		t.Fatal("Permission succeeded before application lifecycle")
	}
	event := desktopEvent()
	event.ID = ""
	if err := s.send(event); err == nil {
		t.Fatal("Invalid notification identifier accepted")
	}
	if err := s.ServiceShutdown(); err != nil {
		t.Fatal(err)
	}
	if len(fake.recorded()) != 0 {
		t.Fatal("Invalid or unstarted wrapper touched SDK")
	}
}
