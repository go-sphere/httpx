package conformance

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/cloudwego/hertz/pkg/app/server"
	"github.com/cloudwego/hertz/pkg/common/hlog"
	"github.com/gin-gonic/gin"
	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/echox"
	"github.com/go-sphere/httpx/fiberx"
	"github.com/go-sphere/httpx/ginx"
	"github.com/go-sphere/httpx/hertzx"
	"github.com/go-sphere/httpx/httpxtest"
	"github.com/go-sphere/httpx/stdx"
	"github.com/gofiber/fiber/v3"
)

// adapter is one adapter under test: its shared suite, which builds the
// in-process engines and declares the capabilities, plus how it serves on a
// real listener.
type adapter struct {
	httpxtest.Suite
	// serve builds an engine that serves on a loopback address once started
	// and returns that address.
	serve func(tb testing.TB, cfg serveConfig) (httpx.Engine, string)
}

// serveConfig is what a real-listener test configures besides its routes.
type serveConfig struct {
	// trustProxies installs WithTrustedProxies(proxies...). Without it the
	// adapter keeps its framework's default forwarding-header policy.
	trustProxies bool
	proxies      []string
	maxBodySize  int64
}

// adapters is every adapter, in httpxtestSuites order: the one framework list
// of this module.
func adapters() []adapter {
	suites := httpxtestSuites()
	out := make([]adapter, len(suites))
	for i, suite := range suites {
		serve, ok := servers[suite.Name]
		if !ok {
			panic("conformance: no real-listener construction for " + suite.Name)
		}
		out[i] = adapter{Suite: suite, serve: serve}
	}
	return out
}

// servers builds each adapter's engine the way an application serving on a
// socket would: adapter-built, with only the listener and serveConfig applied.
// fiberx and hertzx are handed a bound listener; the net/http adapters bind
// their configured address in Start and offer no listener option, so they get
// a reserved one.
var servers = map[string]func(testing.TB, serveConfig) (httpx.Engine, string){
	"ginx": func(tb testing.TB, cfg serveConfig) (httpx.Engine, string) {
		tb.Helper()
		gin.SetMode(gin.ReleaseMode)
		addr := reserveAddr(tb)
		opts := []ginx.Option{ginx.WithAddr(addr), ginx.WithMaxBodySize(cfg.maxBodySize)}
		if cfg.trustProxies {
			opts = append(opts, ginx.WithTrustedProxies(cfg.proxies...))
		}
		return ginx.New(opts...), addr
	},
	"fiberx": func(tb testing.TB, cfg serveConfig) (httpx.Engine, string) {
		tb.Helper()
		ln := listenLoopback(tb)
		opts := []fiberx.Option{
			fiberx.WithListener(ln, fiber.ListenConfig{DisableStartupMessage: true}),
			fiberx.WithMaxBodySize(cfg.maxBodySize),
		}
		if cfg.trustProxies {
			opts = append(opts, fiberx.WithTrustedProxies(cfg.proxies...))
		}
		return fiberx.New(opts...), ln.Addr().String()
	},
	"echox": func(tb testing.TB, cfg serveConfig) (httpx.Engine, string) {
		tb.Helper()
		addr := reserveAddr(tb)
		opts := []echox.Option{echox.WithAddr(addr), echox.WithMaxBodySize(cfg.maxBodySize)}
		if cfg.trustProxies {
			opts = append(opts, echox.WithTrustedProxies(cfg.proxies...))
		}
		return echox.New(opts...), addr
	},
	"hertzx": func(tb testing.TB, cfg serveConfig) (httpx.Engine, string) {
		tb.Helper()
		hlog.SetSilentMode(true)
		hlog.SetOutput(io.Discard)
		ln := listenLoopback(tb)
		// The listener is a server.New option, so the server is built here;
		// it is what hertzx.New would build, bound to ln instead of an address.
		opts := []hertzx.Option{
			hertzx.WithEngine(server.New(server.WithListener(ln))),
			hertzx.WithMaxBodySize(cfg.maxBodySize),
		}
		if cfg.trustProxies {
			opts = append(opts, hertzx.WithTrustedProxies(cfg.proxies...))
		}
		return hertzx.New(opts...), ln.Addr().String()
	},
	"stdx": func(tb testing.TB, cfg serveConfig) (httpx.Engine, string) {
		tb.Helper()
		addr := reserveAddr(tb)
		opts := []stdx.Option{stdx.WithAddr(addr), stdx.WithMaxBodySize(cfg.maxBodySize)}
		if cfg.trustProxies {
			opts = append(opts, stdx.WithTrustedProxies(cfg.proxies...))
		}
		return stdx.New(opts...), addr
	},
}

// listenLoopback binds a loopback port for an engine that accepts a listener.
// The engine closes it on Stop; the cleanup covers an engine never started.
func listenLoopback(tb testing.TB) net.Listener {
	tb.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("listen: %v", err)
	}
	tb.Cleanup(func() { _ = ln.Close() })
	return ln
}

// reserveAddr picks a free loopback port for an engine that binds its own
// address. The port is released before the engine binds it, so another
// process could take it in between; the adapters taking a listener avoid that.
func reserveAddr(tb testing.TB) string {
	tb.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatalf("reserve addr: %v", err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return addr
}

// startServing runs engine.Start in the background and waits until addr
// accepts connections. The cleanup stops the engine and requires Start to
// have returned nil, as it must once Stop ended it.
func startServing(tb testing.TB, engine httpx.Engine, addr string) {
	tb.Helper()
	served := make(chan error, 1)
	go func() { served <- engine.Start() }()
	tb.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		defer cancel()
		_ = engine.Stop(ctx)
		select {
		case err := <-served:
			if err != nil {
				tb.Errorf("Start = %v after Stop, want nil", err)
			}
		case <-time.After(5 * time.Second):
			tb.Error("Start did not return after Stop")
		}
	})
	waitReachable(tb, engine, addr)
}

func waitReachable(tb testing.TB, engine httpx.Engine, addr string) {
	tb.Helper()
	client := oneShotClient(200 * time.Millisecond)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if engine.IsRunning() {
			resp, err := client.Get("http://" + addr + "/__httpx_ready__")
			if err == nil {
				if resp != nil {
					_ = resp.Body.Close()
				}
				return
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	tb.Fatalf("engine on %s did not become reachable", addr)
}

func dialable(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 200*time.Millisecond)
	if err != nil {
		return false
	}
	_ = conn.Close()
	return true
}

type responseSnapshot struct {
	Status  int
	Body    string
	Headers http.Header
}

// serveInProcess serves req through the engine's httpx.TestRequester. It
// reports failures as errors, so it can be called from any goroutine.
func serveInProcess(engine httpx.Engine, req *http.Request) (responseSnapshot, error) {
	requester, ok := httpx.AsTestRequester(engine)
	if !ok {
		return responseSnapshot{}, fmt.Errorf("%T does not implement httpx.TestRequester", engine)
	}
	resp, err := requester.Do(req)
	if err != nil {
		return responseSnapshot{}, err
	}
	defer func() { _ = resp.Body.Close() }()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return responseSnapshot{}, fmt.Errorf("read response body: %w", err)
	}
	return responseSnapshot{Status: resp.StatusCode, Body: string(body), Headers: resp.Header}, nil
}

// mustServe is serveInProcess on the test goroutine, failing t on error.
func mustServe(t *testing.T, engine httpx.Engine, method, target string) responseSnapshot {
	t.Helper()
	got, err := serveInProcess(engine, httptest.NewRequest(method, target, nil))
	if err != nil {
		t.Fatalf("%s %s: %v", method, target, err)
	}
	return got
}
