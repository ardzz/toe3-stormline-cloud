// Package s3blob stores blobs in an S3-compatible bucket.
package s3blob

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/ardzz/toe3-stormline-cloud/internal/blob"
)

var _ blob.Store = (*Store)(nil)

// Config locates the bucket and its credentials.
type Config struct {
	Endpoint        string // service endpoint without the bucket; empty means AWS
	Region          string
	Bucket          string
	Prefix          string // optional key prefix, e.g. "toe3"
	PathStyle       bool
	AccessKeyID     string
	SecretAccessKey string
	SessionToken    string
}

// Store keeps blobs as objects under an optional prefix.
type Store struct {
	client *s3.Client
	bucket string
	prefix string
}

// New returns a Store. SDK checksums are only computed or validated when an operation requires them, because
// several S3-compatible services reject the SDK's default checksum headers.
func New(c Config) *Store {
	cfg := aws.Config{
		Region:                     c.Region,
		Credentials:                credentials.NewStaticCredentialsProvider(c.AccessKeyID, c.SecretAccessKey, c.SessionToken),
		RequestChecksumCalculation: aws.RequestChecksumCalculationWhenRequired,
		ResponseChecksumValidation: aws.ResponseChecksumValidationWhenRequired,
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if c.Endpoint != "" {
			o.BaseEndpoint = aws.String(c.Endpoint)
		}
		o.UsePathStyle = c.PathStyle
	})
	prefix := strings.Trim(c.Prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	return &Store{client: client, bucket: c.Bucket, prefix: prefix}
}

func (s *Store) object(key string) (*string, error) {
	if err := blob.CheckKey(key); err != nil {
		return nil, err
	}
	return aws.String(s.prefix + key), nil
}

// Put uploads data as one object.
func (s *Store) Put(ctx context.Context, key string, data []byte) error {
	obj, err := s.object(key)
	if err != nil {
		return err
	}
	_, err = s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(s.bucket),
		Key:           obj,
		Body:          bytes.NewReader(data),
		ContentLength: aws.Int64(int64(len(data))),
	})
	return err
}

// Get downloads an object, mapping a missing key to blob.ErrNotFound.
func (s *Store) Get(ctx context.Context, key string) ([]byte, error) {
	obj, err := s.object(key)
	if err != nil {
		return nil, err
	}
	out, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.bucket), Key: obj})
	if isMissing(err) {
		return nil, blob.ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

// Delete removes an object; S3 reports success for a missing key.
func (s *Store) Delete(ctx context.Context, key string) error {
	obj, err := s.object(key)
	if err != nil {
		return err
	}
	_, err = s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.bucket), Key: obj})
	return err
}

// isMissing reports a missing object. Some services return a generic error that carries only the error code.
func isMissing(err error) bool {
	var noSuchKey *types.NoSuchKey
	if errors.As(err, &noSuchKey) {
		return true
	}
	var coded interface{ ErrorCode() string }
	if errors.As(err, &coded) {
		switch coded.ErrorCode() {
		case "NoSuchKey", "NotFound":
			return true
		}
	}
	return false
}
