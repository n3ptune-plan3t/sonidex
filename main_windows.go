//go:build windows

package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"sonidex/internal/winui"
)

//go:generate x86_64-w64-mingw32-windres -O coff -o rsrc_windows_amd64.syso winres/sonidex.rc

func main() {
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(50)
	}
	if err := winui.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "sonidex:", err)
		os.Exit(1)
	}
}
