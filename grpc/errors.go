// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

package grpc

import "errors"

// ErrNoServer is returned when the GRPCHandler's getServer function returns nil.
var ErrNoServer = errors.New("emcp-grpc: no server available")
