package storage

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"
)

// AWS Signature Version 4 for S3, standard library only. Two flavours:
//   - header auth (Authorization header) for requests the API/worker make itself;
//   - query auth (presigned URLs) for requests the browser makes straight to storage.
//
// Verified against the official AWS documentation examples (see sigv4_test.go).

const (
	algorithm       = "AWS4-HMAC-SHA256"
	unsignedPayload = "UNSIGNED-PAYLOAD"
	amzDate         = "20060102T150405Z"
	shortDate       = "20060102"
)

func hmacSHA256(key []byte, data string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(data))
	return m.Sum(nil)
}

func sha256Hex(b []byte) string {
	s := sha256.Sum256(b)
	return hex.EncodeToString(s[:])
}

func signingKey(secret, date, region string) []byte {
	k := hmacSHA256([]byte("AWS4"+secret), date)
	k = hmacSHA256(k, region)
	k = hmacSHA256(k, "s3")
	return hmacSHA256(k, "aws4_request")
}

// uriEncode implements the AWS "UriEncode" rule: unreserved characters stay,
// everything else becomes %XX (upper case); "/" is kept only when encodeSlash is false.
func uriEncode(s string, encodeSlash bool) string {
	const hexd = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9', c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		case c == '/' && !encodeSlash:
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hexd[c>>4])
			b.WriteByte(hexd[c&15])
		}
	}
	return b.String()
}

// canonicalQuery sorts parameters by encoded name (then value) as SigV4 requires.
func canonicalQuery(q url.Values) string {
	type kv struct{ k, v string }
	var all []kv
	for k, vs := range q {
		for _, v := range vs {
			all = append(all, kv{uriEncode(k, true), uriEncode(v, true)})
		}
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].k != all[j].k {
			return all[i].k < all[j].k
		}
		return all[i].v < all[j].v
	})
	parts := make([]string, len(all))
	for i, p := range all {
		parts[i] = p.k + "=" + p.v
	}
	return strings.Join(parts, "&")
}

// canonicalHeaders returns the canonical header block and the SignedHeaders list.
// names must be lower case; values are trimmed and inner whitespace collapsed.
func canonicalHeaders(h map[string]string) (block, signed string) {
	names := make([]string, 0, len(h))
	for k := range h {
		names = append(names, strings.ToLower(k))
	}
	sort.Strings(names)
	lower := map[string]string{}
	for k, v := range h {
		lower[strings.ToLower(k)] = strings.Join(strings.Fields(v), " ")
	}
	var b strings.Builder
	for _, n := range names {
		b.WriteString(n + ":" + lower[n] + "\n")
	}
	return b.String(), strings.Join(names, ";")
}

func stringToSign(t time.Time, region, canonicalRequest string) (sts, scope string) {
	scope = t.UTC().Format(shortDate) + "/" + region + "/s3/aws4_request"
	sts = algorithm + "\n" + t.UTC().Format(amzDate) + "\n" + scope + "\n" + sha256Hex([]byte(canonicalRequest))
	return
}

// signHeaders adds the Authorization header to req (header auth). The headers
// host, x-amz-date and x-amz-content-sha256 are set here; extra headers already
// on req are signed only if listed in signExtra (lower-case names).
func signHeaders(req *http.Request, accessKey, secret, region, payloadHash string, t time.Time, signExtra ...string) {
	t = t.UTC()
	req.Header.Set("x-amz-date", t.Format(amzDate))
	req.Header.Set("x-amz-content-sha256", payloadHash)

	hdr := map[string]string{
		"host":                 req.URL.Host,
		"x-amz-date":           t.Format(amzDate),
		"x-amz-content-sha256": payloadHash,
	}
	for _, n := range signExtra {
		hdr[n] = req.Header.Get(n)
	}
	block, signed := canonicalHeaders(hdr)
	cr := strings.Join([]string{req.Method, req.URL.EscapedPath(), canonicalQuery(req.URL.Query()), block, signed, payloadHash}, "\n")
	sts, scope := stringToSign(t, region, cr)
	sig := hex.EncodeToString(hmacSHA256(signingKey(secret, t.Format(shortDate), region), sts))
	req.Header.Set("Authorization", algorithm+" Credential="+accessKey+"/"+scope+",SignedHeaders="+signed+",Signature="+sig)
}

// presign builds a presigned URL. base is scheme://host (no trailing slash),
// escapedPath the already-encoded object path, extra the additional query
// parameters (partNumber, uploadId...), signedHeaders the headers the client
// MUST send with exactly these values (always includes host).
func presign(method, base, host, escapedPath string, extra url.Values, signedHeaders map[string]string,
	accessKey, secret, region string, expires time.Duration, t time.Time) string {
	t = t.UTC()
	hdr := map[string]string{"host": host}
	for k, v := range signedHeaders {
		hdr[strings.ToLower(k)] = v
	}
	block, signed := canonicalHeaders(hdr)

	scope := t.Format(shortDate) + "/" + region + "/s3/aws4_request"
	q := url.Values{}
	for k, vs := range extra {
		q[k] = append([]string(nil), vs...)
	}
	q.Set("X-Amz-Algorithm", algorithm)
	q.Set("X-Amz-Credential", accessKey+"/"+scope)
	q.Set("X-Amz-Date", t.Format(amzDate))
	q.Set("X-Amz-Expires", itoa(int64(expires/time.Second)))
	q.Set("X-Amz-SignedHeaders", signed)

	cq := canonicalQuery(q)
	cr := strings.Join([]string{method, escapedPath, cq, block, signed, unsignedPayload}, "\n")
	sts, _ := stringToSign(t, region, cr)
	sig := hex.EncodeToString(hmacSHA256(signingKey(secret, t.Format(shortDate), region), sts))
	return base + escapedPath + "?" + cq + "&X-Amz-Signature=" + sig
}

func itoa(n int64) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
