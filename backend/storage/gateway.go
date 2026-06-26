// gateway.go — per-request storage seam injected by the Vulos OS gateway.
//
// When Talk runs behind the Vulos OS, the gateway authenticates the user and
// injects per-request S3 credentials for that user's shared bucket as request
// headers (see the constants below). These creds are per-user and often
// short-lived (an optional STS session token may accompany them), so a client
// built from them MUST be scoped to a single request — never cached as a
// process-wide singleton like the OrgBucketClient used in standalone mode.
//
// Talk carves out its own key space under the injected prefix using the
// TalkPrefixSpace ("talk/") so its blobs never collide with other Vulos
// products that share the same per-user bucket.
//
// When the endpoint header is empty/absent the request is NOT behind the
// gateway and callers must fall back to Talk's existing standalone storage
// (process-wide OrgBucketClient, or the local filesystem).
package storage

import (
	"net/http"
	"strings"
)

// Per-request storage seam headers injected by the Vulos OS gateway. They are
// present ONLY behind the gateway; absent in standalone/OSS mode.
const (
	HdrStorageEndpoint  = "X-Vulos-Storage-Endpoint"
	HdrStorageBucket    = "X-Vulos-Storage-Bucket"
	HdrStoragePrefix    = "X-Vulos-Storage-Prefix"
	HdrStorageRegion    = "X-Vulos-Storage-Region"
	HdrStorageAccessKey = "X-Vulos-Storage-Access-Key"
	HdrStorageSecretKey = "X-Vulos-Storage-Secret-Key"
	HdrStorageSession   = "X-Vulos-Storage-Session-Token"
)

// TalkPrefixSpace is the per-product key space Talk owns inside the shared
// per-user bucket. All Talk blobs live under "<injected-prefix>/talk/".
const TalkPrefixSpace = "talk"

// NewGatewayS3Client builds a per-request S3 client from the storage-seam
// headers injected by the Vulos OS gateway. The returned bool is false when the
// request carries no storage endpoint header (i.e. it is not behind the
// gateway) and callers must fall back to standalone storage.
//
// The client's prefix is "<X-Vulos-Storage-Prefix>/talk" so every object it
// writes is confined to Talk's key space within the user's shared bucket.
func NewGatewayS3Client(h http.Header) (*OfficeS3Client, bool) {
	endpoint := strings.TrimSpace(h.Get(HdrStorageEndpoint))
	if endpoint == "" {
		return nil, false
	}

	region := strings.TrimSpace(h.Get(HdrStorageRegion))
	if region == "" {
		region = "auto"
	}

	// Carve out Talk's product space under the injected per-user prefix.
	prefix := strings.Trim(strings.TrimSpace(h.Get(HdrStoragePrefix)), "/")
	if prefix == "" {
		prefix = TalkPrefixSpace
	} else {
		prefix = prefix + "/" + TalkPrefixSpace
	}

	return &OfficeS3Client{
		endpoint:        strings.TrimSuffix(endpoint, "/"),
		region:          region,
		bucket:          strings.TrimSpace(h.Get(HdrStorageBucket)),
		prefix:          prefix,
		accessKeyID:     strings.TrimSpace(h.Get(HdrStorageAccessKey)),
		secretAccessKey: strings.TrimSpace(h.Get(HdrStorageSecretKey)),
		sessionToken:    strings.TrimSpace(h.Get(HdrStorageSession)),
		httpClient:      http.DefaultClient,
	}, true
}

// TalkScopedName returns the object name (relative to a gateway client's
// "<prefix>/talk" prefix) for a given (accountID, name) pair. Each segment is
// sanitized so a caller-controlled value cannot inject path separators and
// escape Talk's key space. When accountID is empty the account segment is
// omitted (a Talk-level shared object).
func TalkScopedName(accountID, name string) string {
	var parts []string
	if accountID != "" {
		parts = append(parts, sanitizeKeySegment(accountID))
	}
	parts = append(parts, sanitizeKeySegment(name))
	return strings.Join(parts, "/")
}
