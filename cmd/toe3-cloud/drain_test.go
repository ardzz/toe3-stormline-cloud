package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestStopServingCancelsAndJoinsRequestsThatOutliveTheGrace(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	reqCtx, cancelRequests := context.WithCancel(context.Background())
	defer cancelRequests()
	var handlers handlerTracker
	entered, cancelled, release := make(chan struct{}), make(chan struct{}), make(chan struct{})
	srv := &http.Server{
		BaseContext: func(net.Listener) context.Context { return reqCtx },
		Handler: handlers.track(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(entered)
			<-r.Context().Done() // a request that never finishes on its own
			close(cancelled)
			<-release // still cleaning up after cancellation
		})),
	}
	go func() { _ = srv.Serve(ln) }()
	go func() {
		if res, err := http.Get("http://" + ln.Addr().String()); err == nil {
			res.Body.Close()
		}
	}()
	waitFor(t, entered, "request never reached the handler")
	stopped := make(chan struct{})
	go func() {
		stopServing(srv, 10*time.Millisecond, cancelRequests, &handlers, slog.New(slog.DiscardHandler))
		close(stopped)
	}()
	waitFor(t, cancelled, "the request was not cancelled after the grace")
	select {
	case <-stopped:
		t.Fatal("stopServing returned while a handler was still running")
	case <-time.After(50 * time.Millisecond):
	}
	close(release)
	waitFor(t, stopped, "stopServing did not return after the handler finished")
}

func TestHandlerTrackerRefusesRequestsAfterClosing(t *testing.T) {
	var handlers handlerTracker
	reached := false
	h := handlers.track(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { reached = true }))
	handlers.closeAndWait()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if reached || rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("late request: reached %v, status %d; want false, 503", reached, rec.Code)
	}
}

func waitFor(t *testing.T, ch <-chan struct{}, msg string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(5 * time.Second):
		t.Fatal(msg)
	}
}
