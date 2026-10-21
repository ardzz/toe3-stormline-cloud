package httpapi

import (
	"bytes"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ardzz/toe3-stormline-cloud/internal/account"
	"github.com/ardzz/toe3-stormline-cloud/internal/save"
)

func authedRequest(method, target string, body io.Reader) *http.Request {
	r := httptest.NewRequest(method, target, body)
	r.Header.Set("Authorization", "Bearer tok")
	return r
}

func TestSaveRoutesRequireASession(t *testing.T) {
	for _, authErr := range []error{nil, account.ErrUnauthorized} {
		saves := &fakeSaves{}
		h := newTestServer(Deps{Accounts: &fakeAccounts{userID: 7, authErr: authErr}, Saves: saves})
		for _, target := range []string{"/v1/saves", "/v1/saves/latest", "/v1/saves/3"} {
			req := httptest.NewRequest(http.MethodGet, target, nil)
			if authErr != nil {
				req.Header.Set("Authorization", "Bearer expired")
			}
			rec := call(h, req)
			wantError(t, rec, http.StatusUnauthorized, "unauthorized")
			if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
				t.Fatalf("%s: WWW-Authenticate = %q, want Bearer", target, got)
			}
		}
		if len(saves.calls) != 0 {
			t.Fatalf("saves called without a session: %q", saves.calls)
		}
	}
}

func TestListRevisions(t *testing.T) {
	created := time.Date(2026, 10, 21, 19, 30, 0, 0, time.FixedZone("WIB", 7*3600))
	shaA, shaB := strings.Repeat("a", 64), strings.Repeat("b", 64)
	accounts := &fakeAccounts{userID: 7}
	saves := &fakeSaves{revs: []save.Revision{
		{Rev: 3, CreatedAt: created, Size: 5120, SHA256: shaA, Device: "Pixel 7", Reason: "auto", GameBuild: 193,
			BlobKey: "u/7/3.json.gz"},
		{Rev: 2, CreatedAt: created.Add(-time.Hour), Size: 5000, SHA256: shaB, Device: "Pixel 7", Reason: "manual",
			GameBuild: 193, BlobKey: "u/7/2.json.gz"},
	}}
	rec := call(newTestServer(Deps{Accounts: accounts, Saves: saves}), authedRequest(http.MethodGet, "/v1/saves", nil))
	want := fmt.Sprintf(`{"revisions":[`+
		`{"rev":3,"created_at":"2026-10-21T12:30:00Z","size":5120,"sha256":"%s","device":"Pixel 7","reason":"auto","game_build":193},`+
		`{"rev":2,"created_at":"2026-10-21T11:30:00Z","size":5000,"sha256":"%s","device":"Pixel 7","reason":"manual","game_build":193}`+
		`]}`+"\n", shaA, shaB)
	if rec.Code != http.StatusOK || rec.Body.String() != want {
		t.Fatalf("list = %d %s; want 200 %s", rec.Code, rec.Body, want)
	}
	if !slices.Equal(accounts.calls, []string{"auth tok"}) || !slices.Equal(saves.calls, []string{"list 7"}) {
		t.Fatalf("account calls %q, save calls %q", accounts.calls, saves.calls)
	}

	rec = call(newTestServer(Deps{Accounts: accounts, Saves: &fakeSaves{}}), authedRequest(http.MethodGet, "/v1/saves", nil))
	if rec.Body.String() != `{"revisions":[]}`+"\n" {
		t.Fatalf("empty list = %q, want an empty array", rec.Body)
	}
}

func TestDownloadsAreTheStoredBytes(t *testing.T) {
	snapshot := []byte{0x1f, 0x8b, 8, 0, 1, 2, 3}
	sha := strings.Repeat("c", 64)
	for _, c := range []struct{ target, call string }{
		{"/v1/saves/latest", "latest 7"},
		{"/v1/saves/3", "get 7 3"},
	} {
		saves := &fakeSaves{rev: save.Revision{Rev: 3, SHA256: sha}, data: snapshot}
		rec := call(newTestServer(Deps{Accounts: &fakeAccounts{userID: 7}, Saves: saves}),
			authedRequest(http.MethodGet, c.target, nil))
		if rec.Code != http.StatusOK || !bytes.Equal(rec.Body.Bytes(), snapshot) {
			t.Fatalf("%s = %d %x; want 200 %x", c.target, rec.Code, rec.Body.Bytes(), snapshot)
		}
		for k, v := range map[string]string{"Content-Type": "application/gzip", "Content-Length": "7", "ETag": `"3"`,
			"X-Toe3-Sha256": sha, "Content-Encoding": ""} {
			if got := rec.Header().Get(k); got != v {
				t.Errorf("%s: %s = %q, want %q", c.target, k, got, v)
			}
		}
		if !slices.Equal(saves.calls, []string{c.call}) {
			t.Fatalf("%s: calls %q, want [%s]", c.target, saves.calls, c.call)
		}
	}
}

func TestDownloadErrors(t *testing.T) {
	cases := []struct {
		target string
		err    error
		status int
		code   string
	}{
		{"/v1/saves/latest", save.ErrNoSaves, http.StatusNotFound, "no_saves"},
		{"/v1/saves/9", save.ErrNotFound, http.StatusNotFound, "not_found"},
		{"/v1/saves/latest", save.ErrBlobMissing, http.StatusInternalServerError, "blob_missing"},
	}
	for _, c := range cases {
		var logs bytes.Buffer
		h := newTestServer(Deps{Accounts: &fakeAccounts{userID: 7}, Saves: &fakeSaves{err: c.err},
			Log: slog.New(slog.NewTextHandler(&logs, nil))})
		wantError(t, call(h, authedRequest(http.MethodGet, c.target, nil)), c.status, c.code)
		if logged := strings.Contains(logs.String(), "request failed"); logged != (c.status >= 500) {
			t.Fatalf("%s: logged failure = %v, want %v; logs:\n%s", c.code, logged, c.status >= 500, logs.String())
		}
	}
}

func TestDownloadOfAnInvalidRevisionIsNotFound(t *testing.T) {
	saves := &fakeSaves{}
	h := newTestServer(Deps{Accounts: &fakeAccounts{userID: 7}, Saves: saves})
	for _, target := range []string{"/v1/saves/0", "/v1/saves/-1", "/v1/saves/abc", "/v1/saves/99999999999999999999"} {
		wantError(t, call(h, authedRequest(http.MethodGet, target, nil)), http.StatusNotFound, "not_found")
	}
	if len(saves.calls) != 0 {
		t.Fatalf("saves called: %q", saves.calls)
	}
}
