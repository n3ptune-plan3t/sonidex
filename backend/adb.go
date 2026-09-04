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

func ConnectWirelessADB(hostPort string) error {
	cmd := exec.Command("adb", "connect", hostPort)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("adb connect failed: %w: %s", err, strings.TrimSpace(string(out)))
	}
	result := strings.ToLower(strings.TrimSpace(string(out)))
	if strings.Contains(result, "unable to connect") || strings.Contains(result, "failed") || strings.Contains(result, "cannot connect") {
		return fmt.Errorf("adb connect failed: %s", strings.TrimSpace(string(out)))
	}
	return nil
}

func DisconnectWirelessADB(hostPort string) error {
	cmd := exec.Command("adb", "disconnect", hostPort)
	return cmd.Run()
}

func SetupADBReverse(serial string, port string) error {
	cmd := exec.Command("adb", "-s", serial, "forward", "tcp:"+port, "tcp:"+port)
	return cmd.Run()
}

func RemoveADBReverse(serial string, port string) error {
	cmd := exec.Command("adb", "-s", serial, "forward", "--remove", "tcp:"+port)
	return cmd.Run()
}
