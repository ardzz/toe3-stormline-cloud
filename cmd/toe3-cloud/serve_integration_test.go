//go:build integration

package main

import (
	"bytes"
	"compress/gzip"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"log/slog"
	"net"
	"net/http"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/s3mock"

	"github.com/ardzz/toe3-stormline-cloud/internal/config"
	"github.com/ardzz/toe3-stormline-cloud/internal/postgres/pgtest"
)

func TestServeEndToEnd(t *testing.T) {
	t.Run("fs", func(t *testing.T) {
		exerciseServer(t, map[string]string{"TOE3_BLOB_DIR": t.TempDir()})
	})
	t.Run("s3", func(t *testing.T) {
		ctx := context.Background()
		c, err := s3mock.Run(ctx, "adobe/s3mock:5.2.3", s3mock.WithInitialBuckets("saves"))
		if err != nil {
			t.Fatalf("start s3mock: %v", err)
		}
		t.Cleanup(func() {
			if err := testcontainers.TerminateContainer(c); err != nil {
				t.Errorf("stop s3mock: %v", err)
			}
		})
		endpoint, err := c.EndpointURL(ctx)
		if err != nil {
			t.Fatal(err)
		}
		exerciseServer(t, map[string]string{"TOE3_BLOB_BACKEND": "s3", "TOE3_S3_ENDPOINT": endpoint,
			"TOE3_S3_BUCKET": "saves", "TOE3_S3_REGION": "us-east-1", "TOE3_S3_PATH_STYLE": "true",
			"AWS_ACCESS_KEY_ID": "test", "AWS_SECRET_ACCESS_KEY": "test"})
	})
}

// exerciseServer runs serve with env plus a fresh database, open registration and a retention of 2. It drives one
// account through registration, uploads, a conflict, pruning, downloads, logout and login, then stops the server.
func exerciseServer(t *testing.T, env map[string]string) {
	env["TOE3_DATABASE_URL"] = pgtest.URL(t)
	env["TOE3_REGISTRATION"] = "open"
	env["TOE3_RETENTION"] = "2"
	cfg, err := config.Load(func(k string) string { return env[k] })
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- serve(ctx, cfg, ln, slog.New(slog.DiscardHandler)) }()
	c := &apiClient{t: t, base: "http://" + ln.Addr().String()}

	if res := c.do(http.MethodGet, "/v1/health", nil, nil); res.status != http.StatusOK ||
		!strings.Contains(res.body, `"registration":"open"`) {
		t.Fatalf("health = %d %s", res.status, res.body)
	}
	const credentials = `{"username":"Alice","password":"correct horse battery","device":"Pixel 7"}`
	c.token = c.session(c.do(http.MethodPost, "/v1/auth/register", strings.NewReader(credentials), nil), http.StatusCreated)

	one, two, three := gzipText(t, "one"), gzipText(t, "two"), gzipText(t, "three")
	upload := func(precondition map[string]string, data []byte) response {
		sum := sha256.Sum256(data)
		header := map[string]string{"X-Toe3-Sha256": hex.EncodeToString(sum[:]), "X-Toe3-Reason": "manual",
			"X-Toe3-Device": "Pixel 7", "X-Toe3-Game-Build": "193"}
		for k, v := range precondition {
			header[k] = v
		}
		return c.do(http.MethodPut, "/v1/saves", bytes.NewReader(data), header)
	}
	steps := []struct {
		precondition map[string]string
		data         []byte
		status       int
		etag         string
	}{
		{nil, one, http.StatusPreconditionRequired, ""},
		{map[string]string{"If-None-Match": "*"}, one, http.StatusCreated, `"1"`},
		{map[string]string{"If-None-Match": "*"}, two, http.StatusConflict, ""},
		{map[string]string{"If-Match": `"1"`}, two, http.StatusCreated, `"2"`},
		{map[string]string{"If-Match": `"2"`}, three, http.StatusCreated, `"3"`},
	}
	for i, s := range steps {
		res := upload(s.precondition, s.data)
		if res.status != s.status || res.header.Get("ETag") != s.etag {
			t.Fatalf("upload %d = %d ETag %q %s; want %d ETag %q", i, res.status, res.header.Get("ETag"), res.body,
				s.status, s.etag)
		}
	}

	var list struct {
		Revisions []struct {
			Rev int64 `json:"rev"`
		} `json:"revisions"`
	}
	res := c.do(http.MethodGet, "/v1/saves", nil, nil)
	if err := json.Unmarshal([]byte(res.body), &list); err != nil || res.status != http.StatusOK {
		t.Fatalf("list = %d %s: %v", res.status, res.body, err)
	}
	var revs []int64
	for _, r := range list.Revisions {
		revs = append(revs, r.Rev)
	}
	if !slices.Equal(revs, []int64{3, 2}) {
		t.Fatalf("revisions = %v, want [3 2] (retention 2)", revs)
	}
	sum := sha256.Sum256(three)
	res = c.do(http.MethodGet, "/v1/saves/latest", nil, nil)
	if res.status != http.StatusOK || res.body != string(three) || res.header.Get("ETag") != `"3"` ||
		res.header.Get("X-Toe3-Sha256") != hex.EncodeToString(sum[:]) || res.header.Get("Content-Type") != "application/gzip" {
		t.Fatalf("latest = %d %q %v", res.status, res.body, res.header)
	}
	if res = c.do(http.MethodGet, "/v1/saves/2", nil, nil); res.status != http.StatusOK || res.body != string(two) {
		t.Fatalf("revision 2 = %d %q", res.status, res.body)
	}
	if res = c.do(http.MethodGet, "/v1/saves/1", nil, nil); res.status != http.StatusNotFound ||
		!strings.Contains(res.body, `"not_found"`) {
		t.Fatalf("pruned revision 1 = %d %s; want 404 not_found", res.status, res.body)
	}

	if res = c.do(http.MethodPost, "/v1/auth/logout", nil, nil); res.status != http.StatusNoContent {
		t.Fatalf("logout = %d %s", res.status, res.body)
	}
	if res = c.do(http.MethodGet, "/v1/saves", nil, nil); res.status != http.StatusUnauthorized {
		t.Fatalf("list after logout = %d %s; want 401", res.status, res.body)
	}
	c.token = c.session(c.do(http.MethodPost, "/v1/auth/login", strings.NewReader(credentials), nil), http.StatusOK)
	if res = c.do(http.MethodGet, "/v1/saves", nil, nil); res.status != http.StatusOK {
		t.Fatalf("list after login = %d %s", res.status, res.body)
	}

	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("serve = %v, want nil after shutdown", err)
		}
	case <-time.After(shutdownGrace + 10*time.Second):
		t.Fatal("serve did not return after cancellation")
	}
}

func gzipText(t *testing.T, s string) []byte {
	t.Helper()
	var b bytes.Buffer
	w := gzip.NewWriter(&b)
	if _, err := w.Write([]byte(s)); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// apiClient calls the API under test, sending token as a bearer token when set.
type apiClient struct {
	t     *testing.T
	base  string
	token string
}

type response struct {
	status int
	header http.Header
	body   string
}

func (c *apiClient) do(method, path string, body io.Reader, header map[string]string) response {
	c.t.Helper()
	req, err := http.NewRequest(method, c.base+path, body)
	if err != nil {
		c.t.Fatal(err)
	}
	for k, v := range header {
		req.Header.Set(k, v)
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	client := http.Client{Timeout: 30 * time.Second}
	res, err := client.Do(req)
	if err != nil {
		c.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer res.Body.Close()
	b, err := io.ReadAll(res.Body)
	if err != nil {
		c.t.Fatal(err)
	}
	return response{status: res.StatusCode, header: res.Header, body: string(b)}
}

// session returns the token of a register or login response with status.
func (c *apiClient) session(res response, status int) string {
	c.t.Helper()
	var s struct {
		Token string `json:"token"`
	}
	if err := json.Unmarshal([]byte(res.body), &s); err != nil || res.status != status || s.Token == "" {
		c.t.Fatalf("session response = %d %s; want %d with a token", res.status, res.body, status)
	}
	return s.Token
}
