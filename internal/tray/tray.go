package tray

import (
	"context"
	"os/exec"
	"runtime"
	"time"

	"fyne.io/systray"
)

// Engine is the sync toggle.
type Engine interface {
	Enabled() bool
	SetEnabled(bool)
	AutosyncEnabled() bool
	SetAutosync(bool)
}

// Run starts the systray. Must be called on the main goroutine on macOS (YYP-060).
func Run(ctx context.Context, engine Engine) {
	runtime.LockOSThread()
	onReady := func() {
		systray.SetTitle("YoYoPaste")
		updateIcon(engine.Enabled())
		mEnabled := systray.AddMenuItemCheckbox("Enabled", "", engine.Enabled())
		mOpen := systray.AddMenuItem("Open YoYoPaste", "Open web UI")
		mAutosync := systray.AddMenuItemCheckbox("Auto-sync", "", engine.AutosyncEnabled())
		systray.AddSeparator()
		mQuit := systray.AddMenuItem("Quit", "Quit YoYoPaste")

		go func() {
			for {
				select {
				case <-ctx.Done():
					systray.Quit()
					return
				case <-mEnabled.ClickedCh:
					newVal := !engine.Enabled()
					engine.SetEnabled(newVal)
					if newVal {
						mEnabled.Check()
					} else {
						mEnabled.Uncheck()
					}
					updateIcon(newVal)
				case <-mOpen.ClickedCh:
					openBrowser("http://127.0.0.1:8384")
				case <-mAutosync.ClickedCh:
					newVal := !engine.AutosyncEnabled()
					engine.SetAutosync(newVal)
					if newVal {
						mAutosync.Check()
					} else {
						mAutosync.Uncheck()
					}
				case <-mQuit.ClickedCh:
					systray.Quit()
					return
				}
			}
		}()

		go func() {
			ticker := timeTick(1)
			defer ticker.Stop()
			for {
				select {
				case <-ctx.Done():
					return
				case <-ticker.C:
					on := engine.Enabled()
					if on != mEnabled.Checked() {
						if on {
							mEnabled.Check()
						} else {
							mEnabled.Uncheck()
						}
						updateIcon(on)
					}
				}
			}
		}()
	}
	onExit := func() {}
	systray.Run(onReady, onExit)
}

func updateIcon(enabled bool) {
	// Minimal: use template icon data; for now set tooltip.
	if enabled {
		systray.SetTooltip("YoYoPaste — ON")
	} else {
		systray.SetTooltip("YoYoPaste — OFF")
	}
}

func openBrowser(url string) {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", url)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", url)
	default:
		cmd = exec.Command("xdg-open", url)
	}
	_ = cmd.Start()
}

// timeTick abstracts time.Ticker for testability
func timeTick(d time.Duration) *time.Ticker { return time.NewTicker(d) }
