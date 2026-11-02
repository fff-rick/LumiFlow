package media

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

type S3Config struct {
	Endpoint, AccessKey, SecretKey, Bucket, Region string
	Secure, PublicRead                             bool
	BytesPerSecond                                 int64
}

type S3ObjectStore struct {
	client *minio.Client
	bucket string
	region string
	public bool
}

func NewS3ObjectStore(config S3Config) (*S3ObjectStore, error) {
	transport, err := minio.DefaultTransport(config.Secure)
	if err != nil {
		return nil, err
	}
	var roundTripper http.RoundTripper = transport
	if config.BytesPerSecond > 0 {
		roundTripper = &rateLimitedTransport{base: transport, bytesPerSecond: config.BytesPerSecond}
	}
	client, err := minio.New(config.Endpoint, &minio.Options{
		Creds: credentials.NewStaticV4(config.AccessKey, config.SecretKey, ""), Secure: config.Secure,
		Region: config.Region, Transport: roundTripper,
	})
	if err != nil {
		return nil, err
	}
	return &S3ObjectStore{client: client, bucket: config.Bucket, region: config.Region, public: config.PublicRead}, nil
}

type rateLimitedTransport struct {
	base           http.RoundTripper
	bytesPerSecond int64
}

func (t *rateLimitedTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.Body == nil {
		return t.base.RoundTrip(request)
	}
	clone := request.Clone(request.Context())
	clone.Body = &limitedReadCloser{
		Reader: &rateLimitedReader{ctx: request.Context(), reader: request.Body, bytesPerSecond: t.bytesPerSecond, started: time.Now()},
		Closer: request.Body,
	}
	return t.base.RoundTrip(clone)
}

type limitedReadCloser struct {
	io.Reader
	io.Closer
}

type rateLimitedReader struct {
	ctx            context.Context
	reader         io.Reader
	bytesPerSecond int64
	started        time.Time
	read           int64
}

func (r *rateLimitedReader) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	r.read += int64(n)
	want := time.Duration(float64(r.read) / float64(r.bytesPerSecond) * float64(time.Second))
	wait := time.Until(r.started.Add(want))
	if wait <= 0 {
		return n, err
	}
	timer := time.NewTimer(wait)
	defer timer.Stop()
	select {
	case <-r.ctx.Done():
		return n, r.ctx.Err()
	case <-timer.C:
		return n, err
	}
}

func (s *S3ObjectStore) EnsureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return err
	}
	if !exists {
		if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: s.region}); err != nil {
			return err
		}
	}
	if !s.public {
		return nil
	}
	policy, err := json.Marshal(map[string]any{
		"Version": "2012-10-17",
		"Statement": []map[string]any{{
			"Effect": "Allow", "Principal": map[string]string{"AWS": "*"},
			"Action": []string{"s3:GetObject"}, "Resource": []string{"arn:aws:s3:::" + s.bucket + "/*"},
		}},
	})
	if err != nil {
		return err
	}
	return s.client.SetBucketPolicy(ctx, s.bucket, string(policy))
}

func (s *S3ObjectStore) Put(ctx context.Context, objectKey string, reader io.Reader, size int64) error {
	info, err := s.client.PutObject(ctx, s.bucket, objectKey, reader, size, minio.PutObjectOptions{
		ContentType: "video/mp4", PartSize: 16 << 20, NumThreads: 1,
	})
	if err != nil {
		return err
	}
	if info.Size != size {
		return fmt.Errorf("uploaded size %d does not match recording size %d", info.Size, size)
	}
	remote, err := s.client.StatObject(ctx, s.bucket, objectKey, minio.StatObjectOptions{})
	if err != nil {
		return err
	}
	if remote.Size != size {
		return fmt.Errorf("stored size %d does not match recording size %d", remote.Size, size)
	}
	return nil
}
