package tui

import (
	"os/exec"
	"runtime"
)

// openURL opens the given URL in the system's default browser.
// On macOS it tries Google Chrome first, then falls back to the default.
func openURL(url string) {
	if url == "" {
		return
	}
	switch runtime.GOOS {
	case "darwin":
		if err := exec.Command("open", "-a", "Google Chrome", url).Start(); err != nil {
			_ = exec.Command("open", url).Start()
		}
	case "linux":
		_ = exec.Command("xdg-open", url).Start()
	case "windows":
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
	}
}
