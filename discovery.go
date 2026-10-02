// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

package emcp

import "github.com/grpmsoft/emcp-go/internal/mcpclient"

// DaemonInfo holds the data read from a daemon PID file.
type DaemonInfo struct {
	PID   int    `json:"pid"`
	Port  int    `json:"port"`
	Name  string `json:"name"`
	Token string `json:"token,omitempty"`
}

// Sentinel errors for PID file operations.
var (
	ErrPIDFileNotFound = mcpclient.ErrPIDFileNotFound
	ErrPIDFileEmpty    = mcpclient.ErrPIDFileEmpty
)

// ReadPIDFile reads and parses a daemon PID file. The file is expected to
// contain JSON with at least "pid" and "port" fields, matching the format
// written by the daemon library (github.com/grpmsoft/daemon).
//
// For backward compatibility, plain-text files containing only a PID number
// are also accepted, but the returned DaemonInfo will have Port=0 and
// empty Token.
func ReadPIDFile(path string) (*DaemonInfo, error) {
	info, err := mcpclient.ReadPIDFile(path)
	if err != nil {
		return nil, err
	}
	return &DaemonInfo{
		PID:   info.PID,
		Port:  info.Port,
		Name:  info.Name,
		Token: info.Token,
	}, nil
}
