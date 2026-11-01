package storage

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeS3 is a tiny in-process S3 that records requests and answers just enough XML.
type fakeS3 struct {
	mu       sync.Mutex
	requests []string
	bodies   map[string]string
	handler  func(w http.ResponseWriter, r *http.Request)
}

func newFake(t *testing.T, h func(w http.ResponseWriter, r *http.Request)) (*fakeS3, *Client) {
	t.Helper()
	f := &fakeS3{bodies: map[string]string{}, handler: h}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		f.mu.Lock()
		f.requests = append(f.requests, r.Method+" "+r.URL.RequestURI())
		f.bodies[r.Method+" "+r.URL.RequestURI()] = string(b)
		f.mu.Unlock()
		if !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 Credential=AK/") {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		f.handler(w, r)
	}))
	t.Cleanup(srv.Close)
	c, err := New(Config{Endpoint: srv.URL, PublicEndpoint: "https://cdn-upload.example.com", AccessKey: "AK", SecretKey: "SK", Bucket: "bkt", PathStyle: true})
	if err != nil {
		t.Fatal(err)
	}
	return f, c
}

func TestMultipartLifecycle(t *testing.T) {
	f, c := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		switch {
		case r.Method == "POST" && q.Has("uploads"):
			io.WriteString(w, `<InitiateMultipartUploadResult><UploadId>UP1</UploadId></InitiateMultipartUploadResult>`)
		case r.Method == "GET" && q.Get("uploadId") == "UP1" && q.Get("part-number-marker") == "":
			io.WriteString(w, `<ListPartsResult><IsTruncated>true</IsTruncated><NextPartNumberMarker>1</NextPartNumberMarker>
<Part><PartNumber>1</PartNumber><ETag>&quot;e1&quot;</ETag><Size>8</Size></Part></ListPartsResult>`)
		case r.Method == "GET" && q.Get("part-number-marker") == "1":
			io.WriteString(w, `<ListPartsResult><IsTruncated>false</IsTruncated>
<Part><PartNumber>2</PartNumber><ETag>&quot;e2&quot;</ETag><Size>3</Size></Part></ListPartsResult>`)
		case r.Method == "POST" && q.Get("uploadId") == "UP1":
			io.WriteString(w, `<CompleteMultipartUploadResult><Key>k</Key></CompleteMultipartUploadResult>`)
		case r.Method == "DELETE" && q.Get("uploadId") == "UP1":
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(500)
		}
	})
	ctx := context.Background()
	id, err := c.CreateMultipart(ctx, "src/u/v/source.mp4", "video/mp4")
	if err != nil || id != "UP1" {
		t.Fatalf("CreateMultipart = %q, %v", id, err)
	}
	parts, err := c.ListParts(ctx, "src/u/v/source.mp4", id)
	if err != nil || len(parts) != 2 || parts[0].Number != 1 || parts[1].Number != 2 || parts[0].Size != 8 || NormalizeETag(parts[0].ETag) != "e1" {
		t.Fatalf("ListParts = %+v, %v", parts, err)
	}
	if err := c.CompleteMultipart(ctx, "src/u/v/source.mp4", id, parts); err != nil {
		t.Fatal(err)
	}
	body := f.bodies["POST /bkt/src/u/v/source.mp4?uploadId=UP1"]
	if !strings.Contains(body, "<CompleteMultipartUpload>") || !strings.Contains(body, "<PartNumber>2</PartNumber>") {
		t.Fatalf("complete body = %q", body)
	}
	if err := c.AbortMultipart(ctx, "src/u/v/source.mp4", id); err != nil {
		t.Fatal(err)
	}
}

func TestCompleteMultipartDetectsErrorInsideOK(t *testing.T) {
	_, c := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `<Error><Code>InternalError</Code><Message>try again</Message></Error>`)
	})
	err := c.CompleteMultipart(context.Background(), "k", "u", []Part{{Number: 1, ETag: "x"}})
	var se *Error
	if !errors.As(err, &se) || se.Code != "InternalError" {
		t.Fatalf("want InternalError, got %v", err)
	}
}

func TestHeadNotFoundAndErrors(t *testing.T) {
	_, c := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "missing") {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if strings.Contains(r.URL.Path, "denied") {
			w.WriteHeader(http.StatusForbidden)
			io.WriteString(w, `<Error><Code>AccessDenied</Code><Message>no</Message></Error>`)
			return
		}
		w.Header().Set("Content-Type", "video/mp4")
		w.Header().Set("Content-Length", "1234")
	})
	ctx := context.Background()
	if _, err := c.Head(ctx, "missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
	if _, err := c.GetRange(ctx, "denied", 0, 3); err == nil || errors.Is(err, ErrNotFound) {
		t.Fatalf("want an access error, got %v", err)
	}
	info, err := c.Head(ctx, "ok")
	if err != nil || info.Size != 1234 || info.ContentType != "video/mp4" {
		t.Fatalf("Head = %+v, %v", info, err)
	}
}

func TestRetriesTransientServerErrors(t *testing.T) {
	calls := 0
	_, c := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		calls++
		if calls < 3 {
			w.WriteHeader(http.StatusServiceUnavailable)
			return
		}
		w.Header().Set("Content-Length", "5")
	})
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if _, err := c.Head(ctx, "k"); err != nil || calls != 3 {
		t.Fatalf("calls=%d err=%v", calls, err)
	}
}

func TestDeletePrefixIsSafe(t *testing.T) {
	var deleted []string
	_, c := newFake(t, func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "GET":
			io.WriteString(w, `<ListBucketResult><IsTruncated>false</IsTruncated>
<Contents><Key>hls/abc/master.m3u8</Key></Contents><Contents><Key>hls/abc/720p/s1.ts</Key></Contents>
<Contents><Key>hls/abcd/evil.ts</Key></Contents></ListBucketResult>`)
		case "DELETE":
			deleted = append(deleted, r.URL.Path)
			w.WriteHeader(204)
		}
	})
	ctx := context.Background()
	for _, bad := range []string{"", "a", "hls/abc", "hls"} {
		if _, err := c.DeletePrefix(ctx, bad); err == nil {
			t.Errorf("prefix %q must be refused", bad)
		}
	}
	n, err := c.DeletePrefix(ctx, "hls/abc/")
	if err != nil || n != 2 {
		t.Fatalf("deleted %d, err %v", n, err)
	}
	for _, d := range deleted {
		if strings.Contains(d, "abcd") {
			t.Fatalf("deleted outside the prefix: %s", d)
		}
	}
}

func TestPresignedURLsUsePublicEndpoint(t *testing.T) {
	_, c := newFake(t, func(w http.ResponseWriter, r *http.Request) {})
	u := c.PresignUploadPart("src/u/v/source.mp4", "UP 1", 3, time.Hour)
	if !strings.HasPrefix(u, "https://cdn-upload.example.com/bkt/src/u/v/source.mp4?") {
		t.Fatalf("unexpected URL %s", u)
	}
	for _, want := range []string{"partNumber=3", "uploadId=UP%201", "X-Amz-Expires=3600", "X-Amz-Signature=", "X-Amz-SignedHeaders=host"} {
		if !strings.Contains(u, want) {
			t.Errorf("missing %q in %s", want, u)
		}
	}
	p := c.PresignPut("pub/videos/v/thumb.jpg", "image/jpeg", 2048, 15*time.Minute)
	if !strings.Contains(p, "X-Amz-SignedHeaders=content-length%3Bcontent-type%3Bhost") {
		t.Errorf("content-type and content-length must be signed: %s", p)
	}
}

func TestNewValidatesConfig(t *testing.T) {
	bad := []Config{
		{Endpoint: "http://x", AccessKey: "a", SecretKey: "b"},                   // no bucket
		{Endpoint: "ftp://x", AccessKey: "a", SecretKey: "b", Bucket: "c"},       // scheme
		{Endpoint: "http://x/path", AccessKey: "a", SecretKey: "b", Bucket: "c"}, // path
		{Endpoint: "http://x", AccessKey: "", SecretKey: "b", Bucket: "c"},       // no key
		{Endpoint: "http://x", PublicEndpoint: "nope", AccessKey: "a", SecretKey: "b", Bucket: "c"},
	}
	for i, cfg := range bad {
		if _, err := New(cfg); err == nil {
			t.Errorf("case %d: expected an error", i)
		}
	}
	c, err := New(Config{Endpoint: "https://s3.example.com:443", AccessKey: "a", SecretKey: "b", Bucket: "c"})
	if err != nil || c.in.Host != "s3.example.com" {
		t.Fatalf("default port must be stripped: %v %v", c, err)
	}
}
