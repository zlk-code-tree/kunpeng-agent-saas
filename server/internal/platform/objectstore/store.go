package objectstore

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"sync"

	"gin-dify-saas/server/internal/config"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type Store struct {
	client *minio.Client
	bucket string
	once   sync.Once
	err    error
}

func New(cfg config.StorageConfig) (*Store, error) {
	endpoint, err := url.Parse(cfg.Endpoint)
	if err != nil || endpoint.Host == "" {
		return nil, fmt.Errorf("invalid S3 endpoint")
	}
	client, err := minio.New(endpoint.Host, &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL || strings.EqualFold(endpoint.Scheme, "https"),
	})
	if err != nil {
		return nil, fmt.Errorf("create S3 client: %w", err)
	}
	return &Store{client: client, bucket: cfg.Bucket}, nil
}

func (s *Store) Put(ctx context.Context, key, contentType string, size int64, reader io.Reader) error {
	s.once.Do(func() {
		exists, err := s.client.BucketExists(ctx, s.bucket)
		if err != nil {
			s.err = err
			return
		}
		if !exists {
			s.err = s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{})
		}
	})
	if s.err != nil {
		return fmt.Errorf("prepare S3 bucket: %w", s.err)
	}
	_, err := s.client.PutObject(ctx, s.bucket, key, reader, size, minio.PutObjectOptions{ContentType: contentType})
	if err != nil {
		return fmt.Errorf("upload S3 object: %w", err)
	}
	return nil
}
