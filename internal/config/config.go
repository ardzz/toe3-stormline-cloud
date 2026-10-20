// Package config loads server settings from TOE3_* environment variables.
package config

import (
	"errors"
	"fmt"
	"net/netip"
	"strconv"
	"strings"
	"time"
)

// Registration controls whether players may create accounts from the app.
type Registration string

const (
	RegistrationClosed Registration = "closed"
	RegistrationOpen   Registration = "open"
)

// BlobBackend selects where save snapshots are stored.
type BlobBackend string

const (
	BlobFS BlobBackend = "fs"
	BlobS3 BlobBackend = "s3"
)

// MaxSnapshotLimit is the mod's hard download cap; the server limit may not exceed it.
const MaxSnapshotLimit = 64 << 20

// S3 configures an S3-compatible bucket. Credentials come from the standard AWS_* environment variables.
type S3 struct {
	Endpoint  string
	Bucket    string
	Prefix    string
	Region    string
	PathStyle bool
}

// Config is the complete server configuration.
type Config struct {
	Listen           string
	DatabaseURL      string
	Registration     Registration
	MaxSnapshotBytes int64
	Retention        int
	SessionTTL       time.Duration
	TrustedProxies   []netip.Prefix
	BlobBackend      BlobBackend
	BlobDir          string
	S3               S3
}

// Load reads the configuration through getenv (os.Getenv in production) and reports every invalid setting at once.
func Load(getenv func(string) string) (Config, error) {
	var errs []error
	str := func(key, def string) string {
		if v := strings.TrimSpace(getenv(key)); v != "" {
			return v
		}
		return def
	}
	c := Config{
		Listen:      str("TOE3_LISTEN", "127.0.0.1:8080"),
		DatabaseURL: str("TOE3_DATABASE_URL", ""),
		BlobDir:     str("TOE3_BLOB_DIR", "/data/blobs"),
		S3: S3{
			Endpoint: str("TOE3_S3_ENDPOINT", ""),
			Bucket:   str("TOE3_S3_BUCKET", ""),
			Prefix:   str("TOE3_S3_PREFIX", ""),
			Region:   str("TOE3_S3_REGION", ""),
		},
	}
	if c.DatabaseURL == "" {
		errs = append(errs, errors.New("TOE3_DATABASE_URL is required"))
	}

	switch r := Registration(str("TOE3_REGISTRATION", string(RegistrationClosed))); r {
	case RegistrationClosed, RegistrationOpen:
		c.Registration = r
	default:
		errs = append(errs, fmt.Errorf("TOE3_REGISTRATION must be closed or open, got %q", r))
	}

	maxBytes, err := strconv.ParseInt(str("TOE3_MAX_SNAPSHOT_BYTES", "16777216"), 10, 64)
	if err != nil || maxBytes < 1 || maxBytes > MaxSnapshotLimit {
		errs = append(errs, fmt.Errorf("TOE3_MAX_SNAPSHOT_BYTES must be an integer in 1..%d", MaxSnapshotLimit))
	}
	c.MaxSnapshotBytes = maxBytes

	retention, err := strconv.Atoi(str("TOE3_RETENTION", "20"))
	if err != nil || retention < 1 || retention > 1000 {
		errs = append(errs, errors.New("TOE3_RETENTION must be an integer in 1..1000"))
	}
	c.Retention = retention

	ttl, err := time.ParseDuration(str("TOE3_SESSION_TTL", "2160h"))
	if err != nil || ttl <= time.Hour {
		errs = append(errs, errors.New("TOE3_SESSION_TTL must be a Go duration longer than 1h, e.g. 2160h"))
	}
	c.SessionTTL = ttl

	proxies, err := parsePrefixes(getenv("TOE3_TRUSTED_PROXIES"))
	if err != nil {
		errs = append(errs, fmt.Errorf("TOE3_TRUSTED_PROXIES: %w", err))
	}
	c.TrustedProxies = proxies

	switch b := BlobBackend(str("TOE3_BLOB_BACKEND", string(BlobFS))); b {
	case BlobFS:
		c.BlobBackend = b
	case BlobS3:
		c.BlobBackend = b
		if c.S3.Bucket == "" {
			errs = append(errs, errors.New("TOE3_S3_BUCKET is required when TOE3_BLOB_BACKEND=s3"))
		}
		if c.S3.Region == "" {
			errs = append(errs, errors.New("TOE3_S3_REGION is required when TOE3_BLOB_BACKEND=s3"))
		}
	default:
		errs = append(errs, fmt.Errorf("TOE3_BLOB_BACKEND must be fs or s3, got %q", b))
	}

	pathStyle, err := strconv.ParseBool(str("TOE3_S3_PATH_STYLE", "false"))
	if err != nil {
		errs = append(errs, errors.New("TOE3_S3_PATH_STYLE must be true or false"))
	}
	c.S3.PathStyle = pathStyle

	return c, errors.Join(errs...)
}

// parsePrefixes parses a comma-separated list of CIDRs or bare addresses (treated as single-host prefixes).
func parsePrefixes(s string) ([]netip.Prefix, error) {
	var out []netip.Prefix
	for _, f := range strings.Split(s, ",") {
		f = strings.TrimSpace(f)
		if f == "" {
			continue
		}
		if strings.Contains(f, "/") {
			p, err := netip.ParsePrefix(f)
			if err != nil {
				return nil, err
			}
			out = append(out, p.Masked())
			continue
		}
		a, err := netip.ParseAddr(f)
		if err != nil {
			return nil, err
		}
		out = append(out, netip.PrefixFrom(a, a.BitLen()))
	}
	return out, nil
}
