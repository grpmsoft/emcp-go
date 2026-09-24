package daemontx

import (
	"encoding/json/v2"
	"errors"
	"fmt"
	"os"
)

// pidInfo mirrors the JSON structure written by the daemon library.
// We intentionally do NOT import the daemon library — reading a small JSON
// file with os.ReadFile + json.Unmarshal keeps our dependency footprint minimal.
type pidInfo struct {
	PID   int    `json:"pid"`
	Port  int    `json:"port"`
	Name  string `json:"name"`
	Token string `json:"token,omitempty"`
}

// Discover reads a daemon PID file and extracts the port and bearer token.
//
// The PID file is a JSON object written by the daemon on startup:
//
//	{"pid":1234,"port":8094,"name":"gode","token":"xxx","startTime":"..."}
//
// Discover validates that the file exists, parses correctly, and contains
// a non-zero port. It returns sentinel errors that callers can match with
// errors.Is for control flow decisions.
func Discover(pidFilePath string) (port int, token string, err error) {
	data, err := os.ReadFile(pidFilePath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, "", ErrDaemonNotRunning
		}
		return 0, "", fmt.Errorf("reading PID file %s: %w", pidFilePath, err)
	}

	var info pidInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return 0, "", fmt.Errorf("%w: %s: %v", ErrPIDFileInvalid, pidFilePath, err)
	}

	if info.Port == 0 {
		return 0, "", ErrPortZero
	}

	return info.Port, info.Token, nil
}
