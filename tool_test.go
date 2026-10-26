// Copyright 2026 GOCO-AI Authors.
// SPDX-License-Identifier: Apache-2.0

package emcp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	emcpv1 "github.com/grpmsoft/emcp-go/grpc/proto/emcpv1"
	"google.golang.org/grpc"
)

type greetIn struct {
	Name  string `json:"name" jsonschema:"who to greet"`
	Times int    `json:"times,omitempty" jsonschema:"how many greetings"`
	Style string `json:"style,omitempty" jsonschema:"plain or loud"`
}

type greetOut struct {
	Greeting string `json:"greeting"`
	Count    int    `json:"count"`
}

type snapIn struct{}

type snapOut struct {
	Width  int `json:"width"`
	Height int `json:"height"`
}

var errNoName = errors.New("name must not be blank")

// newTypedServer returns a server with typed tools: "greet" with a tuned input
// schema (enum, range, default) and annotations, "snap" returning an image
// next to its structured result, and the untyped "echo" from newTestServer.
func newTypedServer(t *testing.T, cfg ServerConfig) *Server {
	t.Helper()
	srv := NewServer(cfg)

	schema, err := InferSchema[greetIn]()
	if err != nil {
		t.Fatalf("InferSchema: %v", err)
	}
	schema.Properties["style"].Enum = []any{"plain", "loud"}
	schema.Properties["times"].Minimum = ptr(1.0)
	schema.Properties["times"].Maximum = ptr(3.0)
	schema.Properties["times"].Default = json.RawMessage(`1`)

	AddTool(srv, Tool{
		Name:        "greet",
		Description: "greets someone",
		InputSchema: schema,
		Annotations: &ToolAnnotations{Title: "Greeter", ReadOnlyHint: true, OpenWorldHint: ptr(false)},
	}, func(_ context.Context, in greetIn) (*ToolResult, greetOut, error) {
		if strings.TrimSpace(in.Name) == "" {
			return nil, greetOut{}, errNoName
		}
		g := "hello " + in.Name
		if in.Style == "loud" {
			g = strings.ToUpper(g)
		}
		return nil, greetOut{Greeting: g, Count: in.Times}, nil
	})

	AddTool(srv, Tool{Name: "snap", Description: "returns an image"},
		func(_ context.Context, _ snapIn) (*ToolResult, snapOut, error) {
			return &ToolResult{Content: []ContentItem{
				{Type: "image", MIMEType: "image/png", Data: []byte{0x89, 'P', 'N', 'G'}},
			}}, snapOut{Width: 2, Height: 1}, nil
		})

	srv.AddTool("echo", "untyped", nil, func(_ context.Context, _ string, args map[string]any) (*ToolResult, error) {
		msg, _ := args["msg"].(string)
		return TextResult("echo: " + msg), nil
	})
	return srv
}

func ptr[T any](v T) *T { return &v }

// structured decodes a result's structured content into T, the way a client
// of a typed tool reads it.
func structured[T any](t *testing.T, res *ToolResult) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(res.StructuredContent, &v); err != nil {
		t.Fatalf("structured content %s: %v", res.StructuredContent, err)
	}
	return v
}

// typedTransports runs f against srv over HTTP and over the gRPC stream
// transport: typed tools must behave the same on both.
func typedTransports(t *testing.T, srv *Server, f func(t *testing.T, client *Client)) {
	t.Run("http", func(t *testing.T) {
		client, cleanup := newHTTPServerAndClient(t, srv)
		defer cleanup()
		f(t, client)
	})
	t.Run("grpc", func(t *testing.T) {
		client, cleanup := newGRPCServerAndClient(t, srv)
		defer cleanup()
		f(t, client)
	})
}

func callCtx(t *testing.T) context.Context {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)
	return ctx
}

func TestAddTool_ListsSchemasAndAnnotations(t *testing.T) {
	srv := newTypedServer(t, ServerConfig{Name: "typed", Version: "0.2.0"})
	typedTransports(t, srv, func(t *testing.T, client *Client) {
		tools, err := client.ListTools(callCtx(t))
		if err != nil {
			t.Fatalf("ListTools: %v", err)
		}
		byName := map[string]*ToolInfo{}
		for _, tool := range tools {
			byName[tool.Name] = tool
		}

		greet := byName["greet"]
		if greet == nil {
			t.Fatalf("greet not listed: %v", tools)
		}
		props, _ := greet.InputSchema["properties"].(map[string]any)
		name, _ := props["name"].(map[string]any)
		if name["description"] != "who to greet" {
			t.Errorf("name property = %v, want the jsonschema tag as description", name)
		}
		style, _ := props["style"].(map[string]any)
		if enum, _ := style["enum"].([]any); len(enum) != 2 {
			t.Errorf("style enum = %v, want the tuned enum", style["enum"])
		}
		if req, _ := greet.InputSchema["required"].([]any); len(req) != 1 || req[0] != "name" {
			t.Errorf("required = %v, want [name]: omitempty fields are optional", greet.InputSchema["required"])
		}
		if greet.InputSchema["additionalProperties"] == nil {
			t.Error("additionalProperties not set: unknown arguments would pass")
		}
		if greet.OutputSchema == nil {
			t.Error("OutputSchema = nil, want the schema inferred from greetOut")
		}
		a := greet.Annotations
		if a == nil || a.Title != "Greeter" || !a.ReadOnlyHint || a.OpenWorldHint == nil || *a.OpenWorldHint {
			t.Errorf("annotations = %+v, want the title, readOnly and a closed world", a)
		}

		if byName["echo"] == nil || byName["echo"].OutputSchema != nil || byName["echo"].Annotations != nil {
			t.Errorf("untyped echo = %+v, want it listed without output schema or annotations", byName["echo"])
		}
	})
}

func TestAddTool_CallReturnsStructuredResult(t *testing.T) {
	srv := newTypedServer(t, ServerConfig{Name: "typed", Version: "0.2.0"})
	typedTransports(t, srv, func(t *testing.T, client *Client) {
		res, err := client.CallTool(callCtx(t), "greet", map[string]any{"name": "ann", "style": "loud"})
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		if res.IsError {
			t.Fatalf("IsError: %+v", res.Content)
		}
		got := structured[greetOut](t, res)
		if got.Greeting != "HELLO ANN" {
			t.Errorf("structured greeting = %q, want HELLO ANN", got.Greeting)
		}
		if got.Count != 1 {
			t.Errorf("structured count = %d, want 1: the schema default must apply", got.Count)
		}
		if len(res.Content) != 1 || !strings.Contains(res.Content[0].Text, `"greeting":"HELLO ANN"`) {
			t.Errorf("content = %+v, want the JSON text fallback", res.Content)
		}
	})
}

func TestAddTool_ExtraContentKeepsStructuredResult(t *testing.T) {
	srv := newTypedServer(t, ServerConfig{Name: "typed", Version: "0.2.0"})
	typedTransports(t, srv, func(t *testing.T, client *Client) {
		res, err := client.CallTool(callCtx(t), "snap", nil)
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		if len(res.Content) != 1 || res.Content[0].Type != "image" || res.Content[0].MIMEType != "image/png" {
			t.Fatalf("content = %+v, want the handler's image only", res.Content)
		}
		if got := structured[snapOut](t, res); got != (snapOut{Width: 2, Height: 1}) {
			t.Errorf("structured = %+v, want the Out value", got)
		}
	})
}

func TestAddTool_FailuresAreToolErrors(t *testing.T) {
	srv := newTypedServer(t, ServerConfig{Name: "typed", Version: "0.2.0"})
	tests := []struct {
		name string
		args map[string]any
		want string
	}{
		{"missing required", map[string]any{}, "name"},
		{"wrong type", map[string]any{"name": 42}, "name"},
		{"unknown argument", map[string]any{"name": "a", "nmae": "b"}, "nmae"},
		{"enum", map[string]any{"name": "a", "style": "quiet"}, "style"},
		{"range", map[string]any{"name": "a", "times": 9}, "times"},
		{"handler error", map[string]any{"name": "  "}, errNoName.Error()},
	}
	typedTransports(t, srv, func(t *testing.T, client *Client) {
		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				res, err := client.CallTool(callCtx(t), "greet", tt.args)
				if err != nil {
					t.Fatalf("protocol error %v, want a tool error the model can read", err)
				}
				if !res.IsError {
					t.Fatalf("IsError = false; result %+v", res)
				}
				if len(res.Content) == 0 || !strings.Contains(res.Content[0].Text, tt.want) {
					t.Errorf("content = %+v, want text naming %q", res.Content, tt.want)
				}
			})
		}
	})
}

// TestAddTool_BehindAuth checks that typed tools sit behind the same bearer
// authentication as untyped ones, on both transports.
func TestAddTool_BehindAuth(t *testing.T) {
	cfg := ServerConfig{Name: "typed", Version: "0.2.0", TokenValidator: StaticToken("s3cret")}

	t.Run("http", func(t *testing.T) {
		ts := httptest.NewServer(newTypedServer(t, cfg).HTTPHandler())
		defer ts.Close()

		if _, err := NewClient(Config{Endpoint: ts.URL, Transport: TransportHTTP}); err == nil {
			t.Fatal("connected without a token")
		}
		client, err := NewClient(Config{Endpoint: ts.URL, Transport: TransportHTTP, BearerToken: "s3cret"})
		if err != nil {
			t.Fatalf("NewClient with token: %v", err)
		}
		defer func() { _ = client.Close() }()
		res, err := client.CallTool(callCtx(t), "greet", map[string]any{"name": "ann"})
		if err != nil || res.IsError {
			t.Fatalf("CallTool = %+v, %v", res, err)
		}
	})

	t.Run("grpc", func(t *testing.T) {
		lis, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		gs := grpc.NewServer()
		emcpv1.RegisterMCPTransportServer(gs, newTypedServer(t, cfg).GRPCHandler())
		go func() { _ = gs.Serve(lis) }()
		defer gs.Stop()

		if _, err := NewClient(Config{Endpoint: lis.Addr().String(), Transport: TransportGRPC}); err == nil {
			t.Fatal("connected without a token")
		}
		client, err := NewClient(Config{Endpoint: lis.Addr().String(), Transport: TransportGRPC, BearerToken: "s3cret"})
		if err != nil {
			t.Fatalf("NewClient with token: %v", err)
		}
		defer func() { _ = client.Close() }()
		res, err := client.CallTool(callCtx(t), "greet", map[string]any{"name": "ann"})
		if err != nil || res.IsError {
			t.Fatalf("CallTool = %+v, %v", res, err)
		}
	})
}

type optsIn struct {
	Size int `json:"size,omitempty" jsonschema:"size, default 7"`
}

type optsOut struct {
	Size int `json:"size"`
}

// TestAddTool_NullArgumentsWithDefaults reproduces a crash: a call without
// arguments arrives as "arguments": null, and the SDK panicked writing the
// schema defaults into the nil map it decoded, killing the server process.
func TestAddTool_NullArgumentsWithDefaults(t *testing.T) {
	srv := NewServer(ServerConfig{Name: "typed", Version: "0.2.0"})
	schema, err := InferSchema[optsIn]()
	if err != nil {
		t.Fatal(err)
	}
	schema.Properties["size"].Default = json.RawMessage(`7`)
	AddTool(srv, Tool{Name: "opts", InputSchema: schema},
		func(_ context.Context, in optsIn) (*ToolResult, optsOut, error) {
			return nil, optsOut(in), nil
		})

	typedTransports(t, srv, func(t *testing.T, client *Client) {
		// The emcp client sends a nil args map as JSON null.
		res, err := client.CallTool(callCtx(t), "opts", nil)
		if err != nil {
			t.Fatalf("CallTool: %v", err)
		}
		if res.IsError || structured[optsOut](t, res).Size != 7 {
			t.Fatalf("result = %+v, want size 7 from the default", res)
		}
	})
}

// TestToolPanicIsToolError checks that a panicking tool, typed or untyped,
// costs one call a tool error and leaves the server serving.
func TestToolPanicIsToolError(t *testing.T) {
	srv := NewServer(ServerConfig{Name: "typed", Version: "0.2.0", Logger: slog.New(slog.DiscardHandler)})
	AddTool(srv, Tool{Name: "boom"}, func(_ context.Context, _ snapIn) (*ToolResult, snapOut, error) {
		panic("typed handler bug")
	})
	srv.AddTool("boom_untyped", "panics", nil, func(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
		panic("untyped handler bug")
	})
	srv.AddTool("ok", "works", nil, func(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
		return TextResult("fine"), nil
	})

	typedTransports(t, srv, func(t *testing.T, client *Client) {
		for tool, want := range map[string]string{"boom": "typed handler bug", "boom_untyped": "untyped handler bug"} {
			res, err := client.CallTool(callCtx(t), tool, map[string]any{})
			if err != nil {
				t.Fatalf("%s: protocol error %v, want a tool error", tool, err)
			}
			if !res.IsError || len(res.Content) == 0 || !strings.Contains(res.Content[0].Text, want) ||
				!strings.Contains(res.Content[0].Text, tool) {
				t.Fatalf("%s: result %+v, want a tool error naming the tool and the panic", tool, res)
			}
		}
		res, err := client.CallTool(callCtx(t), "ok", nil)
		if err != nil || res.IsError || res.Content[0].Text != "fine" {
			t.Fatalf("server after the panics: %+v, %v", res, err)
		}
	})
}

func TestAddTool_PanicsOnNonObjectInput(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Fatal("AddTool with In=int did not panic")
		}
	}()
	srv := NewServer(ServerConfig{Name: "typed"})
	AddTool(srv, Tool{Name: "bad"}, func(_ context.Context, _ int) (*ToolResult, any, error) {
		return nil, nil, nil
	})
}

func TestInferSchema(t *testing.T) {
	s, err := InferSchema[greetIn]()
	if err != nil {
		t.Fatalf("InferSchema: %v", err)
	}
	if s.Type != "object" || s.Properties["name"] == nil || s.Properties["name"].Description != "who to greet" {
		t.Fatalf("schema = %+v, want an object with described properties", s)
	}
}

type listIn struct{}

// TestAddTool_NonObjectOutReachesClient checks that a typed tool whose Out
// is not an object (allowed by SEP-2106) still reaches an emcp client as
// structured content, not only as text.
func TestAddTool_NonObjectOutReachesClient(t *testing.T) {
	srv := NewServer(ServerConfig{Name: "typed", Version: "0.2.0"})
	AddTool(srv, Tool{Name: "list"}, func(_ context.Context, _ listIn) (*ToolResult, []string, error) {
		return nil, []string{"a", "b"}, nil
	})
	typedTransports(t, srv, func(t *testing.T, client *Client) {
		res, err := client.CallTool(callCtx(t), "list", nil)
		if err != nil || res.IsError {
			t.Fatalf("CallTool = %+v, %v", res, err)
		}
		if got := structured[[]string](t, res); len(got) != 2 || got[0] != "a" || got[1] != "b" {
			t.Errorf("structured = %v, want [a b]", got)
		}
	})
}

// TestServerConfig_LoggerGetsRecoveredPanic checks that a recovered tool
// panic goes to the configured logger, with the tool's name.
func TestServerConfig_LoggerGetsRecoveredPanic(t *testing.T) {
	var buf syncBuffer
	srv := NewServer(ServerConfig{
		Name:   "typed",
		Logger: slog.New(slog.NewTextHandler(&buf, nil)),
	})
	srv.AddTool("boom", "panics", nil, func(_ context.Context, _ string, _ map[string]any) (*ToolResult, error) {
		panic("logged bug")
	})
	client, cleanup := newHTTPServerAndClient(t, srv)
	defer cleanup()
	if res, err := client.CallTool(callCtx(t), "boom", nil); err != nil || !res.IsError {
		t.Fatalf("CallTool = %+v, %v", res, err)
	}
	if out := buf.String(); !strings.Contains(out, "panic in tool call") || !strings.Contains(out, "tool=boom") ||
		!strings.Contains(out, "logged bug") {
		t.Errorf("log = %q, want the recovered panic with the tool name", out)
	}
}

// syncBuffer is a bytes.Buffer safe for the logger's and the test's goroutines.
type syncBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

type waitIn struct{}

// TestHTTP_ClientDisconnectCancelsHandler checks that a client giving up a
// call over HTTP cancels the handler (the emcp client speaks protocol
// 2026-07-28), and that DisableRequestCancellation keeps it running.
func TestHTTP_ClientDisconnectCancelsHandler(t *testing.T) {
	for _, disable := range []bool{false, true} {
		t.Run(fmt.Sprintf("disabled=%v", disable), func(t *testing.T) {
			srv := NewServer(ServerConfig{Name: "typed", DisableRequestCancellation: disable})
			started := make(chan struct{}, 1)
			cancelled := make(chan struct{}, 1)
			release := make(chan struct{})
			AddTool(srv, Tool{Name: "wait"}, func(ctx context.Context, _ waitIn) (*ToolResult, any, error) {
				started <- struct{}{}
				select {
				case <-ctx.Done():
					cancelled <- struct{}{}
				case <-release:
				}
				return nil, nil, nil
			})
			client, cleanup := newHTTPServerAndClient(t, srv)
			defer cleanup()
			// Let a handler that keeps running end before the server closes.
			defer close(release)

			ctx, cancel := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() {
				_, err := client.CallTool(ctx, "wait", nil)
				done <- err
			}()
			select {
			case <-started:
			case <-time.After(5 * time.Second):
				t.Fatal("handler did not start")
			}
			cancel()
			if err := <-done; err == nil {
				t.Error("the call succeeded although its ctx was cancelled")
			}

			select {
			case <-cancelled:
				if disable {
					t.Error("handler cancelled although cancellation is disabled")
				}
			case <-time.After(2 * time.Second):
				if !disable {
					t.Error("handler still running 2 s after the client gave up")
				}
			}
		})
	}
}
