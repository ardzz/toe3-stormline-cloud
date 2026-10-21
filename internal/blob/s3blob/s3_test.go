//go:build integration

package s3blob_test

import (
	"context"
	"errors"
	"testing"

	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/modules/s3mock"

	"github.com/ardzz/toe3-stormline-cloud/internal/blob"
	"github.com/ardzz/toe3-stormline-cloud/internal/blob/blobtest"
	"github.com/ardzz/toe3-stormline-cloud/internal/blob/s3blob"
)

func TestS3Store(t *testing.T) {
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
	cfg := func(bucket, prefix string) s3blob.Config {
		return s3blob.Config{Endpoint: endpoint, Region: "us-east-1", Bucket: bucket, Prefix: prefix, PathStyle: true,
			AccessKeyID: "test", SecretAccessKey: "test"}
	}
	t.Run("Contract", func(t *testing.T) { blobtest.Run(t, s3blob.New(cfg("saves", "contract"))) })
	t.Run("PrefixIsolation", func(t *testing.T) {
		a, b := s3blob.New(cfg("saves", "a")), s3blob.New(cfg("saves", "/b/"))
		if err := a.Put(ctx, "u/1/1.json.gz", []byte("x")); err != nil {
			t.Fatal(err)
		}
		if _, err := b.Get(ctx, "u/1/1.json.gz"); !errors.Is(err, blob.ErrNotFound) {
			t.Fatalf("other prefix Get = %v, want ErrNotFound", err)
		}
		if got, err := a.Get(ctx, "u/1/1.json.gz"); err != nil || string(got) != "x" {
			t.Fatalf("same prefix Get = %q, %v", got, err)
		}
	})
	t.Run("MissingBucketIsAnError", func(t *testing.T) {
		_, err := s3blob.New(cfg("missing", "")).Get(ctx, "u/1/1.json.gz")
		if err == nil || errors.Is(err, blob.ErrNotFound) {
			t.Fatalf("Get on a missing bucket = %v, want a non-ErrNotFound error", err)
		}
	})
}
