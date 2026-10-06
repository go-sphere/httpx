// Package httpx defines a framework-agnostic HTTP layer: handlers,
// middleware, routers and engines written once against these interfaces run
// unchanged on Gin, Echo, Fiber, Hertz or plain net/http.
//
// This module holds only the contract and the helpers shared by every adapter;
// it has no third-party dependencies and cannot serve requests by itself.
// Pick an adapter module and construct its Engine:
//
//   - github.com/go-sphere/httpx/stdx — plain net/http, no framework dependency.
//   - github.com/go-sphere/httpx/ginx — Gin.
//   - github.com/go-sphere/httpx/echox — Echo v4.
//   - github.com/go-sphere/httpx/fiberx — Fiber v3.
//   - github.com/go-sphere/httpx/hertzx — Hertz.
//
// Every adapter's New returns an [Engine] and accepts the same portable
// options: WithAddr, WithErrorHandler and WithTrustedProxies.
//
// # Usage
//
//	import (
//		"context"
//		"log"
//		"net/http"
//		"time"
//
//		"github.com/go-sphere/httpx"
//		"github.com/go-sphere/httpx/stdx"
//	)
//
//	engine := stdx.New(stdx.WithAddr(":8080"))
//	engine.Use(func(next httpx.Handler) httpx.Handler {
//		return func(ctx httpx.Context) error {
//			ctx.SetHeader("X-Served-By", "httpx")
//			return next(ctx)
//		}
//	})
//	api := engine.Group("/api")
//	api.GET("/users/:id", func(ctx httpx.Context) error {
//		if ctx.Param("id") == "0" {
//			return httpx.NewNotFoundError("user not found")
//		}
//		return ctx.JSON(http.StatusOK, map[string]string{"id": ctx.Param("id")})
//	})
//
//	go func() {
//		if err := engine.Start(); err != nil { // blocks until Stop
//			log.Print(err)
//		}
//	}()
//	// ... on shutdown:
//	stopCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
//	defer cancel()
//	if err := engine.Stop(stopCtx); err != nil {
//		log.Print(err)
//	}
//
// Engines are single-use: after Stop, Start returns [ErrEngineClosed]. To serve
// a request in tests without a listener, probe the engine with
// [AsTestRequester].
//
// # Handlers, middleware and errors
//
// A [Handler] receives a [Context] and returns an error. A non-nil error is
// rendered by the adapter's error handler ([DefaultErrorHandler] unless
// WithErrorHandler replaced it) unless the response is already committed.
// Build errors with the status helpers such as [NewNotFoundError] and
// [BadRequestError], wrap binder failures with [WrapBindError], and use
// [ClassifyError] or [RenderError] to turn any error into a status and a
// body that does not leak err.Error().
//
// A [Middleware] wraps the rest of the chain; register it with Use on an
// Engine or a [Router]. Layers on the Engine also see requests no route
// matched; layers on a group do not. See [Middleware] and [MiddlewareChain]
// for the ordering rules.
//
// # Optional capabilities
//
// Features that not every adapter can offer are separate interfaces probed by
// type assertion: [AsNativeContext], [AsFlusher], [AsStreamer] (with
// [ServerSentEvents] on top), [MountStd] and [AsTestRequester].
//
// For unit tests of middleware and handlers without an engine, use
// github.com/go-sphere/httpx/httpxmock. Adapter authors certify an
// implementation with github.com/go-sphere/httpx/httpxtest.
package httpx
