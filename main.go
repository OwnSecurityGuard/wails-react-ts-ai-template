package main

import (
	"embed"
	_ "embed"
	"log"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// Wails uses Go's `embed` package to embed the frontend files into the binary.
// Any files in the frontend/dist folder will be embedded into the binary and
// made available to the frontend.
// See https://pkg.go.dev/embed for more information.

//go:embed all:frontend/dist
var assets embed.FS

// main function serves as the application's entry point. It initializes the application, creates a window,
// and starts a goroutine that emits a time-based event every second. It subsequently runs the application and
// logs any error that might occur.
func main() {
	// 初始化 AI 配置存储和服务
	configStore := NewConfigStore()
	aiService := NewAIService(configStore)

	// 启动 CopilotKit Runtime HTTP server
	copilotKitRuntime := NewCopilotKitRuntime(aiService)
	if err := copilotKitRuntime.Start(); err != nil {
		log.Fatalf("Failed to start CopilotKit Runtime: %v", err)
	}
	defer copilotKitRuntime.Stop()

	// Create a new Wails application by providing the necessary options.
	// Variables 'Name' and 'Description' are for application metadata.
	// 'Assets' configures the asset server with the 'FS' variable pointing to the frontend files.
	// 'Bind' is a list of Go struct instances. The frontend has access to the methods of these instances.
	// 'Mac' options tailor the application when running on macOS.
	app := application.New(application.Options{
		Name:        "wails-react-ts-template",
		Description: "A demo of using raw HTML & CSS",
		Services: []application.Service{
			application.NewService(&GreetService{}),
			application.NewService(aiService),
		},
		Assets: application.AssetOptions{
			Handler: application.BundledAssetFileServer(assets),
		},

		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	// Create a new window with the necessary options.
	// 'Title' is the title of the window.
	// 'Mac' options tailor the window when running on macOS.
	// 'BackgroundColour' is the background colour of the window.
	// 'URL' is the URL that will be loaded into the webview.
	////app.W
	app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:      "main",
		Title:     "Window 1",
		Width:     1024,
		Height:    768,
		Frameless: true,

		Mac: application.MacWindow{
			InvisibleTitleBarHeight: 50,
			Backdrop:                application.MacBackdropTranslucent,
			TitleBar:                application.MacTitleBarHiddenInset,
		},
		URL: "/",
	})
	//app.NewWebviewWindowWithOptions(application.WebviewWindowOptions{
	//	Name:      "main",
	//	Title:     "Window 1",
	//	Frameless: true,
	//
	//	Mac: application.MacWindow{
	//		InvisibleTitleBarHeight: 50,
	//		Backdrop:                application.MacBackdropTranslucent,
	//		TitleBar:                application.MacTitleBarHiddenInset,
	//	},
	//	URL:              "/",
	//})

	// Create a goroutine that emits an event containing the current time every second.
	// The frontend can listen to this event and update the UI accordingly.
	go func() {
		for {
			//app.Event.EmitEvent()
			//now := time.Now().Format(time.RFC1123)
			//app.EmitEvent("time", now)
			time.Sleep(time.Second)
		}
	}()

	// Run the application. This blocks until the application has been exited.
	err := app.Run()

	// If an error occurred while running the application, log it and exit.
	if err != nil {
		log.Fatal(err)
	}
}
