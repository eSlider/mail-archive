package storage

import (
	"context"
	"errors"
	"testing"
)

// TestS3StoreRetrieve verifies Put and Get against an S3-compatible store (e.g. MinIO).
//
// Run MinIO first:
//
//	docker compose --profile s3 up -d minio
//
// Then set env and run:
//
//	S3_ENDPOINT=http://localhost:9900 S3_ACCESS_KEY_ID=minioadmin S3_SECRET_ACCESS_KEY=minioadmin S3_BUCKET=mails-test S3_USE_SSL=false go test -v ./internal/storage/ -run TestS3StoreRetrieve
func TestS3StoreRetrieve(t *testing.T) {
	cfg := ConfigFromEnv()
	if cfg == nil {
		t.Skip("S3_ENDPOINT not set, skipping integration test")
	}

	client, err := NewS3Client(cfg)
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}

	ctx := context.Background()
	if err := client.EnsureBucket(ctx); err != nil {
		t.Fatalf("EnsureBucket: %v", err)
	}

	key := "test/integration/hello.eml"
	content := []byte("From: test@example.com\r\nTo: you@example.com\r\nSubject: S3 Test\r\n\r\nHello from S3 storage test.\r\n")

	if err := client.PutBytes(ctx, key, content); err != nil {
		t.Fatalf("PutBytes: %v", err)
	}

	got, err := client.Get(ctx, key)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}

	if string(got) != string(content) {
		t.Errorf("got %q, want %q", got, content)
	}
}

// TestS3GetNotFound verifies Get returns ErrNotFound for missing keys.
func TestS3GetNotFound(t *testing.T) {
	cfg := ConfigFromEnv()
	if cfg == nil {
		t.Skip("S3_ENDPOINT not set, skipping integration test")
	}

	client, err := NewS3Client(cfg)
	if err != nil {
		t.Fatalf("NewS3Client: %v", err)
	}

	ctx := context.Background()
	_, err = client.Get(ctx, "nonexistent/key/12345.eml")
	if err == nil {
		t.Fatal("Get: expected error for missing key")
	}
	if !errors.Is(err, ErrNotFound) {
		t.Errorf("Get: got %v, want ErrNotFound", err)
	}
}
