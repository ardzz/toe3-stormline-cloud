package httpapi

import (
	"context"
	"net/http"
	"time"

	"github.com/ardzz/toe3-stormline-cloud/internal/account"
)

type credentialsJSON struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Device   string `json:"device"`
}

type sessionJSON struct {
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// register creates an account and answers with its first session.
func (s *server) register(w http.ResponseWriter, r *http.Request) {
	s.issue(w, r, http.StatusCreated, s.Accounts.Register)
}

// login answers valid credentials with a new session.
func (s *server) login(w http.ResponseWriter, r *http.Request) {
	s.issue(w, r, http.StatusOK, s.Accounts.Login)
}

// issue reads credentials, opens a session with them and answers with status and the session.
func (s *server) issue(w http.ResponseWriter, r *http.Request, status int,
	open func(ctx context.Context, username, password, device string) (account.Issued, error)) {
	var c credentialsJSON
	if err := readJSON(w, r, &c); err != nil {
		s.fail(w, r, err)
		return
	}
	iss, err := open(r.Context(), c.Username, c.Password, c.Device)
	if err != nil {
		s.fail(w, r, err)
		return
	}
	writeJSON(w, status, sessionJSON{Token: iss.Token, ExpiresAt: iss.ExpiresAt.UTC()})
}

// logout revokes the session whose token authorizes the request; an unknown token is not an error.
func (s *server) logout(w http.ResponseWriter, r *http.Request) {
	token, ok := bearerToken(r)
	if !ok {
		s.fail(w, r, account.ErrUnauthorized)
		return
	}
	if err := s.Accounts.Logout(r.Context(), token); err != nil {
		s.fail(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// limited rations h per client with s.AuthLimit.
func (s *server) limited(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if wait := s.AuthLimit.Allow(clientKey(r, s.Config.TrustedProxies)); wait > 0 {
			writeError(w, rateLimited(wait))
			return
		}
		h(w, r)
	}
}
