package httpxtest

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-sphere/httpx"
)

func init() {
	register("Commit", casesCommit)
}

// The Commit cases pin what happens to a response after it is committed, and
// what Committed reports about it.
//
// The contract is net/http's, measured rather than inferred: a superfluous
// WriteHeader is ignored, a Header change after the header is out is dropped,
// and a further Write appends and returns no error. gin, echo and stdx get all
// of that from the ResponseWriter they write through; fiber and hertz buffer
// the whole response and used to let a second write replace the status, the
// headers and — on fiber — the body, so a handler that wrote and then failed
// answered with the failure's status over the success it had already sent.
//
// Committed is the reason the group exists. Nothing in a post-commit write's
// return value says the response was already committed, so a convention layer
// above httpx — the recover middleware that renders an error body — cannot tell
// a handler that failed before writing from one that failed after, and appends
// a second JSON document to the response the client is already receiving.
func casesCommit(t *testing.T, r runner) {
	// The predicate is "has the response header been written", so it holds for
	// a write with an empty body and not for a status nothing has sent yet.
	t.Run("CommittedTracksTheFirstWrite", func(t *testing.T) {
		var before, afterStatus, afterEmptyWrite bool
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/commit/tracks", func(ctx httpx.Context) error {
				before = ctx.Committed()
				ctx.Status(http.StatusCreated)
				afterStatus = ctx.Committed()
				if err := ctx.Text(http.StatusOK, ""); err != nil {
					return err
				}
				afterEmptyWrite = ctx.Committed()
				return nil
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/commit/tracks", nil))

		if before {
			t.Error("Committed = true before anything was written")
		}
		if afterStatus {
			t.Error("Committed = true after a bare Status: it sends no header of its own")
		}
		if !afterEmptyWrite {
			t.Error("Committed = false after a write with an empty body")
		}
		if got.Status != http.StatusOK {
			t.Fatalf("status = %d, want %d", got.Status, http.StatusOK)
		}
	})

	// The status freezes at the committing write, and later bytes append to it.
	t.Run("SecondWriteAppendsAndKeepsTheStatus", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/commit/append", func(ctx httpx.Context) error {
				if err := ctx.Text(http.StatusOK, "first"); err != nil {
					return err
				}
				return ctx.Text(http.StatusInternalServerError, "second")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/commit/append", nil))

		if got.Status != http.StatusOK {
			t.Errorf("status = %d, want %d: the committing write decides it", got.Status, http.StatusOK)
		}
		if got.Body != "firstsecond" {
			t.Errorf("body = %q, want %q: a later write appends", got.Body, "firstsecond")
		}
	})

	// Neither a bare Status nor the code a later write passes can move it.
	t.Run("PostCommitStatusIsIgnored", func(t *testing.T) {
		var reported int
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/commit/status", func(ctx httpx.Context) error {
				if err := ctx.Text(http.StatusAccepted, "body"); err != nil {
					return err
				}
				ctx.Status(http.StatusInternalServerError)
				reported = ctx.StatusCode()
				return nil
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/commit/status", nil))

		if got.Status != http.StatusAccepted {
			t.Errorf("status = %d, want %d", got.Status, http.StatusAccepted)
		}
		if reported != http.StatusAccepted {
			t.Errorf("StatusCode() = %d, want %d: the ignored Status must not be reported either",
				reported, http.StatusAccepted)
		}
	})

	// Headers freeze with the status: a later SetHeader, a later SetCookie and
	// the Content-Type a later write would have set are all dropped.
	t.Run("PostCommitHeadersAreDropped", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/commit/headers", func(ctx httpx.Context) error {
				if err := ctx.Text(http.StatusOK, "first"); err != nil {
					return err
				}
				ctx.SetHeader("X-Late", "yes")
				ctx.SetCookie(&http.Cookie{Name: "late", Value: "yes", Path: "/"})
				return ctx.Bytes(http.StatusInternalServerError, []byte("|second"), "application/json")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/commit/headers", nil))

		if late := got.Headers.Get("X-Late"); late != "" {
			t.Errorf("X-Late = %q, want it dropped", late)
		}
		if cookie := got.Headers.Get("Set-Cookie"); cookie != "" {
			t.Errorf("Set-Cookie = %q, want it dropped", cookie)
		}
		if ct := got.Headers.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
			t.Errorf("Content-Type = %q, want the committed %q", ct, "text/plain; charset=utf-8")
		}
		if got.Body != "first|second" {
			t.Errorf("body = %q, want %q: the bytes still append", got.Body, "first|second")
		}
	})

	// A post-commit write produces no error, which is exactly why Committed has
	// to exist: the return value cannot carry the fact.
	t.Run("PostCommitWriteReportsNoError", func(t *testing.T) {
		var second error
		var committed bool
		r.serve(t, func(router httpx.Router) {
			router.GET("/commit/noerror", func(ctx httpx.Context) error {
				if err := ctx.Text(http.StatusOK, "first"); err != nil {
					return err
				}
				second = ctx.JSON(http.StatusInternalServerError, map[string]any{"message": "boom"})
				committed = ctx.Committed()
				return nil
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/commit/noerror", nil))

		if second != nil {
			t.Errorf("second write returned %v, want nil", second)
		}
		if !committed {
			t.Error("Committed = false after two writes")
		}
	})

	// The body appends whatever bytes the call produces, so a call that
	// produces none leaves the response exactly as it stands.
	t.Run("PostCommitBodylessWriteChangesNothing", func(t *testing.T) {
		got := r.serve(t, func(router httpx.Router) {
			router.GET("/commit/bodyless", func(ctx httpx.Context) error {
				if err := ctx.Text(http.StatusOK, "first"); err != nil {
					return err
				}
				if err := ctx.NoContent(http.StatusNoContent); err != nil {
					return err
				}
				return ctx.Redirect(http.StatusFound, "/elsewhere")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/commit/bodyless", nil))

		if got.Status != http.StatusOK {
			t.Errorf("status = %d, want %d", got.Status, http.StatusOK)
		}
		if got.Body != "first" {
			t.Errorf("body = %q, want %q", got.Body, "first")
		}
		if loc := got.Headers.Get("Location"); loc != "" {
			t.Errorf("Location = %q, want it dropped", loc)
		}
	})

	// The read the downstream recover middleware makes: after next returns, a
	// layer can tell a handler that already answered from one that failed with
	// nothing written, and only the second may be given an error body.
	t.Run("LayerSeesCommittedAfterNext", func(t *testing.T) {
		observe := func(seen *bool) httpx.Middleware {
			return func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) error {
					err := next(ctx)
					*seen = ctx.Committed()
					return err
				}
			}
		}

		var afterWrite bool
		r.serve(t, func(router httpx.Router) {
			router.Use(observe(&afterWrite))
			router.GET("/commit/layer/wrote", func(ctx httpx.Context) error {
				return ctx.Text(http.StatusOK, "handler")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/commit/layer/wrote", nil))

		if !afterWrite {
			t.Error("Committed = false after a handler that wrote a response")
		}

		var afterFailure bool
		got := r.serve(t, func(router httpx.Router) {
			router.Use(observe(&afterFailure))
			router.GET("/commit/layer/failed", func(httpx.Context) error {
				return httpx.NewBadRequestError("no-write")
			})
		}, httptest.NewRequest(http.MethodGet, "http://example.com/commit/layer/failed", nil))

		if afterFailure {
			t.Error("Committed = true after a handler that failed without writing")
		}
		if got.Status != http.StatusBadRequest {
			t.Fatalf("status = %d, want %d", got.Status, http.StatusBadRequest)
		}
	})

	// The downstream defect this contract exists to make fixable: a recover
	// layer that renders an error body unconditionally emits two documents, and
	// the same layer gated on Committed emits one.
	t.Run("CommittedGatesASecondErrorBody", func(t *testing.T) {
		recoverLayer := func(gated bool) httpx.Middleware {
			return func(next httpx.Handler) httpx.Handler {
				return func(ctx httpx.Context) (err error) {
					defer func() {
						if rec := recover(); rec != nil {
							if gated && ctx.Committed() {
								err = nil
								return
							}
							err = ctx.Text(http.StatusInternalServerError, fmt.Sprintf("|recovered:%v", rec))
						}
					}()
					return next(ctx)
				}
			}
		}
		panicAfterWriting := func(ctx httpx.Context) error {
			if err := ctx.Text(http.StatusOK, "sent"); err != nil {
				return err
			}
			panic("boom")
		}

		ungated := r.serve(t, func(router httpx.Router) {
			router.Use(recoverLayer(false))
			router.GET("/commit/recover/ungated", panicAfterWriting)
		}, httptest.NewRequest(http.MethodGet, "http://example.com/commit/recover/ungated", nil))

		if ungated.Body != "sent|recovered:boom" {
			t.Errorf("ungated body = %q, want %q: the second body appends to the first",
				ungated.Body, "sent|recovered:boom")
		}
		if ungated.Status != http.StatusOK {
			t.Errorf("ungated status = %d, want %d", ungated.Status, http.StatusOK)
		}

		gated := r.serve(t, func(router httpx.Router) {
			router.Use(recoverLayer(true))
			router.GET("/commit/recover/gated", panicAfterWriting)
		}, httptest.NewRequest(http.MethodGet, "http://example.com/commit/recover/gated", nil))

		if gated.Body != "sent" {
			t.Errorf("gated body = %q, want %q", gated.Body, "sent")
		}
		if gated.Status != http.StatusOK {
			t.Errorf("gated status = %d, want %d", gated.Status, http.StatusOK)
		}
	})
}
