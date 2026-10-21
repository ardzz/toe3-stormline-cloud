package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ardzz/toe3-stormline-cloud/internal/account"
)

func postJSON(path, body string) *http.Request {
	return httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
}

func TestRegisterAndLoginIssueSessions(t *testing.T) {
	expiry := time.Date(2027, 1, 19, 19, 0, 0, 0, time.FixedZone("WIB", 7*3600))
	for _, c := range []struct {
		path   string
		status int
		call   string
	}{
		{"/v1/auth/register", http.StatusCreated, "register Alice correct horse Pixel 7"},
		{"/v1/auth/login", http.StatusOK, "login Alice correct horse Pixel 7"},
	} {
		accounts := &fakeAccounts{issued: account.Issued{UserID: 7, Token: "tok", ExpiresAt: expiry}}
		limiter := &fakeLimiter{}
		h := newTestServer(Deps{Accounts: accounts, AuthLimit: limiter})
		rec := call(h, postJSON(c.path, `{"username":"Alice","password":"correct horse","device":"Pixel 7"}`))
		want := `{"token":"tok","expires_at":"2027-01-19T12:00:00Z"}` + "\n"
		if rec.Code != c.status || rec.Body.String() != want {
			t.Fatalf("%s = %d %q; want %d %q", c.path, rec.Code, rec.Body, c.status, want)
		}
		if !slices.Equal(accounts.calls, []string{c.call}) || !slices.Equal(limiter.keys, []string{"192.0.2.1"}) {
			t.Fatalf("%s: calls %q, limiter keys %q", c.path, accounts.calls, limiter.keys)
		}
	}
}

func TestAuthServiceErrors(t *testing.T) {
	h := newTestServer(Deps{Accounts: &fakeAccounts{err: account.ErrRegistrationClosed}, AuthLimit: &fakeLimiter{}})
	wantError(t, call(h, postJSON("/v1/auth/register", `{"username":"alice","password":"correct horse"}`)),
		http.StatusForbidden, "registration_closed")

	h = newTestServer(Deps{Accounts: &fakeAccounts{err: &account.ThrottledError{RetryAfter: 90 * time.Second}},
		AuthLimit: &fakeLimiter{}})
	rec := call(h, postJSON("/v1/auth/login", `{"username":"alice","password":"wrong password"}`))
	wantError(t, rec, http.StatusTooManyRequests, "rate_limited")
	if got := rec.Header().Get("Retry-After"); got != "90" {
		t.Fatalf("Retry-After = %q, want 90", got)
	}
}

func TestAuthRejectsMalformedJSON(t *testing.T) {
	const valid = `{"username":"alice","password":"correct horse"}`
	accounts := &fakeAccounts{}
	h := newTestServer(Deps{Accounts: accounts, AuthLimit: &fakeLimiter{}})
	for _, body := range []string{"", "{", `{"username": 7}`, strings.Repeat(" ", maxJSONBody) + "{}",
		valid + " trailing", valid + "{}", valid + strings.Repeat(" ", maxJSONBody)} {
		wantError(t, call(h, postJSON("/v1/auth/login", body)), http.StatusBadRequest, "bad_request")
	}
	if len(accounts.calls) != 0 {
		t.Fatalf("accounts called: %q", accounts.calls)
	}
	if rec := call(h, postJSON("/v1/auth/login", valid+"\n")); rec.Code != http.StatusOK {
		t.Fatalf("body with a trailing newline = %d %s; want 200", rec.Code, rec.Body)
	}
}

func TestAuthEndpointsAreRateLimitedPerClient(t *testing.T) {
	accounts := &fakeAccounts{}
	limiter := &fakeLimiter{wait: 30 * time.Second}
	h := newTestServer(Deps{Accounts: accounts, AuthLimit: limiter,
		Config: Config{TrustedProxies: []netip.Prefix{netip.MustParsePrefix("192.0.2.0/24")}}})
	for _, path := range []string{"/v1/auth/register", "/v1/auth/login", "/v1/auth/logout"} {
		req := postJSON(path, `{}`)
		req.Header.Set("X-Forwarded-For", "203.0.113.9")
		req.Header.Set("Authorization", "Bearer tok")
		rec := call(h, req)
		wantError(t, rec, http.StatusTooManyRequests, "rate_limited")
		if got := rec.Header().Get("Retry-After"); got != "30" {
			t.Fatalf("%s: Retry-After = %q, want 30", path, got)
		}
	}
	if len(accounts.calls) != 0 || !slices.Equal(limiter.keys, []string{"203.0.113.9", "203.0.113.9", "203.0.113.9"}) {
		t.Fatalf("calls %q, limiter keys %q", accounts.calls, limiter.keys)
	}
}

func TestLogout(t *testing.T) {
	accounts := &fakeAccounts{}
	h := newTestServer(Deps{Accounts: accounts, AuthLimit: &fakeLimiter{}})
	for _, header := range []string{"", "Basic dG9rOg==", "Bearer "} {
		req := postJSON("/v1/auth/logout", "")
		if header != "" {
			req.Header.Set("Authorization", header)
		}
		rec := call(h, req)
		wantError(t, rec, http.StatusUnauthorized, "unauthorized")
		if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
			t.Fatalf("Authorization %q: WWW-Authenticate = %q, want Bearer", header, got)
		}
	}
	req := postJSON("/v1/auth/logout", "")
	req.Header.Set("Authorization", "bearer tok")
	if rec := call(h, req); rec.Code != http.StatusNoContent || rec.Body.Len() != 0 {
		t.Fatalf("logout = %d %q; want 204 without a body", rec.Code, rec.Body)
	}
	if !slices.Equal(accounts.calls, []string{"logout tok"}) {
		t.Fatalf("calls %q, want [logout tok]", accounts.calls)
	}
}
