package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
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
	entered, finished := make(chan struct{}), make(chan struct{})
	srv := &http.Server{
		BaseContext: func(net.Listener) context.Context { return reqCtx },
		Handler: handlers.track(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			close(entered)
			<-r.Context().Done()              // a request that never finishes on its own
			time.Sleep(50 * time.Millisecond) // and still cleans up after cancellation
			close(finished)
		})),
	}
	go func() { _ = srv.Serve(ln) }()
	go func() {
		if res, err := http.Get("http://" + ln.Addr().String()); err == nil {
			res.Body.Close()
		}
	}()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("request never reached the handler")
	}
	stopServing(srv, 10*time.Millisecond, cancelRequests, &handlers, slog.New(slog.DiscardHandler))
	select {
	case <-finished:
	default:
		t.Fatal("stopServing returned while a handler was still running")
	}
}
