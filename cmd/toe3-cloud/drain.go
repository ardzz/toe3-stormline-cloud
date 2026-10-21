package main

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// handlerTracker counts the requests being handled, so shutdown can wait until none is left.
type handlerTracker struct{ running sync.WaitGroup }

// track wraps h so each request is counted while h handles it.
func (t *handlerTracker) track(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.running.Add(1)
		defer t.running.Done()
		h.ServeHTTP(w, r)
	})
}

// stopServing stops srv accepting requests and gives in-flight ones grace to finish. Requests still running after
// that are cancelled through cancelRequests (it cancels the context srv's BaseContext returns), and their connections
// are closed. It returns once every tracked handler has returned, so the dependencies the handlers use can be released
// afterwards.
func stopServing(srv *http.Server, grace time.Duration, cancelRequests context.CancelFunc, handlers *handlerTracker,
	log *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Warn("shutdown grace expired; cancelling the remaining requests", "error", err)
		cancelRequests()
		_ = srv.Close() // closes the remaining connections; its error concerns listeners, which are closed already
	}
	handlers.running.Wait()
}
