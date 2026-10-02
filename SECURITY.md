# Security Policy

## Reporting a Vulnerability

If you discover a security vulnerability in emcp-go, please report it privately
through [GitHub Security Advisories](https://github.com/goco-ai/emcp-go/security/advisories/new).

Alternatively, email a.kolkov@gmail.com with details.

**Do not open a public issue for security vulnerabilities.**

## Response

We commit to:

- Acknowledging your report within **48 hours**
- Providing an initial assessment within **5 business days**
- Releasing a fix as soon as practical, coordinated with you

## Scope

emcp-go is a Go library that wraps the official MCP Go SDK and adds gRPC
transport. It does not run as a standalone service. Security concerns include:

- **gRPC transport** -- bidirectional stream handling; crafted protobuf payloads
  could cause unexpected behavior
- **Bearer token forwarding** -- HTTP transport forwards bearer tokens from
  PID files to daemon servers
- **PID file parsing** -- daemon discovery reads JSON from disk; malformed PID
  files should not cause crashes
- **Typed gRPC** -- Google canonical proto handling; crafted messages could
  cause panics in conversion code

## Supported Versions

Only the latest release receives security updates.
