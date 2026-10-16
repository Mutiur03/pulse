package replay

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Config points at any S3-compatible service.
type S3Config struct {
	Endpoint  string // host:port
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
	Region    string
}

// S3 stores replay chunks in a bucket.
type S3 struct {
	c      *minio.Client
	bucket string
}

// NewS3 connects and makes sure the bucket exists, retrying while the
// storage service starts.
func NewS3(ctx context.Context, cfg S3Config) (*S3, error) {
	c, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
	})
	if err != nil {
		return nil, err
	}
	s := &S3{c: c, bucket: cfg.Bucket}
	for i := 0; ; i++ {
		err = s.ensureBucket(ctx, cfg.Region)
		if err == nil || i >= 30 {
			return s, err
		}
		log.Printf("object storage not ready (%v), retrying", err)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Second):
		}
	}
}

func (s *S3) ensureBucket(ctx context.Context, region string) error {
	ok, err := s.c.BucketExists(ctx, s.bucket)
	if err != nil || ok {
		return err
	}
	err = s.c.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: region})
	if err != nil {
		if ok, err2 := s.c.BucketExists(ctx, s.bucket); err2 == nil && ok {
			return nil // created concurrently by another service
		}
	}
	return err
}

func (s *S3) Put(ctx context.Context, key string, data []byte) error {
	_, err := s.c.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)),
		minio.PutObjectOptions{ContentType: "application/json", ContentEncoding: "gzip"})
	return err
}

func (s *S3) Get(ctx context.Context, key string) ([]byte, error) {
	o, err := s.c.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, err
	}
	defer func() { _ = o.Close() }()
	b, err := io.ReadAll(o)
	var resp minio.ErrorResponse
	if errors.As(err, &resp) && resp.Code == "NoSuchKey" {
		return nil, ErrNotFound
	}
	return b, err
}

// Connect opens the replay bucket, or returns nil when no endpoint is set or
// it cannot be reached (replay is then disabled, everything else works).
func Connect(cfg S3Config) *S3 {
	return connect(cfg, cfg.Endpoint != "", "session replay")
}

// ConnectArtifacts opens the source map bucket, on the same server as
// replays; bucket defaults to pulse-artifacts.
func ConnectArtifacts(cfg S3Config, bucket string) *S3 {
	if bucket == "" {
		bucket = "pulse-artifacts"
	}
	cfg.Bucket = bucket
	return connect(cfg, cfg.Endpoint != "", "source map")
}

func connect(cfg S3Config, ok bool, what string) *S3 {
	if !ok {
		log.Printf("S3_ENDPOINT not set: %s storage disabled", what)
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	s, err := NewS3(ctx, cfg)
	if err != nil {
		log.Printf("%s storage unavailable, disabled: %v", what, err)
		return nil
	}
	return s
}

// Object is a stored object's key and last change.
type Object struct {
	Key      string
	Modified time.Time
}

// List returns the objects under a prefix.
func (s *S3) List(ctx context.Context, prefix string) ([]Object, error) {
	var out []Object
	for o := range s.c.ListObjects(ctx, s.bucket, minio.ListObjectsOptions{Prefix: prefix, Recursive: true}) {
		if o.Err != nil {
			return out, o.Err
		}
		out = append(out, Object{Key: o.Key, Modified: o.LastModified})
	}
	return out, nil
}

// Remove deletes an object; a missing object is not an error.
func (s *S3) Remove(ctx context.Context, key string) error {
	err := s.c.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{})
	var resp minio.ErrorResponse
	if errors.As(err, &resp) && resp.Code == "NoSuchKey" {
		return nil
	}
	return err
}
