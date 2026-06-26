package handlers

// bucket_store.go — FIX-OFFICE-STORE-WIRE-01: thin wrapper around an
// S3-compatible object client so file-CRUD and recording handlers can push
// blobs without importing the storage package's internal client directly.
//
// Two flavours:
//
//   - SharedBucketStore() — the process-wide org-bucket client resolved once at
//     startup from TIGRIS_*/VULOS_MINIO_* env (standalone/OSS mode). Keys are
//     org-scoped via storage.OrgScopedKey. The underlying client may be nil when
//     no S3 backend is configured, in which case every method is a graceful
//     no-op so callers can skip the S3 path without extra guard code.
//
//   - NewRequestBucketStore(headers) — a per-request client built from the
//     storage-seam headers the Vulos OS gateway injects (per-user, possibly
//     short-lived creds). Keys are scoped under Talk's "talk/" space inside the
//     user's shared bucket. Returns nil when the request is not behind the
//     gateway, signalling the caller to fall back to the shared/local path.

import (
	"io"
	"log"
	"net/http"

	"vulos-talk/backend/storage"
)

// BucketStore is a thin convenience wrapper around an S3 object client.
//
// When client is nil the store targets the process-wide org bucket
// (storage.OrgBucketClient), resolved lazily on every call so it works whether
// or not the backend was configured. When client is non-nil it is an explicit
// per-request gateway client and keys are scoped under Talk's product space.
type BucketStore struct {
	client *storage.OfficeS3Client
}

// sharedBucketStore is the process-wide singleton (client nil ⇒ uses
// OrgBucketClient lazily). Always non-nil.
var sharedBucketStore = &BucketStore{}

// SharedBucketStore returns the process-wide BucketStore singleton.
// The BucketStore itself is always non-nil; the underlying S3 client may be nil
// when no S3 backend is configured, in which case every method is a no-op.
func SharedBucketStore() *BucketStore {
	return sharedBucketStore
}

// NewRequestBucketStore returns a BucketStore bound to a per-request gateway S3
// client built from the injected storage-seam headers, or nil when the request
// carries no storage endpoint header (not behind the Vulos OS gateway).
func NewRequestBucketStore(h http.Header) *BucketStore {
	c, ok := storage.NewGatewayS3Client(h)
	if !ok {
		return nil
	}
	return &BucketStore{client: c}
}

// clientFor returns the S3 client this store should use, resolving the
// process-wide org bucket lazily when no explicit client is bound.
func (b *BucketStore) clientFor() *storage.OfficeS3Client {
	if b.client != nil {
		return b.client
	}
	return storage.OrgBucketClient()
}

// Active reports whether an S3 backend is available for this store.
func (b *BucketStore) Active() bool {
	return b.clientFor() != nil
}

// Key returns the object key (as persisted in MeetingRecording.BucketKey) for a
// given (accountID, name) pair under this store's scoping scheme.
func (b *BucketStore) Key(accountID, name string) string {
	if b.client != nil {
		return storage.TalkScopedName(accountID, name)
	}
	return storage.OrgScopedKey(accountID, name)
}

// PutObject uploads data scoped to (accountID, name).
// When no S3 client is available it logs nothing and returns nil so callers can
// ignore the error and treat the local/DB store as the sole source.
// The contentType argument is informational — the underlying OfficeS3Client does
// not forward Content-Type headers in this iteration.
func (b *BucketStore) PutObject(accountID, name string, data []byte, _ string) error {
	client := b.clientFor()
	if client == nil {
		return nil // S3 not configured — silent no-op
	}
	key := b.Key(accountID, name)
	if err := client.Put(key, data); err != nil {
		log.Printf("[bucket_store] PutObject key=%q: %v", key, err)
		return err
	}
	return nil
}

// GetObject downloads the object scoped to (accountID, name).
// Returns (nil, nil) when no S3 client is available so callers can skip the S3
// path cleanly.
func (b *BucketStore) GetObject(accountID, name string) ([]byte, error) {
	client := b.clientFor()
	if client == nil {
		return nil, nil // S3 not configured — signal "no object, not an error"
	}
	key := b.Key(accountID, name)
	rc, err := client.Get(key)
	if err != nil {
		log.Printf("[bucket_store] GetObject key=%q: %v", key, err)
		return nil, err
	}
	defer rc.Close()
	data, err := io.ReadAll(rc)
	if err != nil {
		return nil, err
	}
	return data, nil
}

// DeleteObject removes the object scoped to (accountID, name).
// Silent no-op when no S3 client is available.
func (b *BucketStore) DeleteObject(accountID, name string) error {
	client := b.clientFor()
	if client == nil {
		return nil // S3 not configured — no-op
	}
	key := b.Key(accountID, name)
	if err := client.Delete(key); err != nil {
		log.Printf("[bucket_store] DeleteObject key=%q: %v", key, err)
		return err
	}
	return nil
}
