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
	"crypto/subtle"
	"net"
	"net/http"
	"net/url"
	"os"
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

	// HdrStorageBrokerAuth carries the shared broker secret the Vulos OS gateway
	// presents to prove it injected the storage-seam headers. The seam headers
	// above are trusted ONLY when this header matches EnvStorageBrokerSecret via
	// a constant-time compare (see brokerAuthorized). Mirrors lilmail's
	// X-Vulos-Broker-Auth gate.
	HdrStorageBrokerAuth = "X-Vulos-Storage-Broker-Auth"
)

// EnvStorageBrokerSecret is the env var that gates the whole storage-seam path.
// When empty, the X-Vulos-Storage-* headers are NEVER trusted (standalone/OSS
// behaviour: env S3 or local filesystem). The Vulos OS gateway sets this same
// secret on the box and injects it as HdrStorageBrokerAuth on every request.
const EnvStorageBrokerSecret = "VULOS_STORAGE_BROKER_SECRET"

// TalkPrefixSpace is the per-product key space Talk owns inside the shared
// per-user bucket. All Talk blobs live under "<injected-prefix>/talk/".
const TalkPrefixSpace = "talk"

// brokerAuthorized reports whether the storage-seam headers on this request may
// be trusted. The gate is closed (false) unless EnvStorageBrokerSecret is set
// AND the request presents a matching HdrStorageBrokerAuth header, compared in
// constant time. When the env secret is unset (standalone/OSS) the seam is
// always ignored, so a client cannot inject storage credentials by spoofing the
// X-Vulos-Storage-* headers.
func brokerAuthorized(h http.Header) bool {
	secret := strings.TrimSpace(os.Getenv(EnvStorageBrokerSecret))
	if secret == "" {
		return false // gate disabled — never trust seam headers
	}
	presented := h.Get(HdrStorageBrokerAuth)
	if presented == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(presented), []byte(secret)) == 1
}

// endpointAllowed reports whether the injected endpoint is safe to use. HTTPS
// endpoints are always allowed; plaintext HTTP is allowed ONLY when the host is
// a loopback or private-network address (so an on-box MinIO reachable over http
// works), and rejected for any public host so a compromised/forged endpoint
// header cannot exfiltrate credentials over the network in the clear.
func endpointAllowed(endpoint string) bool {
	u, err := url.Parse(endpoint)
	if err != nil {
		return false
	}
	switch strings.ToLower(u.Scheme) {
	case "https":
		return true
	case "http":
		return isPrivateHost(u.Hostname())
	default:
		return false
	}
}

// isPrivateHost reports whether host is a loopback or private-network host. It
// accepts "localhost", IP literals in loopback/private/link-local/unspecified
// ranges, and single-label hostnames (e.g. a container/service name like
// "minio") which cannot be public FQDNs. Any dotted public hostname is rejected.
func isPrivateHost(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback() || ip.IsPrivate() ||
			ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() ||
			ip.IsUnspecified()
	}
	// A single-label hostname (no dot) is an internal service/container name,
	// not a public FQDN.
	return !strings.Contains(host, ".")
}

// NewGatewayS3Client builds a per-request S3 client from the storage-seam
// headers injected by the Vulos OS gateway. The returned bool is false when the
// request is not behind the gateway: either the broker-auth gate is closed (env
// secret unset or HdrStorageBrokerAuth missing/mismatched), the storage endpoint
// header is absent, or the endpoint is unsafe (non-loopback plaintext http). In
// every such case callers must fall back to standalone storage.
//
// The client's prefix is "<X-Vulos-Storage-Prefix>/talk" so every object it
// writes is confined to Talk's key space within the user's shared bucket.
func NewGatewayS3Client(h http.Header) (*OfficeS3Client, bool) {
	// Broker-auth gate: ignore the seam headers entirely unless the request
	// proves it came through the trusted Vulos OS gateway.
	if !brokerAuthorized(h) {
		return nil, false
	}

	endpoint := strings.TrimSpace(h.Get(HdrStorageEndpoint))
	if endpoint == "" {
		return nil, false
	}
	// Endpoint safety: reject plaintext http to arbitrary external hosts.
	if !endpointAllowed(endpoint) {
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
