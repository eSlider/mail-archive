// Package storage provides S3-compatible object storage (AWS S3, MinIO).
package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

// S3Config holds S3/MinIO connection settings.
type S3Config struct {
	Endpoint        string // e.g. http://localhost:9000
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
	UseSSL          bool
	Region          string
}

// S3Client provides Put and Get for objects in S3-compatible storage.
type S3Client struct {
	client *s3.Client
	bucket string
}

// ConfigFromEnv reads S3 config from environment variables.
// Returns nil if S3_ENDPOINT is not set.
func ConfigFromEnv() *S3Config {
	endpoint := os.Getenv("S3_ENDPOINT")
	if endpoint == "" {
		return nil
	}
	useSSL := true
	if v := os.Getenv("S3_USE_SSL"); v != "" {
		useSSL, _ = strconv.ParseBool(v)
	}
	return &S3Config{
		Endpoint:        normalizeEndpoint(endpoint, useSSL),
		AccessKeyID:     os.Getenv("S3_ACCESS_KEY_ID"),
		SecretAccessKey: os.Getenv("S3_SECRET_ACCESS_KEY"),
		Bucket:          envOr("S3_BUCKET", "mails"),
		UseSSL:          useSSL,
		Region:          envOr("AWS_REGION", "us-east-1"),
	}
}

func normalizeEndpoint(endpoint string, useSSL bool) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	scheme := "https"
	if !useSSL {
		scheme = "http"
	}
	if !strings.HasPrefix(endpoint, "http://") && !strings.HasPrefix(endpoint, "https://") {
		return scheme + "://" + endpoint
	}
	return endpoint
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

// NewS3Client creates an S3 client from config. Returns error if config is invalid.
func NewS3Client(cfg *S3Config) (*S3Client, error) {
	if cfg == nil || cfg.Endpoint == "" {
		return nil, fmt.Errorf("storage: S3 config required (endpoint)")
	}
	if cfg.Bucket == "" {
		return nil, fmt.Errorf("storage: S3 bucket required")
	}

	credProvider := credentials.NewStaticCredentialsProvider(
		cfg.AccessKeyID,
		cfg.SecretAccessKey,
		"",
	)

	customResolver := aws.EndpointResolverWithOptionsFunc(func(service, region string, opts ...interface{}) (aws.Endpoint, error) {
		return aws.Endpoint{
			URL:               cfg.Endpoint,
			HostnameImmutable: true,
			SigningRegion:     cfg.Region,
		}, nil
	})

	client := s3.NewFromConfig(aws.Config{
		Region:                      cfg.Region,
		Credentials:                 credProvider,
		EndpointResolverWithOptions: customResolver,
	}, func(o *s3.Options) {
		o.UsePathStyle = true // required for MinIO
	})

	return &S3Client{client: client, bucket: cfg.Bucket}, nil
}

// EnsureBucket creates the bucket if it does not exist.
func (c *S3Client) EnsureBucket(ctx context.Context) error {
	_, err := c.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(c.bucket)})
	if err == nil {
		return nil
	}
	// Try to create
	_, err = c.client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(c.bucket),
	})
	if err != nil {
		// Bucket may have been created concurrently
		var conflict *types.BucketAlreadyOwnedByYou
		if errors.As(err, &conflict) {
			return nil
		}
		return fmt.Errorf("create bucket %s: %w", c.bucket, err)
	}
	return nil
}

// Put writes an object to the given key.
func (c *S3Client) Put(ctx context.Context, key string, body io.Reader) error {
	_, err := c.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
		Body:   body,
	})
	return err
}

// PutBytes writes bytes to the given key.
func (c *S3Client) PutBytes(ctx context.Context, key string, data []byte) error {
	return c.Put(ctx, key, bytes.NewReader(data))
}

// Get reads an object by key. Returns ErrNotFound if the object does not exist.
func (c *S3Client) Get(ctx context.Context, key string) ([]byte, error) {
	out, err := c.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		var noSuchKey *types.NoSuchKey
		var notFound *types.NotFound
		if errors.As(err, &noSuchKey) || errors.As(err, &notFound) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	defer out.Body.Close()
	return io.ReadAll(out.Body)
}

// List lists object keys with the given prefix.
func (c *S3Client) List(ctx context.Context, prefix string) ([]string, error) {
	var keys []string
	var contToken *string
	for {
		out, err := c.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket:            aws.String(c.bucket),
			Prefix:            aws.String(prefix),
			ContinuationToken: contToken,
		})
		if err != nil {
			return nil, err
		}
		for _, obj := range out.Contents {
			if obj.Key != nil {
				keys = append(keys, *obj.Key)
			}
		}
		if !aws.ToBool(out.IsTruncated) {
			break
		}
		contToken = out.NextContinuationToken
	}
	return keys, nil
}

// ErrNotFound is returned when an object does not exist.
var ErrNotFound = errors.New("object not found")
