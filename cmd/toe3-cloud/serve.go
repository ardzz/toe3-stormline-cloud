package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/ardzz/toe3-stormline-cloud/internal/account"
	"github.com/ardzz/toe3-stormline-cloud/internal/auth"
	"github.com/ardzz/toe3-stormline-cloud/internal/config"
	"github.com/ardzz/toe3-stormline-cloud/internal/httpapi"
	"github.com/ardzz/toe3-stormline-cloud/internal/postgres"
	"github.com/ardzz/toe3-stormline-cloud/internal/save"
)

const (
	maintenanceEvery      = 10 * time.Minute
	shutdownGrace         = 30 * time.Second
	authRequestsPerMinute = 20 // per client on /v1/auth/*
	hashConcurrency       = 2  // Argon2id computations at once
	loginFreeFailures     = 5  // failed logins before a username is locked out
	loginFirstLockout     = time.Second
	loginMaxLockout       = 15 * time.Minute
)

// runServe serves the API on TOE3_LISTEN until ctx is done, logging to stderr.
func runServe(ctx context.Context, getenv func(string) string, stderr io.Writer) error {
	cfg, err := config.Load(getenv)
	if err != nil {
		return err
	}
	ln, err := net.Listen("tcp", cfg.Listen)
	if err != nil {
		return err
	}
	return serve(ctx, cfg, ln, slog.New(slog.NewTextHandler(stderr, nil)))
}

// serve migrates the database, then serves the API on ln with periodic cleanup until ctx is done, and finally lets
// in-flight requests finish for up to shutdownGrace. It closes ln.
func serve(ctx context.Context, cfg config.Config, ln net.Listener, log *slog.Logger) (err error) {
	defer ln.Close()
	if err := postgres.Migrate(ctx, cfg.DatabaseURL); err != nil {
		return err
	}
	pool, err := postgres.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer pool.Close()
	blobs, releaseBlobs, err := openBlobs(cfg)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, releaseBlobs()) }()

	accounts := account.NewService(postgres.NewUsers(pool), postgres.NewSessions(pool), auth.NewHasher(hashConcurrency),
		auth.NewBackoff(time.Now, loginFreeFailures, loginFirstLockout, loginMaxLockout),
		account.Options{RegistrationOpen: cfg.Registration == config.RegistrationOpen, SessionTTL: cfg.SessionTTL})
	saves := postgres.NewSaves(pool)
	srv := &http.Server{
		Handler: httpapi.New(httpapi.Deps{
			Config: httpapi.Config{Registration: string(cfg.Registration), MaxSnapshotBytes: cfg.MaxSnapshotBytes,
				TrustedProxies: cfg.TrustedProxies},
			Accounts:  accounts,
			Saves:     save.NewService(saves, blobs, save.Options{MaxBytes: cfg.MaxSnapshotBytes, Retention: cfg.Retention}),
			AuthLimit: auth.NewRateLimiter(time.Now, authRequestsPerMinute, time.Minute),
			Log:       log,
		}),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       time.Minute,
		WriteTimeout:      time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    16 << 10,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelWarn),
	}

	loopCtx, stopLoop := context.WithCancel(ctx)
	var loop sync.WaitGroup
	loop.Go(func() {
		every(loopCtx, maintenanceEvery, maintenance(log, save.NewSweeper(saves, blobs, nil), accounts))
	})
	served := make(chan error, 1)
	go func() { served <- srv.Serve(ln) }()
	log.Info("listening", "addr", ln.Addr().String(), "version", version, "blob_backend", string(cfg.BlobBackend))

	select {
	case err = <-served:
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownGrace)
		err = srv.Shutdown(shutdownCtx)
		cancel()
		<-served
		log.Info("stopped")
	}
	stopLoop()
	loop.Wait()
	if errors.Is(err, http.ErrServerClosed) {
		err = nil
	}
	return err
}
