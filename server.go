package httpx

import (
	"context"
	"errors"
	"net/http"
)

// Start serves on the configured address until shutdown. A nil server is a
// no-op, and http.ErrServerClosed — expected during a graceful shutdown — is
// reported as success.
func Start(server *http.Server) error {
	if server == nil {
		return nil
	}
	if err := server.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Close stops the HTTP server: Shutdown(ctx) drains the requests in flight,
// and a drain the caller's context cut short is followed by a forced close, so
// nothing is left serving once Close returns. Graceful alone is not enough —
// without the forced close a caller whose deadline expired has no way to get
// the connections down, which is what Engine.Stop promises.
//
// A forced close reports success: the drain degraded, but the server is down.
// A nil result therefore does not by itself mean every in-flight request
// finished. A Shutdown that failed for some other reason is force-closed too,
// but that cause is what Close returns.
//
// A nil server is a no-op.
func Close(ctx context.Context, server *http.Server) error {
	if server == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	err := server.Shutdown(ctx)
	if err == nil {
		return nil
	}
	if ctx.Err() == nil {
		// Shutdown failed for a reason other than the caller's context — a
		// listener or connection close error, say. Force-close anyway so
		// nothing keeps serving, but report the original cause.
		if closeErr := server.Close(); closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
			return errors.Join(err, closeErr)
		}
		return err
	}
	if closeErr := server.Close(); closeErr != nil && !errors.Is(closeErr, http.ErrServerClosed) {
		return closeErr
	}
	return nil
}
