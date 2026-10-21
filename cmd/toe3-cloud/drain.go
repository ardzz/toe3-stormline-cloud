package main

import (
	"context"
	"log/slog"
	"net/http"
	"sync"
	"time"
)

// handlerTracker admits requests to a handler until shutdown closes admission, and lets shutdown wait for the
// admitted ones.
type handlerTracker struct {
	mu      sync.Mutex
	closed  bool
	running sync.WaitGroup
}

// track wraps h so each request is admitted and counted while h handles it. A request that reaches the wrapper after
// admission closed gets 503 without reaching h; shutdown is closing its connection anyway.
func (t *handlerTracker) track(h http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !t.admit() {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		defer t.running.Done()
		h.ServeHTTP(w, r)
	})
}

// admit counts a request unless admission is closed. Counting under the lock keeps every Add before closeAndWait's
// Wait.
func (t *handlerTracker) admit() bool {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return false
	}
	t.running.Add(1)
	return true
}

// closeAndWait stops admitting requests and waits for the admitted ones to finish.
func (t *handlerTracker) closeAndWait() {
	t.mu.Lock()
	t.closed = true
	t.mu.Unlock()
	t.running.Wait()
}

// stopServing stops srv accepting requests and gives in-flight ones grace to finish. Requests still running after
// that are cancelled through cancelRequests (it cancels the context srv's BaseContext returns), and their connections
// are closed. It returns once admission is closed and every admitted handler has returned, so the dependencies the
// handlers use can be released afterwards.
func stopServing(srv *http.Server, grace time.Duration, cancelRequests context.CancelFunc, handlers *handlerTracker,
	log *slog.Logger) {
	ctx, cancel := context.WithTimeout(context.Background(), grace)
	defer cancel()
	if err := srv.Shutdown(ctx); err != nil {
		log.Warn("shutdown grace expired; cancelling the remaining requests", "error", err)
		cancelRequests()
		_ = srv.Close() // closes the remaining connections; its error concerns listeners, which are closed already
	}
	handlers.closeAndWait()
}
