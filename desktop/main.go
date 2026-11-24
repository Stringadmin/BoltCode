package main

import (
	"embed"
	"io/fs"
	"log"
	"os"
	"path/filepath"

	"github.com/Stringadmin/BoltCode/desktop/workbench"
	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist
var frontend embed.FS

func main() {
	assets, err := fs.Sub(frontend, "frontend/dist")
	if err != nil {
		log.Fatal(err)
	}
	config, err := os.UserConfigDir()
	if err != nil {
		log.Fatal(err)
	}
	var window *application.WebviewWindow
	app := application.New(application.Options{
		Name:        "BoltCode",
		Description: "BoltCode 桌面编程工作台",
		Assets:      application.AssetOptions{Handler: application.BundledAssetFileServer(assets)},
		SingleInstance: &application.SingleInstanceOptions{
			UniqueID: "com.stringadmin.boltcode.desktop",
			OnSecondInstanceLaunch: func(application.SecondInstanceData) {
				if window != nil {
					window.Show()
					window.Focus()
				}
			},
		},
	})
	choose := func() (string, error) {
		path, err := app.Dialog.OpenFile().
			SetTitle("打开本地 Git 仓库").
			CanChooseFiles(false).
			CanChooseDirectories(true).
			PromptForSingleSelection()
		// Wails' Windows picker reports cancellation with an internal error.
		if err != nil && err.Error() == "cancelled by user" {
			return "", nil
		}
		return path, err
	}
	service, err := workbench.New(filepath.Join(config, "BoltCode", "desktop"), choose, func(event workbench.Event) {
		app.Event.Emit("boltcode:turn", event)
	})
	if err != nil {
		log.Fatal(err)
	}
	app.RegisterService(application.NewService(service))
	app.OnShutdown(func() { workbench.Close(service) })
	window = app.Window.NewWithOptions(application.WebviewWindowOptions{
		Title: "BoltCode",
		Width: 1120, Height: 740, MinWidth: 640, MinHeight: 480,
		Frameless:        true,
		Windows:          application.WindowsWindow{NonClientRegionSupport: true},
		BackgroundColour: application.NewRGB(250, 251, 252),
	})
	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
