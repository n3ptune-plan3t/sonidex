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

func SetupADBReverse(serial string, port string) error {
	cmd := exec.Command("adb", "-s", serial, "reverse", "tcp:"+port, "tcp:"+port)
	return cmd.Run()
}

func RemoveADBReverse(serial string, port string) error {
	cmd := exec.Command("adb", "-s", serial, "reverse", "--remove", "tcp:"+port)
	return cmd.Run()
}
