package main

import (
	"embed"
	"encoding/json"
	"fmt"
	"log"
	"mag-ik-een-review/internal/gitclient"
	"os"
	"path/filepath"
	"runtime"

	"github.com/wailsapp/wails/v3/pkg/application"
	"github.com/wailsapp/wails/v3/pkg/updater"
)

//go:embed all:frontend/dist
var assets embed.FS

func init() {
	application.RegisterEvent[WorkspaceUpdate]("workspace:updated")
	application.RegisterEvent[PRNotificationEvent]("notifications:pr")
	application.RegisterEvent[LSPToolsChangedEvent]("lsp:changed")
}

type nativeCommand struct {
	Action string `json:"action"`
	Value  string `json:"value,omitempty"`
}

func main() {
	if handled, err := gitclient.HandleGitEditor(os.Args); handled {
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}
	updater.HandleHelperMode()
	credentials := NewCredentialService()
	providers := NewReviewProviderService(credentials)
	credentials.providers = providers
	repositories := NewRepositoryService(credentials)
	github := NewGitHubService(credentials)
	jira := NewIntegrationService()
	linear := NewLinearService()
	ai := NewAIService(github, credentials)
	ai.jira, ai.linear = jira, linear
	workspace := NewWorkspaceService()
	var app *application.App
	desktop := NewDesktopNotificationService()
	prNotifications := NewPRNotificationService(credentials, func(event PRNotificationEvent) {
		app.Event.Emit("notifications:pr", event)
		if event.Desktop {
			go func() {
				if err := desktop.send(event); err != nil {
					log.Printf("Desktop notification could not be delivered: %v", err)
				}
			}()
		}
	}, desktop.requestPermission)
	syncService := NewWorkspaceSyncService(repositories, github, func(update WorkspaceUpdate) { app.Event.Emit("workspace:updated", update) })
	syncService.observePRs = prNotifications.Observe
	app = application.New(application.Options{
		Name:        "M.I.E.R.",
		Description: fmt.Sprintf("A native Git and pull request workspace\n\nVersion %s", appVersion),
		Services: []application.Service{
			application.NewService(ai),
			application.NewService(jira),
			application.NewService(linear),
			application.NewService(NewEditorService(repositories)),
			application.NewService(NewCommandService(repositories)),
			application.NewService(NewLSPService(repositories)),
			application.NewService(desktop),
			application.NewService(prNotifications),
			application.NewService(repositories),
			application.NewService(credentials),
			application.NewService(providers),
			application.NewService(github),
			application.NewService(workspace),
			application.NewService(syncService),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})
	workspace.app = app
	windowPreferencesPath := ""
	if directory, err := os.UserConfigDir(); err == nil {
		windowPreferencesPath = filepath.Join(directory, "mag-ik-een-review", "window-state.json")
	} else {
		log.Printf("Window preferences are unavailable: %v", err)
	}
	windowPreferences := newWindowStateStore(windowPreferencesPath)
	windowState := windowPreferences.State()
	startState := application.WindowStateNormal
	if windowState.Maximised {
		startState = application.WindowStateMaximised
	}

	inset := "0px"
	if runtime.GOOS == "darwin" {
		inset = "80px"
	}
	window := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:             "main",
		Title:            "M.I.E.R.",
		Width:            windowState.Width,
		Height:           windowState.Height,
		MinWidth:         minimumWindowWidth,
		MinHeight:        minimumWindowHeight,
		StartState:       startState,
		URL:              "/",
		BackgroundColour: application.NewRGB(245, 246, 248),
		JS: fmt.Sprintf(
			"document.documentElement.dataset.platform = %q; document.documentElement.style.setProperty('--native-titlebar-inset', %q);",
			runtime.GOOS, inset,
		),
		Mac: application.MacWindow{
			TitleBar:                application.MacTitleBarHiddenInset,
			InvisibleTitleBarHeight: 50,
			Appearance:              application.DefaultAppearance,
		},
	})
	rememberWindowState(app, window, windowPreferences)

	updatesEnabled := configureUpdates(app)
	defer app.Updater.StopPeriodicCheck()
	menu := createNativeMenu(app, window, updatesEnabled)
	app.Menu.Set(menu)
	window.SetMenu(menu)

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}

func createNativeMenu(app *application.App, window *application.WebviewWindow, updatesEnabled bool) *application.Menu {
	menu := application.NewMenu()
	about := func(_ *application.Context) { app.Menu.ShowAbout() }
	checkUpdates := func(_ *application.Context) { go checkForUpdates(app) }
	if runtime.GOOS == "darwin" {
		appMenu := menu.AddSubmenu("M.I.E.R.")
		// Cocoa's About role reads the numeric bundle version. Use our About
		// dialog to retain the full GitHub release version, including prereleases.
		appMenu.Add("About M.I.E.R.").OnClick(about)
		appMenu.Add("Check for Updates…").SetEnabled(updatesEnabled).OnClick(checkUpdates)
		appMenu.AddSeparator()
		appMenu.AddRole(application.ServicesMenu)
		appMenu.AddSeparator()
		appMenu.AddRole(application.Hide)
		appMenu.AddRole(application.HideOthers)
		appMenu.AddRole(application.UnHide)
		appMenu.AddSeparator()
		appMenu.AddRole(application.Quit)
	}

	command := func(action, value string) func(*application.Context) {
		return func(_ *application.Context) {
			payload, err := json.Marshal(nativeCommand{Action: action, Value: value})
			if err != nil {
				log.Printf("Unable to dispatch native command: %v", err)
				return
			}
			window.ExecJS("window.dispatchEvent(new CustomEvent('mag-ik-een-review:command', {detail: " + string(payload) + "}));")
		}
	}

	file := menu.AddSubmenu("File")
	file.Add("Open Repository…").SetAccelerator("CmdOrCtrl+O").OnClick(command("open-repository", ""))
	file.Add("Create Pull Request…").
		SetAccelerator("CmdOrCtrl+Shift+N").
		OnClick(command("create-pr", ""))
	file.AddSeparator()
	file.AddRole(application.CloseWindow)
	if runtime.GOOS != "darwin" {
		file.AddRole(application.Quit)
	}

	menu.AddRole(application.EditMenu)
	view := menu.AddSubmenu("View")
	view.Add("History").SetAccelerator("CmdOrCtrl+1").OnClick(command("navigate", "history"))
	view.Add("Changes").SetAccelerator("CmdOrCtrl+2").OnClick(command("navigate", "changes"))
	view.Add("Pull Requests").SetAccelerator("CmdOrCtrl+3").OnClick(command("navigate", "pull-requests"))
	view.Add("Actions").SetAccelerator("CmdOrCtrl+4").OnClick(command("navigate", "actions"))
	view.AddSeparator()
	view.Add("Command Palette…").SetAccelerator("CmdOrCtrl+K").OnClick(command("command-palette", ""))
	fullscreenShortcut := "F11"
	if runtime.GOOS == "darwin" {
		fullscreenShortcut = "CmdOrCtrl+Control+F"
	}
	view.Add("Toggle Full Screen").SetAccelerator(fullscreenShortcut).OnClick(command("toggle-fullscreen", ""))
	view.AddSeparator()
	appearance := view.AddSubmenu("Appearance")
	appearance.AddRadio("System", true).OnClick(command("theme", "system"))
	appearance.AddRadio("Light", false).OnClick(command("theme", "light"))
	appearance.AddRadio("Dark", false).OnClick(command("theme", "dark"))

	menu.AddRole(application.WindowMenu)
	if runtime.GOOS != "darwin" {
		help := menu.AddSubmenu("Help")
		help.Add("Check for Updates…").SetEnabled(updatesEnabled).OnClick(checkUpdates)
		help.AddSeparator()
		help.Add("About M.I.E.R.").OnClick(about)
	}
	return menu
}
