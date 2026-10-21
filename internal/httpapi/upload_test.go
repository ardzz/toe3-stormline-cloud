package httpapi

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/ardzz/toe3-stormline-cloud/internal/save"
)

var testSnapshot = []byte{0x1f, 0x8b, 8, 0, 1, 2, 3}

func uploadRequest(body []byte, header map[string]string) *http.Request {
	r := authedRequest(http.MethodPut, "/v1/saves", bytes.NewReader(body))
	for k, v := range header {
		r.Header.Set(k, v)
	}
	return r
}

func TestUploadStoresTheSnapshot(t *testing.T) {
	created := time.Date(2026, 10, 21, 19, 30, 0, 0, time.FixedZone("WIB", 7*3600))
	sha := strings.Repeat("c", 64)
	for _, c := range []struct{ header, value, call string }{
		{"If-Match", `"2"`, "upload 7 head=2"},
		{"If-None-Match", "*", "upload 7 head=0"},
	} {
		saves := &fakeSaves{rev: save.Revision{Rev: 3, CreatedAt: created, Size: 7, SHA256: sha, Device: "Pixel 7",
			Reason: "manual", GameBuild: 193}}
		h := newTestServer(Deps{Accounts: &fakeAccounts{userID: 7}, Saves: saves})
		rec := call(h, uploadRequest(testSnapshot, map[string]string{c.header: c.value,
			"X-Toe3-Sha256": strings.ToUpper(sha), "X-Toe3-Device": "Pixel 7", "X-Toe3-Reason": "manual",
			"X-Toe3-Game-Build": "193"}))
		want := fmt.Sprintf(`{"rev":3,"created_at":"2026-10-21T12:30:00Z","size":7,"sha256":"%s","device":"Pixel 7",`+
			`"reason":"manual","game_build":193}`+"\n", sha)
		if rec.Code != http.StatusCreated || rec.Body.String() != want || rec.Header().Get("ETag") != `"3"` {
			t.Fatalf("%s: upload = %d %q ETag %q; want 201 %q ETag \"3\"", c.header, rec.Code, rec.Body,
				rec.Header().Get("ETag"), want)
		}
		wantMeta := save.Meta{SHA256: strings.ToUpper(sha), Device: "Pixel 7", Reason: "manual", GameBuild: 193}
		if !slices.Equal(saves.calls, []string{c.call}) || !bytes.Equal(saves.uploaded, testSnapshot) || saves.meta != wantMeta {
			t.Fatalf("%s: calls %q, uploaded %x, meta %+v", c.header, saves.calls, saves.uploaded, saves.meta)
		}
	}
}

func TestUploadPreconditions(t *testing.T) {
	cases := []struct {
		name   string
		header http.Header
		status int
		code   string
	}{
		{"none", http.Header{}, http.StatusPreconditionRequired, "precondition_required"},
		{"unquoted tag", http.Header{"If-Match": {"3"}}, http.StatusBadRequest, "bad_request"},
		{"weak tag", http.Header{"If-Match": {`W/"3"`}}, http.StatusBadRequest, "bad_request"},
		{"revision zero", http.Header{"If-Match": {`"0"`}}, http.StatusBadRequest, "bad_request"},
		{"signed revision", http.Header{"If-Match": {`"+3"`}}, http.StatusBadRequest, "bad_request"},
		{"zero-padded revision", http.Header{"If-Match": {`"03"`}}, http.StatusBadRequest, "bad_request"},
		{"revision overflow", http.Header{"If-Match": {`"99999999999999999999"`}}, http.StatusBadRequest, "bad_request"},
		{"two header lines", http.Header{"If-Match": {`"3"`, `"4"`}}, http.StatusBadRequest, "bad_request"},
		{"tag list", http.Header{"If-Match": {`"3", "4"`}}, http.StatusBadRequest, "bad_request"},
		{"If-None-Match with a tag", http.Header{"If-None-Match": {`"3"`}}, http.StatusBadRequest, "bad_request"},
		{"both preconditions", http.Header{"If-Match": {`"3"`}, "If-None-Match": {"*"}}, http.StatusBadRequest, "bad_request"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			saves := &fakeSaves{}
			h := newTestServer(Deps{Accounts: &fakeAccounts{userID: 7}, Saves: saves})
			req := uploadRequest(testSnapshot, nil)
			for k, vs := range c.header {
				for _, v := range vs {
					req.Header.Add(k, v)
				}
			}
			wantError(t, call(h, req), c.status, c.code)
			if len(saves.calls) != 0 {
				t.Fatalf("saves called: %q", saves.calls)
			}
		})
	}
}

func TestUploadSizeLimit(t *testing.T) {
	exact := append(slices.Clone(testSnapshot), 0) // 8 bytes, the limit
	over := append(slices.Clone(exact), 0)
	for _, lengthKnown := range []bool{true, false} {
		saves := &fakeSaves{}
		h := newTestServer(Deps{Config: Config{MaxSnapshotBytes: 8}, Accounts: &fakeAccounts{userID: 7}, Saves: saves})
		req := uploadRequest(over, map[string]string{"If-None-Match": "*"})
		if !lengthKnown {
			req.ContentLength = -1
			req.Body = io.NopCloser(bytes.NewReader(over))
		}
		b := wantError(t, call(h, req), http.StatusRequestEntityTooLarge, "too_large")
		if b.MaxSnapshotBytes == nil || *b.MaxSnapshotBytes != 8 || len(saves.calls) != 0 {
			t.Fatalf("length known %v: max_snapshot_bytes %v, calls %q", lengthKnown, b.MaxSnapshotBytes, saves.calls)
		}
		if rec := call(h, uploadRequest(exact, map[string]string{"If-None-Match": "*"})); rec.Code != http.StatusCreated {
			t.Fatalf("snapshot at the limit = %d %s; want 201", rec.Code, rec.Body)
		}
	}
}

func TestUploadConflictReportsTheHead(t *testing.T) {
	h := newTestServer(Deps{Accounts: &fakeAccounts{userID: 7}, Saves: &fakeSaves{err: &save.StaleError{Latest: 5}}})
	b := wantError(t, call(h, uploadRequest(testSnapshot, map[string]string{"If-Match": `"4"`})), http.StatusConflict, "stale")
	if b.Latest == nil || *b.Latest != 5 {
		t.Fatalf("latest = %v, want 5", b.Latest)
	}
}

func TestUploadGameBuildHeader(t *testing.T) {
	saves := &fakeSaves{}
	h := newTestServer(Deps{Accounts: &fakeAccounts{userID: 7}, Saves: saves})
	if rec := call(h, uploadRequest(testSnapshot, map[string]string{"If-None-Match": "*"})); rec.Code != http.StatusCreated ||
		saves.meta.GameBuild != 0 {
		t.Fatalf("without X-Toe3-Game-Build: %d, game build %d; want 201, 0", rec.Code, saves.meta.GameBuild)
	}
	saves.calls = nil
	wantError(t, call(h, uploadRequest(testSnapshot, map[string]string{"If-None-Match": "*", "X-Toe3-Game-Build": "1.2"})),
		http.StatusBadRequest, "invalid_meta")
	if len(saves.calls) != 0 {
		t.Fatalf("saves called with a malformed game build: %q", saves.calls)
	}
}

func TestUploadRequiresASession(t *testing.T) {
	saves := &fakeSaves{}
	h := newTestServer(Deps{Accounts: &fakeAccounts{}, Saves: saves})
	req := httptest.NewRequest(http.MethodPut, "/v1/saves", bytes.NewReader(testSnapshot))
	req.Header.Set("If-None-Match", "*")
	wantError(t, call(h, req), http.StatusUnauthorized, "unauthorized")
	if len(saves.calls) != 0 {
		t.Fatalf("saves called without a session: %q", saves.calls)
	}
}
