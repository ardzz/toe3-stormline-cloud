package main

import (
	"github.com/ardzz/toe3-stormline-cloud/internal/blob"
	"github.com/ardzz/toe3-stormline-cloud/internal/blob/fsblob"
	"github.com/ardzz/toe3-stormline-cloud/internal/blob/s3blob"
	"github.com/ardzz/toe3-stormline-cloud/internal/config"
)

// openBlobs opens the configured snapshot store; release frees it once nothing uses the store any more.
func openBlobs(cfg config.Config) (store blob.Store, release func() error, err error) {
	if cfg.BlobBackend == config.BlobS3 {
		s := cfg.S3
		return s3blob.New(s3blob.Config{Endpoint: s.Endpoint, Region: s.Region, Bucket: s.Bucket, Prefix: s.Prefix,
			PathStyle: s.PathStyle, AccessKeyID: s.AccessKeyID, SecretAccessKey: s.SecretAccessKey,
			SessionToken: s.SessionToken}), func() error { return nil }, nil
	}
	fs, err := fsblob.New(cfg.BlobDir)
	if err != nil {
		return nil, nil, err
	}
	return fs, fs.Close, nil
}
