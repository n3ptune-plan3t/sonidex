package backend

import (
	"bytes"
	"fmt"
	"os/exec"
	"strings"
)

func ListADBDevices() ([]string, error) {
	cmd := exec.Command("adb", "devices")
	var out bytes.Buffer
	cmd.Stdout = &out
	err := cmd.Run()
	if err != nil {
		return nil, fmt.Errorf("adb unavailable: %w", err)
	}

	lines := strings.Split(out.String(), "\n")
	var devices []string
	for _, line := range lines[1:] {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == "device" {
			devices = append(devices, fields[0])
		}
	}
	return devices, nil
}

// SetupADBReverse provisions the tunnel used to reach the phone's TCP listener
// over the USB cable.
//
// NOTE: despite the name (kept for compatibility with callers), this uses
// `adb forward`, not `adb reverse`. The desktop app is the one that *dials*
// (backend.StartTCPSender connects to 127.0.0.1:<port>), and the Android app
// is the one that *listens* (backend.StartTCPReceiver). `adb reverse` routes
// device-initiated connections back to the host, which is the wrong
// direction for this topology and left nothing listening on the host side -
// the desktop Dial would just get connection-refused. `adb forward` routes
// host-initiated connections on the given local port through to the same
// port on the device, which is what we actually need here.
func SetupADBReverse(serial string, port string) error {
	cmd := exec.Command("adb", "-s", serial, "forward", "tcp:"+port, "tcp:"+port)
	return cmd.Run()
}

func RemoveADBReverse(serial string, port string) error {
	cmd := exec.Command("adb", "-s", serial, "forward", "--remove", "tcp:"+port)
	return cmd.Run()
}
