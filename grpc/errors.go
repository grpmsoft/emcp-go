// Copyright 2026 GOCO-AI. All rights reserved.
// Use of this source code is governed by an MIT-style license.

package grpc

import "errors"

// ErrNoServer is returned when the GRPCHandler's getServer function returns nil.
var ErrNoServer = errors.New("emcp-grpc: no server available")
