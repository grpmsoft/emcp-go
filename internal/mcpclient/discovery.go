// Copyright 2026 GOCO-AI. All rights reserved.
// Use of this source code is governed by an MIT-style license.

package mcpclient

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
)

// DaemonInfo holds the data read from a daemon PID file.
type DaemonInfo struct {
	PID   int    `json:"pid"`
	Port  int    `json:"port"`
	Name  string `json:"name"`
	Token string `json:"token,omitempty"`
}

// ErrPIDFileNotFound is returned when the PID file does not exist.
var ErrPIDFileNotFound = errors.New("pid file not found")

// ErrPIDFileEmpty is returned when the PID file is empty.
var ErrPIDFileEmpty = errors.New("pid file is empty")

// ReadPIDFile reads and parses a daemon PID file. The file is expected to
// contain JSON with at least "pid" and "port" fields, matching the format
// written by the daemon library (github.com/grpmsoft/daemon).
//
// For backward compatibility, plain-text files containing only a PID number
// are also accepted, but the returned DaemonInfo will have Port=0 and
// empty Token.
func ReadPIDFile(path string) (*DaemonInfo, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("%w: %s", ErrPIDFileNotFound, path)
		}
		return nil, fmt.Errorf("reading pid file %s: %w", path, err)
	}

	content := strings.TrimSpace(string(raw))
	if content == "" {
		return nil, fmt.Errorf("%w: %s", ErrPIDFileEmpty, path)
	}

	// Try JSON first.
	var info DaemonInfo
	if err := json.Unmarshal(raw, &info); err == nil && info.PID > 0 {
		return &info, nil
	}

	// Fall back to plain PID number.
	pid, err := strconv.Atoi(content)
	if err != nil {
		return nil, fmt.Errorf("parse pid file %s: not JSON and not a plain PID: %w", path, err)
	}
	return &DaemonInfo{PID: pid}, nil
}
