package main

import (
	"encoding/json"
	"io"
	"log"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

const (
	defaultWindowWidth          = 1440
	defaultWindowHeight         = 900
	minimumWindowWidth          = 1000
	minimumWindowHeight         = 650
	maximumSavedWindowDimension = 32768
)

type savedWindowState struct {
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximised bool `json:"maximised"`
}

func defaultWindowState() savedWindowState {
	return savedWindowState{Width: defaultWindowWidth, Height: defaultWindowHeight}
}
func validWindowSize(width, height int) bool {
	return width >= minimumWindowWidth && height >= minimumWindowHeight && width <= maximumSavedWindowDimension && height <= maximumSavedWindowDimension
}

type windowStateStore struct {
	mu            sync.Mutex
	path          string
	state         savedWindowState
	dirty, closed bool
	timer         *time.Timer
}

func newWindowStateStore(path string) *windowStateStore {
	store := &windowStateStore{path: path, state: defaultWindowState()}
	if path == "" {
		return store
	}
	file, err := os.Open(path)
	if os.IsNotExist(err) {
		return store
	}
	if err == nil {
		defer file.Close()
		var data []byte
		data, err = io.ReadAll(io.LimitReader(file, 4097))
		var state savedWindowState
		if err == nil && len(data) <= 4096 && json.Unmarshal(data, &state) == nil && validWindowSize(state.Width, state.Height) {
			store.state = state
			return store
		}
	}
	log.Printf("Window preferences could not be read; using the default size")
	return store
}
func (s *windowStateStore) State() savedWindowState {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.state
}

// Maximised/fullscreen/minimised dimensions must never replace the normal
// window size. Minimise/fullscreen also retain the prior maximised state.
func (s *windowStateStore) observe(width, height int, maximised, minimised, fullscreen bool) {
	if minimised || fullscreen || !validWindowSize(width, height) {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	next := s.state
	next.Maximised = maximised
	if !maximised {
		next.Width, next.Height = width, height
	}
	if next == s.state {
		return
	}
	s.state, s.dirty = next, true
}

func (s *windowStateStore) scheduleCapture(capture func()) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}
	if s.timer != nil {
		s.timer.Stop()
	}
	s.timer = time.AfterFunc(300*time.Millisecond, func() {
		s.mu.Lock()
		closed := s.closed
		s.mu.Unlock()
		if closed {
			return
		}
		// Some native resize events arrive before maximised/fullscreen flags
		// settle. Sample after the resize burst, outside the persistence lock.
		capture()
		if err := s.Flush(); err != nil {
			log.Printf("Window preferences could not be saved: %v", err)
		}
	})
}
func (s *windowStateStore) saveLocked() error {
	if !s.dirty || s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0700); err != nil {
		return err
	}
	data, err := json.Marshal(s.state)
	if err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(s.path), ".window-state-*.json")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err = file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err = file.Close(); err != nil {
		return err
	}
	if err = os.Rename(file.Name(), s.path); err != nil {
		return err
	}
	s.dirty = false
	return nil
}
func (s *windowStateStore) Flush() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.saveLocked()
}
func (s *windowStateStore) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	if s.timer != nil {
		s.timer.Stop()
	}
	return s.saveLocked()
}

func rememberWindowState(app *application.App, window *application.WebviewWindow, store *windowStateStore) {
	capture := func() {
		// Native events arrive on different goroutines. Sample and record on the
		// UI thread so resize and state transitions cannot overwrite newer data.
		application.InvokeSync(func() {
			width, height := nativeWindowSize(window)
			store.observe(width, height, window.IsMaximised(), window.IsMinimised(), window.IsFullscreen())
		})
	}
	for _, event := range []events.WindowEventType{events.Common.WindowShow, events.Common.WindowDidResize, events.Common.WindowMaximise, events.Common.WindowUnMaximise, events.Common.WindowRestore, events.Common.WindowUnMinimise, events.Common.WindowUnFullscreen} {
		window.OnWindowEvent(event, func(*application.WindowEvent) { store.scheduleCapture(capture) })
	}
	// A hook runs before Wails' default close listener destroys the window.
	window.RegisterHook(events.Common.WindowClosing, func(*application.WindowEvent) {
		capture()
		if err := store.Flush(); err != nil {
			log.Printf("Window preferences could not be saved: %v", err)
		}
	})
	stopSampling := make(chan struct{})
	if nativeWindowSizePollingInterval > 0 {
		var startSampling sync.Once
		// Do not query the UI until Wails has created and shown the window.
		window.OnWindowEvent(events.Common.WindowShow, func(*application.WindowEvent) {
			startSampling.Do(func() {
				go func() {
					ticker := time.NewTicker(nativeWindowSizePollingInterval)
					defer ticker.Stop()
					for {
						select {
						case <-ticker.C:
							store.scheduleCapture(capture)
						case <-stopSampling:
							return
						}
					}
				}()
			})
		})
	}
	// Covers Quit/Cmd+Q and development restarts as well as the close button.
	app.OnShutdown(func() {
		close(stopSampling)
		capture()
		if err := store.Close(); err != nil {
			log.Printf("Window preferences could not be saved: %v", err)
		}
	})
}
