package httpapi

import (
	"net/http"
	"strings"
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
