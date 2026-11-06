//go:build linux && !android

package main

import (
	"fmt"
	"os"
	"runtime/debug"

	"sonidex/internal/linuxui"
)

func main() {
	if os.Getenv("GOGC") == "" {
		debug.SetGCPercent(50)
	}
	if err := linuxui.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "sonidex:", err)
		os.Exit(1)
	}
}
