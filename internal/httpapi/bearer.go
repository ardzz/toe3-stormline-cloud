package httpapi

import (
	"net/http"
	"strings"

	"github.com/ardzz/toe3-stormline-cloud/internal/account"
)

// bearerToken returns the token of an "Authorization: Bearer <token>" header; the scheme is case-insensitive.
func bearerToken(r *http.Request) (string, bool) {
	scheme, token, ok := strings.Cut(r.Header.Get("Authorization"), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	token = strings.TrimSpace(token)
	return token, token != ""
}

// authed resolves the request's bearer token to its account before calling h.
func (s *server) authed(h func(w http.ResponseWriter, r *http.Request, userID int64)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token, ok := bearerToken(r)
		if !ok {
			s.fail(w, r, account.ErrUnauthorized)
			return
		}
		userID, err := s.Accounts.Authenticate(r.Context(), token)
		if err != nil {
			s.fail(w, r, err)
			return
		}
		h(w, r, userID)
	}
}
