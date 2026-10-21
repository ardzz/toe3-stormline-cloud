package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/ardzz/toe3-stormline-cloud/internal/account"
	"github.com/ardzz/toe3-stormline-cloud/internal/save"
)

// newTestServer builds the API with a discarding logger and test defaults for the settings d leaves empty.
func newTestServer(d Deps) http.Handler {
	if d.Log == nil {
		d.Log = slog.New(slog.DiscardHandler)
	}
	if d.Config.Registration == "" {
		d.Config.Registration = "closed"
	}
	if d.Config.MaxSnapshotBytes == 0 {
		d.Config.MaxSnapshotBytes = 1 << 20
	}
	return New(d)
}

func call(h http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

// errorBody is the JSON error envelope.
type errorBody struct {
	Error            string `json:"error"`
	Message          string `json:"message"`
	Latest           *int64 `json:"latest"`
	MaxSnapshotBytes *int64 `json:"max_snapshot_bytes"`
}

// wantError fails unless rec is a JSON error with status and code, and returns its body.
func wantError(t *testing.T, rec *httptest.ResponseRecorder, status int, code string) errorBody {
	t.Helper()
	var body errorBody
	if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil {
		t.Fatalf("status %d, body %q is not a JSON error: %v", rec.Code, rec.Body, err)
	}
	if rec.Code != status || body.Error != code || body.Message == "" {
		t.Fatalf("got %d %+v; want %d %q with a message", rec.Code, body, status, code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q, want application/json", ct)
	}
	return body
}

func TestHealthDescribesTheServer(t *testing.T) {
	h := newTestServer(Deps{Config: Config{Registration: "open", MaxSnapshotBytes: 4096}})
	rec := call(h, httptest.NewRequest(http.MethodGet, "/v1/health", nil))
	want := `{"service":"toe3-cloud","api":1,"registration":"open","max_snapshot_bytes":4096}` + "\n"
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Fatalf("health = %d %q; want 200 %q", rec.Code, rec.Body, want)
	}
	for k, v := range map[string]string{"Content-Type": "application/json", "Cache-Control": "no-store",
		"X-Content-Type-Options": "nosniff"} {
		if got := rec.Header().Get(k); got != v {
			t.Errorf("%s = %q, want %q", k, got, v)
		}
	}
}

func TestUnknownRoutesAndMethodsAreJSONErrors(t *testing.T) {
	h := newTestServer(Deps{})
	wantError(t, call(h, httptest.NewRequest(http.MethodGet, "/v1/nope", nil)), http.StatusNotFound, "not_found")
	rec := call(h, httptest.NewRequest(http.MethodDelete, "/v1/health", nil))
	wantError(t, rec, http.StatusMethodNotAllowed, "method_not_allowed")
	if got := rec.Header().Get("Allow"); got != "GET" {
		t.Fatalf("Allow = %q, want GET", got)
	}
}

func TestErrorResponses(t *testing.T) {
	s := &server{Deps: Deps{Config: Config{MaxSnapshotBytes: 4096}}}
	cases := []struct {
		err    error
		status int
		code   string
	}{
		{errBadRequest, http.StatusBadRequest, "bad_request"},
		{errPreconditionRequired, http.StatusPreconditionRequired, "precondition_required"},
		{account.ErrInvalidUsername, http.StatusBadRequest, "invalid_username"},
		{account.ErrInvalidPassword, http.StatusBadRequest, "invalid_password"},
		{account.ErrRegistrationClosed, http.StatusForbidden, "registration_closed"},
		{account.ErrUsernameTaken, http.StatusConflict, "username_taken"},
		{account.ErrInvalidCredentials, http.StatusUnauthorized, "invalid_credentials"},
		{account.ErrUnauthorized, http.StatusUnauthorized, "unauthorized"},
		{save.ErrNoSaves, http.StatusNotFound, "no_saves"},
		{save.ErrNotFound, http.StatusNotFound, "not_found"},
		{save.ErrNotGzip, http.StatusBadRequest, "not_gzip"},
		{save.ErrEmpty, http.StatusBadRequest, "not_gzip"},
		{save.ErrSHAMismatch, http.StatusBadRequest, "sha_mismatch"},
		{save.ErrInvalidMeta, http.StatusBadRequest, "invalid_meta"},
		{save.ErrBlobMissing, http.StatusInternalServerError, "blob_missing"},
		{save.ErrReservationLost, http.StatusInternalServerError, "internal"},
		{fmt.Errorf("list: %w", save.ErrNoSaves), http.StatusNotFound, "no_saves"},
		{errors.New("connection reset"), http.StatusInternalServerError, "internal"},
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		writeError(rec, s.errorFor(c.err))
		wantError(t, rec, c.status, c.code)
	}
}

func TestErrorDetails(t *testing.T) {
	s := &server{Deps: Deps{Config: Config{MaxSnapshotBytes: 4096}}}

	rec := httptest.NewRecorder()
	writeError(rec, s.errorFor(&save.StaleError{Latest: 0}))
	if b := wantError(t, rec, http.StatusConflict, "stale"); b.Latest == nil || *b.Latest != 0 {
		t.Fatalf("stale latest = %v, want 0", b.Latest)
	}

	for _, err := range []error{save.ErrTooLarge, &http.MaxBytesError{Limit: 4096}} {
		rec = httptest.NewRecorder()
		writeError(rec, s.errorFor(err))
		b := wantError(t, rec, http.StatusRequestEntityTooLarge, "too_large")
		if b.MaxSnapshotBytes == nil || *b.MaxSnapshotBytes != 4096 {
			t.Fatalf("%v: max_snapshot_bytes = %v, want 4096", err, b.MaxSnapshotBytes)
		}
	}

	rec = httptest.NewRecorder()
	writeError(rec, s.errorFor(&account.ThrottledError{RetryAfter: 1500 * time.Millisecond}))
	wantError(t, rec, http.StatusTooManyRequests, "rate_limited")
	if got := rec.Header().Get("Retry-After"); got != "2" {
		t.Fatalf("Retry-After = %q, want 2 (rounded up)", got)
	}

	rec = httptest.NewRecorder()
	writeError(rec, s.errorFor(account.ErrUnauthorized))
	if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
		t.Fatalf("unauthorized WWW-Authenticate = %q, want Bearer", got)
	}
	rec = httptest.NewRecorder()
	writeError(rec, s.errorFor(account.ErrInvalidCredentials))
	if got := rec.Header().Get("WWW-Authenticate"); got != "" {
		t.Fatalf("invalid_credentials WWW-Authenticate = %q, want none", got)
	}
}
