// Package storage provides S3-compatible object storage and a BlobStore
// abstraction for user data (users/ directory layout).
package storage

import (
	"context"
	"os"
	"path/filepath"
	"strings"
)

// BlobStore reads and writes blobs by key. Keys use forward slashes and are
// relative to the users directory (e.g. "uuid/user.json", "uuid/domain/local/inbox/file.eml").
type BlobStore interface {
	Write(ctx context.Context, key string, data []byte) error
	Read(ctx context.Context, key string) ([]byte, error)
	List(ctx context.Context, prefix string) ([]string, error)
}

// FSBlobStore stores blobs on the local filesystem.
type FSBlobStore struct {
	root string
}

// NewFSBlobStore creates a filesystem-backed blob store.
func NewFSBlobStore(root string) *FSBlobStore {
	return &FSBlobStore{root: filepath.Clean(root)}
}

// Write writes data to key (path relative to root).
func (f *FSBlobStore) Write(ctx context.Context, key string, data []byte) error {
	path := filepath.Join(f.root, filepath.FromSlash(key))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, data, 0o644)
}

// Read reads a blob by key.
func (f *FSBlobStore) Read(ctx context.Context, key string) ([]byte, error) {
	path := filepath.Join(f.root, filepath.FromSlash(key))
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return data, nil
}

// List returns keys under prefix (non-recursive for first level, or recursive based on impl).
// FS impl walks recursively to match S3 List behavior.
func (f *FSBlobStore) List(ctx context.Context, prefix string) ([]string, error) {
	dir := filepath.Join(f.root, filepath.FromSlash(prefix))
	var keys []string
	err := filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if os.IsNotExist(err) {
				return nil
			}
			return err
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(f.root, path)
		if err != nil {
			return nil
		}
		keys = append(keys, filepath.ToSlash(rel))
		return nil
	})
	return keys, err
}

// S3BlobStore stores blobs in S3. Keys are used as S3 object keys.
type S3BlobStore struct {
	client *S3Client
	prefix string
}

// NewS3BlobStore creates an S3-backed blob store with optional key prefix.
func NewS3BlobStore(client *S3Client, prefix string) *S3BlobStore {
	prefix = strings.Trim(prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	return &S3BlobStore{client: client, prefix: prefix}
}

// Write writes data to key.
func (s *S3BlobStore) Write(ctx context.Context, key string, data []byte) error {
	return s.client.PutBytes(ctx, s.prefix+key, data)
}

// Read reads a blob by key.
func (s *S3BlobStore) Read(ctx context.Context, key string) ([]byte, error) {
	return s.client.Get(ctx, s.prefix+key)
}

// NewBlobStore returns a BlobStore from env. If S3 env vars are set, returns S3BlobStore;
// otherwise returns FSBlobStore rooted at dataDir.
func NewBlobStore(dataDir string) (BlobStore, error) {
	cfg := ConfigFromEnv()
	if cfg != nil && cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		client, err := NewS3Client(cfg)
		if err != nil {
			return nil, err
		}
		ctx := context.Background()
		if err := client.EnsureBucket(ctx); err != nil {
			return nil, err
		}
		return NewS3BlobStore(client, "users"), nil
	}
	return NewFSBlobStore(dataDir), nil
}

// List returns keys under prefix (relative to prefix, without store prefix).
func (s *S3BlobStore) List(ctx context.Context, prefix string) ([]string, error) {
	fullPrefix := s.prefix + prefix
	all, err := s.client.List(ctx, fullPrefix)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(all))
	for _, k := range all {
		if k != "" {
			keys = append(keys, strings.TrimPrefix(k, s.prefix))
		}
	}
	return keys, nil
}
