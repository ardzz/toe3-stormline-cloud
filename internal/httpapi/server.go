// Package httpapi serves the v1 HTTP API used by the mod: server health, accounts and save snapshots.
package httpapi

import (
	"context"
	"log/slog"
	"net/http"
	"net/netip"
	"time"

	"github.com/ardzz/toe3-stormline-cloud/internal/account"
	"github.com/ardzz/toe3-stormline-cloud/internal/save"
)

// Accounts is the account service the API needs (implemented by account.Service).
type Accounts interface {
	Register(ctx context.Context, username, password, device string) (account.Issued, error)
	Login(ctx context.Context, username, password, device string) (account.Issued, error)
	Logout(ctx context.Context, token string) error
	Authenticate(ctx context.Context, token string) (int64, error)
}

// Saves is the save service the API needs (implemented by save.Service).
type Saves interface {
	Upload(ctx context.Context, userID, expectedHead int64, data []byte, m save.Meta) (save.Revision, error)
	List(ctx context.Context, userID int64) ([]save.Revision, error)
	Latest(ctx context.Context, userID int64) (save.Revision, []byte, error)
	Get(ctx context.Context, userID, rev int64) (save.Revision, []byte, error)
}

// Limiter rations requests per key (implemented by auth.RateLimiter): it returns 0 to allow, otherwise the wait.
type Limiter interface {
	Allow(key string) time.Duration
}

// Config holds the settings the API reports or enforces.
type Config struct {
	Registration     string         // "open" or "closed", reported by /v1/health
	MaxSnapshotBytes int64          // largest accepted upload, reported by /v1/health
	TrustedProxies   []netip.Prefix // peers whose X-Forwarded-For header is believed
}

// Deps is everything the API is built from.
type Deps struct {
	Config    Config
	Accounts  Accounts
	Saves     Saves
	AuthLimit Limiter // per-client limit on /v1/auth/*
	Log       *slog.Logger
}

type server struct{ Deps }

// New returns the API handler. Every unsuccessful response, including unknown routes and methods, has a JSON error
// body.
func New(d Deps) http.Handler {
	s := &server{Deps: d}
	mux := http.NewServeMux()
	for path, h := range s.routes() {
		mux.Handle(path, h)
	}
	mux.HandleFunc("/", notFound)
	return s.observe(mux)
}

// routes maps each path to its handlers by method.
func (s *server) routes() map[string]methods {
	return map[string]methods{
		"/v1/health":        {http.MethodGet: s.health},
		"/v1/auth/register": {http.MethodPost: s.limited(s.register)},
		"/v1/auth/login":    {http.MethodPost: s.limited(s.login)},
		"/v1/auth/logout":   {http.MethodPost: s.limited(s.logout)},
		"/v1/saves":         {http.MethodGet: s.authed(s.list)},
		"/v1/saves/latest":  {http.MethodGet: s.authed(s.latest)},
		"/v1/saves/{rev}":   {http.MethodGet: s.authed(s.get)},
	}
}
