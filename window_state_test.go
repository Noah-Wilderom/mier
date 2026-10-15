package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestWindowStateRoundTripPreservesNormalSizeAcrossDisplayModes(t *testing.T) {
	path := filepath.Join(t.TempDir(), "preferences", "window-state.json")
	store := newWindowStateStore(path)
	if got := store.State(); got != defaultWindowState() {
		t.Fatal("missing preferences did not use default size", got)
	}
	store.observe(1180, 720, false, false, false)
	store.observe(1920, 1080, true, false, false)
	store.observe(0, 0, false, true, false)
	store.observe(1920, 1080, false, false, true)
	store.observe(0, 0, false, false, false) // A closed native window returns zero.
	want := savedWindowState{Width: 1180, Height: 720, Maximised: true}
	if got := store.State(); got != want {
		t.Fatal("display modes overwrote normal dimensions or maximised state", got)
	}
	// Closing flushes the last captured change synchronously.
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	reopened := newWindowStateStore(path)
	if got := reopened.State(); got != want {
		t.Fatal("window state did not survive restart", got)
	}
	store.observe(1500, 950, false, false, false)
	if got := store.State(); got != want {
		t.Fatal("closed store accepted a late resize", got)
	}
	reopened.observe(1180, 720, false, false, false)
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	if got := newWindowStateStore(path).State(); got != (savedWindowState{Width: 1180, Height: 720}) {
		t.Fatal("restored normal mode was not saved", got)
	}
}

func TestWindowStateDebouncesNativeSamplingWithoutHoldingTheStoreLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "window-state.json")
	store := newWindowStateStore(path)
	defer store.Close()
	var captures atomic.Int32
	store.scheduleCapture(func() { captures.Add(100) })
	captured := make(chan struct{})
	store.scheduleCapture(func() {
		captures.Add(1)
		// observe takes the store lock; sampling must not hold that lock.
		store.observe(1180, 720, false, false, false)
		close(captured)
	})
	select {
	case <-captured:
	case <-time.After(3 * time.Second):
		t.Fatal("debounced native sampling blocked")
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	if captures.Load() != 1 || newWindowStateStore(path).State() != (savedWindowState{Width: 1180, Height: 720}) {
		t.Fatal("resize events were not coalesced into the latest captured state")
	}
}

func TestInvalidWindowPreferencesFallBackWithoutPreventingStartup(t *testing.T) {
	for name, content := range map[string]string{
		"malformed":          "{",
		"missing dimensions": `{"maximised":true}`,
		"negative":           `{"width":-1,"height":720}`,
		"below minimum":      `{"width":700,"height":500}`,
		"huge":               `{"width":999999,"height":999999,"maximised":true}`,
		"wrong types":        `{"width":"1200","height":720}`,
		"oversized file":     strings.Repeat(" ", 4097),
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "window-state.json")
			if err := os.WriteFile(path, []byte(content), 0600); err != nil {
				t.Fatal(err)
			}
			store := newWindowStateStore(path)
			defer store.Close()
			if got := store.State(); got != defaultWindowState() {
				t.Fatal("invalid settings affected startup", got)
			}
		})
	}
}

func TestWindowStateWriteFailureRetainsChangesForRetry(t *testing.T) {
	parent := filepath.Join(t.TempDir(), "preferences")
	if err := os.WriteFile(parent, []byte("not a directory"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(parent, "window-state.json")
	store := newWindowStateStore(path)
	defer store.Close()
	store.observe(1200, 700, false, false, false)
	if err := store.Flush(); err == nil {
		t.Fatal("failed persistence reported success")
	}
	if got := store.State(); got.Width != 1200 || got.Height != 700 {
		t.Fatal("write failure discarded remembered size", got)
	}
	if err := os.Remove(parent); err != nil {
		t.Fatal(err)
	}
	if err := store.Flush(); err != nil {
		t.Fatal("write could not recover", err)
	}
	if got := newWindowStateStore(path).State(); got.Width != 1200 || got.Height != 700 {
		t.Fatal("retry did not save dimensions", got)
	}
}

func TestWindowStateConcurrentEventsKeepValidPreferences(t *testing.T) {
	path := filepath.Join(t.TempDir(), "window-state.json")
	store := newWindowStateStore(path)
	var workers sync.WaitGroup
	for i := 0; i < 20; i++ {
		workers.Add(1)
		go func(i int) {
			defer workers.Done()
			store.observe(1100+i, 700+i, false, false, false)
			if err := store.Flush(); err != nil {
				t.Error(err)
			}
			_ = store.State()
		}(i)
	}
	workers.Wait()
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	var state savedWindowState
	if err != nil || json.Unmarshal(data, &state) != nil || !validWindowSize(state.Width, state.Height) {
		t.Fatal("concurrent writes corrupted preferences", string(data), err)
	}
	entries, err := os.ReadDir(filepath.Dir(path))
	if err != nil || len(entries) != 1 {
		t.Fatal("temporary settings files were left behind", entries, err)
	}
}
