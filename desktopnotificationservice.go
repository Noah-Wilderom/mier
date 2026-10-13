package main

import (
	"context"
	"errors"
	"fmt"
	"html"
	"runtime"
	"strings"
	"sync"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/services/notifications"
)

type desktopNotificationSDK interface {
	ServiceStartup(context.Context, application.ServiceOptions) error
	ServiceShutdown() error
	RequestNotificationAuthorization() (bool, error)
	CheckNotificationAuthorization() (bool, error)
	SendNotification(notifications.NotificationOptions) error
}

var errDesktopNotificationsClosed = errors.New("Desktop notifications have shut down")

// DesktopNotificationService keeps the optional native SDK out of app startup.
// Creating the SDK singleton does not open D-Bus, register Windows activation,
// or inspect macOS signing; those happen only after explicit desktop opt-in.
type DesktopNotificationService struct {
	mu      sync.Mutex
	sdk     desktopNotificationSDK
	ctx     context.Context
	cancel  context.CancelFunc
	options application.ServiceOptions
	closed  bool
	gate    chan struct{}
	// Accessed only while holding gate, including deferred shutdown.
	started bool
}

func NewDesktopNotificationService() *DesktopNotificationService {
	return newDesktopNotificationService(notifications.New())
}

func newDesktopNotificationService(sdk desktopNotificationSDK) *DesktopNotificationService {
	s := &DesktopNotificationService{sdk: sdk, gate: make(chan struct{}, 1)}
	s.gate <- struct{}{}
	return s
}

func (s *DesktopNotificationService) ServiceStartup(ctx context.Context, options application.ServiceOptions) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return errDesktopNotificationsClosed
	}
	if s.ctx == nil {
		s.ctx, s.cancel = context.WithCancel(ctx)
		s.options = options
	}
	return nil
}

func (s *DesktopNotificationService) ServiceShutdown() error {
	s.mu.Lock()
	if s.closed {
		s.mu.Unlock()
		return nil
	}
	s.closed = true
	if s.cancel != nil {
		s.cancel()
	}
	s.mu.Unlock()

	select {
	case <-s.gate:
		defer func() { s.gate <- struct{}{} }()
		return s.shutdownSDK()
	default:
		// The pinned SDK has no caller-context parameter for native permission
		// dialogs. Return promptly, then close once that bounded call settles.
		// Running native cleanup concurrently with it would race the SDK.
		go func() {
			<-s.gate
			defer func() { s.gate <- struct{}{} }()
			_ = s.shutdownSDK()
		}()
		return nil
	}
}

// shutdownSDK requires gate and never opens an SDK that was left disabled.
func (s *DesktopNotificationService) shutdownSDK() error {
	if !s.started {
		return nil
	}
	s.started = false
	if err := s.sdk.ServiceShutdown(); err != nil {
		return fmt.Errorf("Could not close desktop notifications: %w", err)
	}
	return nil
}

// run serializes the SDK while allowing lifecycle cancellation to release the
// caller promptly. A cancelled authorization check can never lead to a send.
func (s *DesktopNotificationService) run(operation func(context.Context) error) error {
	s.mu.Lock()
	ctx, options, closed := s.ctx, s.options, s.closed
	s.mu.Unlock()
	if closed {
		return errDesktopNotificationsClosed
	}
	if ctx == nil {
		return errors.New("Desktop notifications are not ready yet")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-s.gate:
	}
	result := make(chan error, 1)
	go func() {
		var outcome error
		defer func() {
			s.gate <- struct{}{}
			result <- outcome
		}()
		if err := ctx.Err(); err != nil {
			outcome = err
			return
		}
		if !s.started {
			if err := s.sdk.ServiceStartup(ctx, options); err != nil {
				// Startup may already have allocated a connection before failing.
				// Clean that partial attempt here, never at ordinary app startup.
				_ = s.sdk.ServiceShutdown()
				outcome = fmt.Errorf("Desktop notifications are unavailable: %w", err)
				return
			}
			s.started = true
		}
		if err := ctx.Err(); err != nil {
			outcome = err
			return
		}
		outcome = operation(ctx)
	}()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-result:
		if cancelled := ctx.Err(); cancelled != nil {
			return cancelled
		}
		return err
	}
}

func (s *DesktopNotificationService) requestPermission() error {
	return s.run(func(ctx context.Context) error {
		allowed, err := s.sdk.RequestNotificationAuthorization()
		if cancelled := ctx.Err(); cancelled != nil {
			return cancelled
		}
		if err != nil {
			return fmt.Errorf("Could not request desktop notification permission: %w", err)
		}
		if !allowed {
			return errors.New("Desktop notification permission was denied. Allow M.I.E.R. notifications in your system settings")
		}
		return nil
	})
}

func (s *DesktopNotificationService) send(event PRNotificationEvent) error {
	if !event.Desktop {
		return nil
	}
	if strings.TrimSpace(event.ID) == "" || strings.TrimSpace(event.Title) == "" {
		return errors.New("Desktop notification needs an event identifier and pull request title")
	}
	return s.run(func(ctx context.Context) error {
		allowed, err := s.sdk.CheckNotificationAuthorization()
		if cancelled := ctx.Err(); cancelled != nil {
			return cancelled
		}
		if err != nil {
			return fmt.Errorf("Could not check desktop notification permission: %w", err)
		}
		if !allowed {
			return errors.New("Desktop notifications are blocked by your system settings")
		}
		options := nativeNotificationOptions(event)
		// freedesktop notification bodies can interpret markup. Display the
		// provider's title and actor as plain text on Linux, like other OSes.
		if runtime.GOOS == "linux" {
			options.Body = html.EscapeString(options.Body)
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := s.sdk.SendNotification(options); err != nil {
			return fmt.Errorf("Could not send desktop notification: %w", err)
		}
		return ctx.Err()
	})
}

func nativeNotificationOptions(event PRNotificationEvent) notifications.NotificationOptions {
	title := "Pull request activity"
	switch event.Type {
	case "opened":
		title = "Pull request opened"
	case "closed":
		title = "Pull request closed"
	case "merged":
		title = "Pull request merged"
	case "reviewRequested":
		title = "Review requested"
	}
	identity := fmt.Sprintf("%s/%s/%s #%d", event.Host, event.Owner, event.Repo, event.Number)
	body := identity + ": " + event.Title
	if event.Actor != "" {
		body = event.Actor + " · " + body
	}
	return notifications.NotificationOptions{
		ID:                event.ID,
		Title:             title,
		Body:              body,
		ThreadID:          fmt.Sprintf("%s/%s/%s/%d", event.Host, event.Owner, event.Repo, event.Number),
		InterruptionLevel: notifications.InterruptionLevelActive,
		Data: map[string]interface{}{
			"url": event.URL, "host": event.Host, "owner": event.Owner, "repo": event.Repo,
			"number": event.Number, "type": event.Type, "key": event.Key,
		},
	}
}
