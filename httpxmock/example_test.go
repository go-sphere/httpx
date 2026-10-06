package httpxmock_test

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/go-sphere/httpx"
	"github.com/go-sphere/httpx/httpxmock"
)

// requireUser is the middleware under test: it rejects a request without an
// X-User header and stores the user for the handlers below it.
func requireUser(next httpx.Handler) httpx.Handler {
	return func(ctx httpx.Context) error {
		user := ctx.Header("X-User")
		if user == "" {
			return ctx.JSON(http.StatusUnauthorized, map[string]string{"error": "no user"})
		}
		ctx.Set("user", user)
		return next(ctx)
	}
}

// Drive a middleware with a mock Context and a recording Handler, then read
// the response back off the Context.
func Example() {
	ctx := httpxmock.NewRequest(http.MethodGet, "/users/42", nil,
		httpxmock.WithFullPath("/users/:id"),
		httpxmock.WithParam("id", "42"),
	)
	next := &httpxmock.Handler{}
	if err := httpxmock.Run(ctx, next.Handle, requireUser); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println("reached handler:", next.Called())
	fmt.Println(ctx.StatusCode(), ctx.BodyString())
	// Output:
	// reached handler: false
	// 401 {"error":"no user"}
}

// Set Fn for a terminus that reads what the layer stored or writes a
// response of its own.
func ExampleHandler() {
	ctx := httpxmock.NewRequest(http.MethodGet, "/", nil,
		httpxmock.WithHeader("X-User", "ada"))
	next := &httpxmock.Handler{Fn: func(ctx httpx.Context) error {
		user, _ := ctx.Get("user")
		return ctx.Text(http.StatusOK, fmt.Sprint("hello ", user))
	}}
	if err := httpxmock.Run(ctx, next.Handle, requireUser); err != nil {
		fmt.Println("error:", err)
		return
	}
	fmt.Println(next.Calls(), ctx.StatusCode(), ctx.BodyString())
	// Output:
	// 1 200 hello ada
}

// LastJSON returns the Go value a handler passed to JSON, before encoding.
func ExampleContext_LastJSON() {
	type user struct{ ID int }
	ctx := httpxmock.New(nil)
	if err := ctx.JSON(http.StatusOK, user{ID: 7}); err != nil {
		fmt.Println("error:", err)
		return
	}
	v, ok := ctx.LastJSON()
	fmt.Printf("%v %+v\n", ok, v.(user))
	// Output:
	// true {ID:7}
}

// The Bind methods decode nothing on a mock; drive a real adapter such as
// stdx when a test needs binding.
func ExampleErrBindUnsupported() {
	ctx := httpxmock.New(nil)
	var dst struct{}
	err := ctx.BindJSON(&dst)
	fmt.Println(errors.Is(err, httpxmock.ErrBindUnsupported))
	// Output:
	// true
}
