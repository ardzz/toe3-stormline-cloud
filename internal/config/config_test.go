package config

import (
	"net/netip"
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string { return func(k string) string { return m[k] } }

func TestLoadDefaults(t *testing.T) {
	c, err := Load(env(map[string]string{"TOE3_DATABASE_URL": "postgres://db/toe3"}))
	if err != nil {
		t.Fatal(err)
	}
	if c.Listen != "127.0.0.1:8080" || c.Registration != RegistrationClosed || c.MaxSnapshotBytes != 16<<20 ||
		c.Retention != 20 || c.SessionTTL != 2160*time.Hour || c.BlobBackend != BlobFS || c.BlobDir != "/data/blobs" ||
		len(c.TrustedProxies) != 0 || c.S3.PathStyle {
		t.Fatalf("unexpected defaults: %+v", c)
	}
}

func TestLoadRejectsInvalidSettings(t *testing.T) {
	base := map[string]string{"TOE3_DATABASE_URL": "postgres://db/toe3"}
	cases := map[string][2]string{
		"registration": {"TOE3_REGISTRATION", "invite"},
		"snapshot 0":   {"TOE3_MAX_SNAPSHOT_BYTES", "0"},
		"snapshot big": {"TOE3_MAX_SNAPSHOT_BYTES", "67108865"},
		"retention":    {"TOE3_RETENTION", "0"},
		"ttl short":    {"TOE3_SESSION_TTL", "30m"},
		"ttl 1h":       {"TOE3_SESSION_TTL", "1h"},
		"ttl days":     {"TOE3_SESSION_TTL", "90d"},
		"proxy":        {"TOE3_TRUSTED_PROXIES", "10.0.0.0/33"},
		"backend":      {"TOE3_BLOB_BACKEND", "gcs"},
		"path style":   {"TOE3_S3_PATH_STYLE", "maybe"},
	}
	for name, kv := range cases {
		m := map[string]string{kv[0]: kv[1]}
		for k, v := range base {
			m[k] = v
		}
		if _, err := Load(env(m)); err == nil || !strings.Contains(err.Error(), kv[0]) {
			t.Errorf("%s: err = %v, want mention of %s", name, err, kv[0])
		}
	}
}

func TestLoadReportsEveryProblem(t *testing.T) {
	_, err := Load(env(map[string]string{"TOE3_RETENTION": "x"}))
	if err == nil || !strings.Contains(err.Error(), "TOE3_DATABASE_URL") || !strings.Contains(err.Error(), "TOE3_RETENTION") {
		t.Fatalf("err = %v, want both TOE3_DATABASE_URL and TOE3_RETENTION", err)
	}
}

func TestLoadSessionTTLMustExceedTouchInterval(t *testing.T) {
	_, err := Load(env(map[string]string{"TOE3_DATABASE_URL": "postgres://db/toe3", "TOE3_SESSION_TTL": "1h"}))
	if want := "TOE3_SESSION_TTL must be a Go duration longer than 1h, e.g. 2160h"; err == nil || err.Error() != want {
		t.Fatalf("1h: err = %v, want %q", err, want)
	}
	c, err := Load(env(map[string]string{"TOE3_DATABASE_URL": "postgres://db/toe3", "TOE3_SESSION_TTL": "61m"}))
	if err != nil || c.SessionTTL != 61*time.Minute {
		t.Fatalf("61m: Load = %v, %v; want 61m, nil", c.SessionTTL, err)
	}
}

func TestLoadS3RequiresBucketAndRegion(t *testing.T) {
	_, err := Load(env(map[string]string{"TOE3_DATABASE_URL": "postgres://db/toe3", "TOE3_BLOB_BACKEND": "s3"}))
	if err == nil || !strings.Contains(err.Error(), "TOE3_S3_BUCKET") || !strings.Contains(err.Error(), "TOE3_S3_REGION") {
		t.Fatalf("err = %v, want bucket and region errors", err)
	}
	c, err := Load(env(map[string]string{"TOE3_DATABASE_URL": "postgres://db/toe3", "TOE3_BLOB_BACKEND": "s3",
		"TOE3_S3_BUCKET": "saves", "TOE3_S3_REGION": "auto", "TOE3_S3_ENDPOINT": "https://s3.example", "TOE3_S3_PATH_STYLE": "true"}))
	if err != nil || c.S3 != (S3{Endpoint: "https://s3.example", Bucket: "saves", Region: "auto", PathStyle: true}) {
		t.Fatalf("Load = %+v, %v", c.S3, err)
	}
}

func TestLoadTrustedProxies(t *testing.T) {
	c, err := Load(env(map[string]string{"TOE3_DATABASE_URL": "postgres://db/toe3", "TOE3_TRUSTED_PROXIES": " 10.1.2.3/8, 127.0.0.1 ,::1"}))
	if err != nil {
		t.Fatal(err)
	}
	want := []netip.Prefix{netip.MustParsePrefix("10.0.0.0/8"), netip.MustParsePrefix("127.0.0.1/32"), netip.MustParsePrefix("::1/128")}
	if len(c.TrustedProxies) != len(want) {
		t.Fatalf("got %v, want %v", c.TrustedProxies, want)
	}
	for i := range want {
		if c.TrustedProxies[i] != want[i] {
			t.Fatalf("got %v, want %v", c.TrustedProxies, want)
		}
	}
}
