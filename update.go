package main

import (
	"context"
	_ "embed"
	"log"
	"os"
	"runtime"
	"time"

	"mag-ik-een-review/internal/appupdate"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/events"
)

// Release builds set this from the release tag through Go's linker.
var appVersion = "dev"

//go:embed build/updater.key.pub
var updaterPublicKey []byte

func configureUpdates(app *application.App) bool {
	if appVersion == "dev" {
		return false
	}
	executable, err := os.Executable()
	if err != nil || !appupdate.SupportsSelfUpdate(runtime.GOOS, executable, os.Getenv("APPIMAGE")) {
		return false
	}
	config, err := appupdate.NewConfig(appVersion, updaterPublicKey)
	if err == nil {
		err = app.Updater.Init(config)
	}
	if err != nil {
		log.Printf("Automatic updates are unavailable: %v", err)
		return false
	}
	// Check quietly at startup. Show Wails' download/restart window only when
	// a newer signed release exists. Subsequent checks use Wails' own timer.
	app.Event.OnApplicationEvent(events.Common.ApplicationStarted, func(*application.ApplicationEvent) {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
			defer cancel()
			release, err := app.Updater.Check(ctx)
			if err != nil {
				log.Printf("Check for updates: %v", err)
				return
			}
			if release != nil {
				checkForUpdates(app)
			}
		}()
	})
	return true
}

func checkForUpdates(app *application.App) {
	if err := app.Updater.CheckAndInstall(context.Background()); err != nil {
		log.Printf("Check for updates: %v", err)
	}
}
