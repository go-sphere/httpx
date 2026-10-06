package httpx_test

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/httpxmock"
)

// A middleware wraps the rest of the chain. This one stops the chain with a
// 401 when a header is missing; the error is rendered by the adapter at the
// route, so here it is classified explicitly to show what a client receives.
func ExampleMiddleware() {
	requireToken := func(next httpx.Handler) httpx.Handler {
		return func(ctx httpx.Context) error {
			if ctx.Header("Authorization") == "" {
				return httpx.NewUnauthorizedError("missing token")
			}
			return next(ctx)
		}
	}
	handler := func(ctx httpx.Context) error {
		return ctx.Text(http.StatusOK, "hello")
	}

	allowed := httpxmock.NewRequest(http.MethodGet, "/", nil,
		httpxmock.WithHeader("Authorization", "Bearer t"))
	if err := httpx.ComposeMiddleware(handler, []httpx.Middleware{requireToken})(allowed); err != nil {
		fmt.Println("unexpected:", err)
		return
	}
	fmt.Println(allowed.StatusCode(), allowed.BodyString())

	denied := httpxmock.NewRequest(http.MethodGet, "/", nil)
	err := httpx.ComposeMiddleware(handler, []httpx.Middleware{requireToken})(denied)
	status, body := httpx.RenderError(err)
	fmt.Println(status, body.Message, denied.Committed())
	// Output:
	// 200 hello
	// 401 missing token false
}

// RenderError never puts err.Error() in the body: an error without a user
// message is reported with the generic status text.
func ExampleRenderError() {
	for _, err := range []error{
		httpx.NewNotFoundError("user not found"),
		httpx.BadRequestError(errors.New("strconv: invalid syntax")),
		httpx.WrapBindError(errors.New("json: unexpected end of input")),
		errors.New("dial tcp 10.0.0.1:5432: connection refused"),
	} {
		status, body := httpx.RenderError(err)
		fmt.Printf("%d %q\n", status, body.Message)
	}
	// Output:
	// 404 "user not found"
	// 400 "Bad Request"
	// 400 "Bad Request"
	// 500 "Internal Server Error"
}

func ExampleWithStatus() {
	err := httpx.WithStatus(http.StatusConflict, errors.New("unique violation"), "email already registered")
	code, status, message := httpx.ParseError(err)
	fmt.Println(code, status, message)
	fmt.Println(err)
	// Output:
	// 0 409 email already registered
	// unique violation
}

// ServerSentEvents commits a text/event-stream response and hands the
// callback a writer that encodes one event per write.
func ExampleServerSentEvents() {
	ctx := httpxmock.NewRequest(http.MethodGet, "/events", nil)
	err := httpx.ServerSentEvents(ctx, func(w *httpx.SSEWriter) error {
		if err := w.Send(&httpx.SSEEvent{ID: "1", Event: "greeting", Data: "hello"}); err != nil {
			return err
		}
		return w.SendJSON("", map[string]int{"n": 2})
	})
	if err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(ctx.StatusCode(), ctx.ResponseHeader("Content-Type"))
	fmt.Print(ctx.BodyString())
	// Output:
	// 200 text/event-stream
	// event: greeting
	// id: 1
	// data: hello
	//
	// data: {"n":2}
}
