//go:build !linux || !cgo || gtk3 || android || server

package main

import (
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const nativeWindowSizePollingInterval time.Duration = 0

func nativeWindowSize(window *application.WebviewWindow) (int, int) {
	return window.Size()
}
