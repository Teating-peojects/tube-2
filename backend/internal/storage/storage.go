// Package storage is a small S3-compatible object storage client (AWS S3,
// Cloudflare R2, MinIO, Backblaze B2...) written against the standard library
// only, so the API, the worker and the edge need no extra dependency.
//
// It covers exactly what the platform needs: presigned multipart uploads for
// the browser, completion/verification of those uploads, ranged reads, puts
// and deletes for the worker.
package storage

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Config describes one bucket. Endpoint is what servers inside your network
// use; PublicEndpoint is what the BROWSER can reach (they differ in Docker:
// http://minio:9000 vs http://localhost:9000). Presigned URLs are signed for
// the public host, so the two must point at the same storage.
type Config struct {
	Endpoint       string
	PublicEndpoint string // defaults to Endpoint
	Region         string // defaults to us-east-1 (MinIO, R2 use "auto"/any)
	AccessKey      string
	SecretKey      string
	Bucket         string
	PathStyle      bool // true for MinIO / most self-hosted; false = virtual-hosted (AWS S3)
	HTTPClient     *http.Client
}

var (
	// ErrNotFound is returned when the object does not exist.
	ErrNotFound = errors.New("storage: object not found")
)

// Error is an error response from the storage service.
type Error struct {
	Status  int
	Code    string
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("storage: %d %s: %s", e.Status, e.Code, e.Message)
}

// Part is one uploaded multipart part.
type Part struct {
	Number int
	ETag   string // as returned by the service (usually quoted)
	Size   int64
}

// ObjectInfo is the result of a HEAD request.
type ObjectInfo struct {
	Size        int64
	ContentType string
}

// API is what services depend on (and what tests fake).
type API interface {
	CreateMultipart(ctx context.Context, key, contentType string) (uploadID string, err error)
	PresignUploadPart(key, uploadID string, partNumber int, ttl time.Duration) string
	ListParts(ctx context.Context, key, uploadID string) ([]Part, error)
	CompleteMultipart(ctx context.Context, key, uploadID string, parts []Part) error
	AbortMultipart(ctx context.Context, key, uploadID string) error
	PresignPut(key, contentType string, size int64, ttl time.Duration) string
	Head(ctx context.Context, key string) (ObjectInfo, error)
	GetRange(ctx context.Context, key string, start, end int64) ([]byte, error)
	Download(ctx context.Context, key string, w io.Writer) (int64, error)
	Put(ctx context.Context, key, contentType, cacheControl string, r io.Reader, size int64) error
	PutBytes(ctx context.Context, key, contentType, cacheControl string, b []byte) error
	Delete(ctx context.Context, key string) error
	DeletePrefix(ctx context.Context, prefix string) (int, error)
}

// Client implements API.
type Client struct {
	cfg  Config
	in   *url.URL
	pub  *url.URL
	http *http.Client
	now  func() time.Time
}

var _ API = (*Client)(nil)

func parseEndpoint(raw string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimRight(raw, "/"))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || (u.Path != "" && u.Path != "/") {
		return nil, fmt.Errorf("storage: endpoint %q must look like https://host[:port]", raw)
	}
	// Browsers omit default ports from the Host header; the signature must match.
	if (u.Scheme == "https" && u.Port() == "443") || (u.Scheme == "http" && u.Port() == "80") {
		u.Host = u.Hostname()
	}
	return u, nil
}

// New validates the configuration and returns a client.
func New(cfg Config) (*Client, error) {
	if cfg.Bucket == "" || cfg.AccessKey == "" || cfg.SecretKey == "" {
		return nil, errors.New("storage: bucket, access key and secret key are required")
	}
	in, err := parseEndpoint(cfg.Endpoint)
	if err != nil {
		return nil, err
	}
	pub := in
	if cfg.PublicEndpoint != "" {
		if pub, err = parseEndpoint(cfg.PublicEndpoint); err != nil {
			return nil, err
		}
	}
	if cfg.Region == "" {
		cfg.Region = "us-east-1"
	}
	h := cfg.HTTPClient
	if h == nil {
		h = &http.Client{Timeout: 0} // per-request deadlines come from contexts
	}
	return &Client{cfg: cfg, in: in, pub: pub, http: h, now: time.Now}, nil
}

// target returns scheme://host and the escaped path for a key on the given base.
func (c *Client) target(base *url.URL, key string) (root, host, escPath string) {
	if c.cfg.PathStyle {
		p := "/" + uriEncode(c.cfg.Bucket, false)
		if key != "" {
			p += "/" + uriEncode(key, false)
		}
		return base.Scheme + "://" + base.Host, base.Host, p
	}
	host = c.cfg.Bucket + "." + base.Host
	p := "/"
	if key != "" {
		p += uriEncode(key, false)
	}
	return base.Scheme + "://" + host, host, p
}

func (c *Client) newRequest(ctx context.Context, method, key string, q url.Values, body io.Reader) (*http.Request, error) {
	root, _, p := c.target(c.in, key)
	u := root + p
	if len(q) > 0 {
		u += "?" + canonicalQuery(q)
	}
	return http.NewRequestWithContext(ctx, method, u, body)
}

func parseError(resp *http.Response) error {
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	e := &Error{Status: resp.StatusCode}
	var x struct {
		Code    string `xml:"Code"`
		Message string `xml:"Message"`
	}
	if xml.Unmarshal(b, &x) == nil {
		e.Code, e.Message = x.Code, x.Message
	}
	if resp.StatusCode == http.StatusNotFound && (e.Code == "" || e.Code == "NoSuchKey" || e.Code == "NotFound") {
		return ErrNotFound
	}
	return e
}

// do sends a signed request. body may be nil. size is the exact body length.
// payloadHash is the hex SHA-256 of the body, or "UNSIGNED-PAYLOAD".
func (c *Client) do(ctx context.Context, method, key string, q url.Values, headers map[string]string, body io.Reader, size int64, payloadHash string) (*http.Response, error) {
	req, err := c.newRequest(ctx, method, key, q, body)
	if err != nil {
		return nil, err
	}
	if body != nil {
		req.ContentLength = size
	}
	var extra []string
	for k, v := range headers {
		k = strings.ToLower(k)
		req.Header.Set(k, v)
		if k == "content-type" || k == "range" {
			extra = append(extra, k) // sign what matters; others (cache-control...) are not required to be signed
		}
	}
	signHeaders(req, c.cfg.AccessKey, c.cfg.SecretKey, c.cfg.Region, payloadHash, c.now(), extra...)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 300 {
		return nil, parseError(resp)
	}
	return resp, nil
}

// doBytes retries transient failures (5xx, network) because the body can be replayed.
func (c *Client) doBytes(ctx context.Context, method, key string, q url.Values, headers map[string]string, body []byte) (*http.Response, error) {
	hash := sha256Hex(body)
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt) * 300 * time.Millisecond):
			}
		}
		var r io.Reader
		if body != nil {
			r = bytes.NewReader(body)
		}
		resp, err := c.do(ctx, method, key, q, headers, r, int64(len(body)), hash)
		if err == nil {
			return resp, nil
		}
		lastErr = err
		var se *Error
		if errors.As(err, &se) && se.Status < 500 {
			return nil, err
		}
		if errors.Is(err, ErrNotFound) || ctx.Err() != nil {
			return nil, err
		}
	}
	return nil, lastErr
}

// ---------- multipart ----------

func (c *Client) CreateMultipart(ctx context.Context, key, contentType string) (string, error) {
	resp, err := c.doBytes(ctx, http.MethodPost, key, url.Values{"uploads": {""}}, map[string]string{"content-type": contentType}, nil)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	var out struct {
		UploadID string `xml:"UploadId"`
	}
	if err := xml.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&out); err != nil || out.UploadID == "" {
		return "", errors.New("storage: unexpected CreateMultipartUpload response")
	}
	return out.UploadID, nil
}

// PresignUploadPart returns a URL the browser can PUT one part to. Only the
// host header is signed, so the browser needs no special headers.
func (c *Client) PresignUploadPart(key, uploadID string, partNumber int, ttl time.Duration) string {
	root, host, p := c.target(c.pub, key)
	q := url.Values{"partNumber": {strconv.Itoa(partNumber)}, "uploadId": {uploadID}}
	return presign(http.MethodPut, root, host, p, q, nil, c.cfg.AccessKey, c.cfg.SecretKey, c.cfg.Region, ttl, c.now())
}

// PresignPut returns a URL for a single-object PUT. Content-Type and
// Content-Length are signed: the browser must send exactly these values.
func (c *Client) PresignPut(key, contentType string, size int64, ttl time.Duration) string {
	root, host, p := c.target(c.pub, key)
	hdr := map[string]string{"content-type": contentType, "content-length": strconv.FormatInt(size, 10)}
	return presign(http.MethodPut, root, host, p, nil, hdr, c.cfg.AccessKey, c.cfg.SecretKey, c.cfg.Region, ttl, c.now())
}

func (c *Client) ListParts(ctx context.Context, key, uploadID string) ([]Part, error) {
	var out []Part
	marker := 0
	for page := 0; page < 20; page++ { // 20 x 1000 covers the 10,000-part maximum
		q := url.Values{"uploadId": {uploadID}, "max-parts": {"1000"}}
		if marker > 0 {
			q.Set("part-number-marker", strconv.Itoa(marker))
		}
		resp, err := c.doBytes(ctx, http.MethodGet, key, q, nil, nil)
		if err != nil {
			return nil, err
		}
		var res struct {
			IsTruncated          bool `xml:"IsTruncated"`
			NextPartNumberMarker int  `xml:"NextPartNumberMarker"`
			Parts                []struct {
				PartNumber int    `xml:"PartNumber"`
				ETag       string `xml:"ETag"`
				Size       int64  `xml:"Size"`
			} `xml:"Part"`
		}
		err = xml.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(&res)
		resp.Body.Close()
		if err != nil {
			return nil, fmt.Errorf("storage: parse ListParts: %w", err)
		}
		for _, p := range res.Parts {
			out = append(out, Part{Number: p.PartNumber, ETag: p.ETag, Size: p.Size})
		}
		if !res.IsTruncated {
			sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
			return out, nil
		}
		marker = res.NextPartNumberMarker
	}
	return nil, errors.New("storage: too many parts")
}

func (c *Client) CompleteMultipart(ctx context.Context, key, uploadID string, parts []Part) error {
	type xp struct {
		PartNumber int    `xml:"PartNumber"`
		ETag       string `xml:"ETag"`
	}
	body := struct {
		XMLName xml.Name `xml:"CompleteMultipartUpload"`
		Parts   []xp     `xml:"Part"`
	}{}
	for _, p := range parts {
		body.Parts = append(body.Parts, xp{p.Number, p.ETag})
	}
	b, err := xml.Marshal(body)
	if err != nil {
		return err
	}
	resp, err := c.doBytes(ctx, http.MethodPost, key, url.Values{"uploadId": {uploadID}}, map[string]string{"content-type": "application/xml"}, b)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	// S3 can answer 200 and still report a failure in the body.
	raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if bytes.Contains(raw, []byte("<Error>")) {
		var x struct {
			Code    string `xml:"Code"`
			Message string `xml:"Message"`
		}
		_ = xml.Unmarshal(raw, &x)
		return &Error{Status: 500, Code: x.Code, Message: x.Message}
	}
	return nil
}

func (c *Client) AbortMultipart(ctx context.Context, key, uploadID string) error {
	resp, err := c.doBytes(ctx, http.MethodDelete, key, url.Values{"uploadId": {uploadID}}, nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil // already gone
	}
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// ---------- objects ----------

func (c *Client) Head(ctx context.Context, key string) (ObjectInfo, error) {
	resp, err := c.doBytes(ctx, http.MethodHead, key, nil, nil, nil)
	if err != nil {
		return ObjectInfo{}, err
	}
	resp.Body.Close()
	return ObjectInfo{Size: resp.ContentLength, ContentType: resp.Header.Get("Content-Type")}, nil
}

// GetRange reads bytes start..end (inclusive). Used to sniff file signatures
// without downloading the object.
func (c *Client) GetRange(ctx context.Context, key string, start, end int64) ([]byte, error) {
	resp, err := c.doBytes(ctx, http.MethodGet, key, nil, map[string]string{"range": fmt.Sprintf("bytes=%d-%d", start, end)}, nil)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(io.LimitReader(resp.Body, end-start+1))
}

// Download streams the object into w.
func (c *Client) Download(ctx context.Context, key string, w io.Writer) (int64, error) {
	resp, err := c.do(ctx, http.MethodGet, key, nil, nil, nil, 0, sha256Hex(nil))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	return io.Copy(w, resp.Body)
}

// Put streams r (exactly size bytes) to key without buffering it. The payload
// is sent unsigned (UNSIGNED-PAYLOAD): use https or a private network.
func (c *Client) Put(ctx context.Context, key, contentType, cacheControl string, r io.Reader, size int64) error {
	h := map[string]string{"content-type": contentType}
	if cacheControl != "" {
		h["cache-control"] = cacheControl
	}
	resp, err := c.do(ctx, http.MethodPut, key, nil, h, r, size, unsignedPayload)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// PutBytes stores a small object with a signed payload hash.
func (c *Client) PutBytes(ctx context.Context, key, contentType, cacheControl string, b []byte) error {
	h := map[string]string{"content-type": contentType}
	if cacheControl != "" {
		h["cache-control"] = cacheControl
	}
	resp, err := c.doBytes(ctx, http.MethodPut, key, nil, h, b)
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

func (c *Client) Delete(ctx context.Context, key string) error {
	resp, err := c.doBytes(ctx, http.MethodDelete, key, nil, nil, nil)
	if errors.Is(err, ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	resp.Body.Close()
	return nil
}

// DeletePrefix removes every object under prefix and returns how many it deleted.
// The prefix must end with "/" so "videos/ab" can never match "videos/abc".
func (c *Client) DeletePrefix(ctx context.Context, prefix string) (int, error) {
	if !strings.HasSuffix(prefix, "/") || len(prefix) < 3 {
		return 0, errors.New("storage: refusing to delete a short or non-directory prefix")
	}
	deleted := 0
	token := ""
	for page := 0; page < 1000; page++ {
		q := url.Values{"list-type": {"2"}, "prefix": {prefix}, "max-keys": {"1000"}}
		if token != "" {
			q.Set("continuation-token", token)
		}
		resp, err := c.doBytes(ctx, http.MethodGet, "", q, nil, nil)
		if err != nil {
			return deleted, err
		}
		var res struct {
			IsTruncated           bool   `xml:"IsTruncated"`
			NextContinuationToken string `xml:"NextContinuationToken"`
			Contents              []struct {
				Key string `xml:"Key"`
			} `xml:"Contents"`
		}
		err = xml.NewDecoder(io.LimitReader(resp.Body, 16<<20)).Decode(&res)
		resp.Body.Close()
		if err != nil {
			return deleted, fmt.Errorf("storage: parse ListObjectsV2: %w", err)
		}
		for _, o := range res.Contents {
			if !strings.HasPrefix(o.Key, prefix) {
				continue // defensive: never delete outside the prefix
			}
			if err := c.Delete(ctx, o.Key); err != nil {
				return deleted, err
			}
			deleted++
		}
		if !res.IsTruncated || res.NextContinuationToken == "" {
			return deleted, nil
		}
		token = res.NextContinuationToken
	}
	return deleted, errors.New("storage: prefix too large")
}

// ---------- helpers ----------

// NormalizeETag strips quotes and whitespace so ETags from the browser and from
// ListParts compare equal.
func NormalizeETag(s string) string { return strings.Trim(strings.TrimSpace(s), `"`) }
