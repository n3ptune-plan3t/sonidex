//go:build !windows && !(linux && !android)

package main

import (
	"fmt"
	"os"
)

// The streamer GUI is native per platform: Win32 on Windows, X11/Wayland on
// Linux. The Android receiver is a separate native app (see android/).
func main() {
	fmt.Fprintln(os.Stderr, "sonidex: no GUI for this platform; on Linux use the GUI or sonidex-tui, on Android install the receiver APK")
	os.Exit(1)
}
