package httpapi

import (
	"errors"
	"math"
	"net/http"
	"strconv"
	"time"

	"github.com/ardzz/toe3-stormline-cloud/internal/account"
	"github.com/ardzz/toe3-stormline-cloud/internal/save"
)

var (
	errBadRequest           = errors.New("httpapi: malformed request")
	errPreconditionRequired = errors.New("httpapi: If-Match or If-None-Match is required")
)

const codeUnauthorized = "unauthorized"

// apiError is an error response; its body is {"error": Code, "message": Message} plus the details that apply.
type apiError struct {
	Status     int           `json:"-"`
	RetryAfter time.Duration `json:"-"`
	Code       string        `json:"error"`
	Message    string        `json:"message"`
	Latest     *int64        `json:"latest,omitempty"`             // stale: the account's head revision
	MaxBytes   *int64        `json:"max_snapshot_bytes,omitempty"` // too_large: the server's limit
}

// knownErrors maps service errors to their responses; errorFor handles the ones that carry details.
var knownErrors = []struct {
	err  error
	resp apiError
}{
	{errBadRequest, apiError{Status: http.StatusBadRequest, Code: "bad_request", Message: "the request is malformed"}},
	{errPreconditionRequired, apiError{Status: http.StatusPreconditionRequired, Code: "precondition_required",
		Message: `send If-Match with the head revision, or If-None-Match: * for the first upload`}},
	{account.ErrInvalidUsername, apiError{Status: http.StatusBadRequest, Code: "invalid_username",
		Message: "username must match ^[a-z0-9_.-]{3,32}$"}},
	{account.ErrInvalidPassword, apiError{Status: http.StatusBadRequest, Code: "invalid_password",
		Message: "password must be 10-256 characters"}},
	{account.ErrRegistrationClosed, apiError{Status: http.StatusForbidden, Code: "registration_closed",
		Message: "registration is closed on this server"}},
	{account.ErrUsernameTaken, apiError{Status: http.StatusConflict, Code: "username_taken",
		Message: "that username is taken"}},
	{account.ErrInvalidCredentials, apiError{Status: http.StatusUnauthorized, Code: "invalid_credentials",
		Message: "invalid username or password"}},
	{account.ErrUnauthorized, apiError{Status: http.StatusUnauthorized, Code: codeUnauthorized,
		Message: "missing, invalid or expired session token"}},
	{save.ErrNoSaves, apiError{Status: http.StatusNotFound, Code: "no_saves", Message: "the account has no saves yet"}},
	{save.ErrNotFound, apiError{Status: http.StatusNotFound, Code: "not_found", Message: "no such revision"}},
	{save.ErrNotGzip, apiError{Status: http.StatusBadRequest, Code: "not_gzip", Message: "the snapshot is not gzip data"}},
	{save.ErrEmpty, apiError{Status: http.StatusBadRequest, Code: "not_gzip", Message: "the snapshot is empty"}},
	{save.ErrSHAMismatch, apiError{Status: http.StatusBadRequest, Code: "sha_mismatch",
		Message: "X-Toe3-Sha256 does not match the snapshot"}},
	{save.ErrInvalidMeta, apiError{Status: http.StatusBadRequest, Code: "invalid_meta",
		Message: "invalid X-Toe3-* upload metadata"}},
	{save.ErrBlobMissing, apiError{Status: http.StatusInternalServerError, Code: "blob_missing",
		Message: "the stored snapshot is missing"}},
}

// errorFor maps err to its response; unknown errors are internal.
func (s *server) errorFor(err error) apiError {
	var stale *save.StaleError
	var throttled *account.ThrottledError
	var tooBig *http.MaxBytesError
	switch {
	case errors.As(err, &stale):
		latest := stale.Latest
		return apiError{Status: http.StatusConflict, Code: "stale", Message: "the account has a newer revision",
			Latest: &latest}
	case errors.As(err, &throttled):
		return rateLimited(throttled.RetryAfter)
	case errors.As(err, &tooBig), errors.Is(err, save.ErrTooLarge):
		limit := s.Config.MaxSnapshotBytes
		return apiError{Status: http.StatusRequestEntityTooLarge, Code: "too_large", Message: "the snapshot is too large",
			MaxBytes: &limit}
	}
	for _, k := range knownErrors {
		if errors.Is(err, k.err) {
			return k.resp
		}
	}
	return apiError{Status: http.StatusInternalServerError, Code: "internal", Message: "internal server error"}
}

func rateLimited(wait time.Duration) apiError {
	return apiError{Status: http.StatusTooManyRequests, Code: "rate_limited", Message: "too many requests; retry later",
		RetryAfter: wait}
}

// fail answers r with the response for err, logging server-side failures unless the client has gone away.
func (s *server) fail(w http.ResponseWriter, r *http.Request, err error) {
	e := s.errorFor(err)
	if e.Status >= http.StatusInternalServerError && r.Context().Err() == nil {
		s.Log.Error("request failed", "method", r.Method, "path", r.URL.Path, "error", err)
	}
	writeError(w, e)
}

// writeError sends e with its headers: Retry-After in whole seconds rounded up, and a Bearer challenge when the
// session token is the problem.
func writeError(w http.ResponseWriter, e apiError) {
	if e.RetryAfter > 0 {
		w.Header().Set("Retry-After", strconv.FormatInt(int64(math.Ceil(e.RetryAfter.Seconds())), 10))
	}
	if e.Code == codeUnauthorized {
		w.Header().Set("WWW-Authenticate", "Bearer")
	}
	writeJSON(w, e.Status, e)
}
