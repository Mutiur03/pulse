package replay

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log"
	"os"
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

// S3ConfigFromEnv reads S3_ENDPOINT (host:port), S3_ACCESS_KEY,
// S3_SECRET_KEY, S3_BUCKET, S3_REGION and S3_USE_SSL. ok is false when no
// endpoint is set, which disables replay storage.
func S3ConfigFromEnv() (S3Config, bool) {
	c := S3Config{
		Endpoint:  os.Getenv("S3_ENDPOINT"),
		AccessKey: os.Getenv("S3_ACCESS_KEY"),
		SecretKey: os.Getenv("S3_SECRET_KEY"),
		Bucket:    os.Getenv("S3_BUCKET"),
		Region:    os.Getenv("S3_REGION"),
		UseSSL:    os.Getenv("S3_USE_SSL") == "true",
	}
	if c.Bucket == "" {
		c.Bucket = "pulse-replays"
	}
	if c.Region == "" {
		c.Region = "us-east-1"
	}
	return c, c.Endpoint != ""
}

// Connect opens the configured storage, or returns nil when none is set or
// it cannot be reached (replay is then disabled, everything else works).
func Connect() *S3 {
	cfg, ok := S3ConfigFromEnv()
	return connect(cfg, ok, "session replay")
}

// ConnectArtifacts opens the bucket for source maps: S3_ARTIFACT_BUCKET,
// default pulse-artifacts, on the same server as replays.
func ConnectArtifacts() *S3 {
	cfg, ok := S3ConfigFromEnv()
	cfg.Bucket = os.Getenv("S3_ARTIFACT_BUCKET")
	if cfg.Bucket == "" {
		cfg.Bucket = "pulse-artifacts"
	}
	return connect(cfg, ok, "source map")
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
