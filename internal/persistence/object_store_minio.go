package persistence

import (
	"context"
	"io"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type MinioObjectStore struct {
	client *minio.Client
	bucket string
	prefix string
}

func NewMinioObjectStoreFromOptions(options *Options) (ObjectStore, error) {
	client, err := minio.New(options.OffloadEndpoint, &minio.Options{
		Creds:  credentials.NewStaticV4(options.OffloadAccessKey, options.OffloadSecretKey, ""),
		Secure: options.OffloadUseSSL,
	})
	if err != nil {
		return nil, err
	}

	ctx := context.Background()
	exists, err := client.BucketExists(ctx, options.OffloadBucket)
	if err != nil {
		return nil, err
	}
	if !exists {
		if err := client.MakeBucket(ctx, options.OffloadBucket, minio.MakeBucketOptions{Region: options.OffloadRegion}); err != nil {
			return nil, err
		}
	}

	return &MinioObjectStore{
		client: client,
		bucket: options.OffloadBucket,
		prefix: options.OffloadPrefix,
	}, nil
}

func (s *MinioObjectStore) PutObject(key string, r io.Reader) error {
	_, err := s.client.PutObject(context.Background(), s.bucket, s.prefix+key, r, -1, minio.PutObjectOptions{})
	return err
}

func (s *MinioObjectStore) GetObject(key string) (io.ReadCloser, error) {
	return s.client.GetObject(context.Background(), s.bucket, s.prefix+key, minio.GetObjectOptions{})
}

func (s *MinioObjectStore) StatObject(key string) (bool, error) {
	_, err := s.client.StatObject(context.Background(), s.bucket, s.prefix+key, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.StatusCode == 404 {
		return false, nil
	}
	return false, err
}

func (s *MinioObjectStore) DeleteObject(key string) error {
	err := s.client.RemoveObject(context.Background(), s.bucket, s.prefix+key, minio.RemoveObjectOptions{})
	if err == nil {
		return nil
	}
	resp := minio.ToErrorResponse(err)
	if resp.StatusCode == 404 {
		return nil
	}
	return err
}
