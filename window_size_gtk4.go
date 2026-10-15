//go:build linux && cgo && !gtk3 && !android && !server

package main

/*
#cgo pkg-config: gtk4
#include <gtk/gtk.h>
*/
import "C"

import (
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// This Wails GTK4 version does not emit ordinary window resize events either.
// Sample infrequently as a fallback; unchanged state never writes to disk.
const nativeWindowSizePollingInterval = time.Second

// Call on the UI thread. Wails alpha2.117's GTK4 Size() reads the default size,
// which can remain unchanged after a restored window is resized. Persist the
// current allocation instead; GTK reports these dimensions in logical pixels.
func nativeWindowSize(window *application.WebviewWindow) (int, int) {
	handle := window.NativeWindow()
	if handle == nil {
		return 0, 0
	}
	widget := (*C.GtkWidget)(handle)
	return int(C.gtk_widget_get_width(widget)), int(C.gtk_widget_get_height(widget))
}
