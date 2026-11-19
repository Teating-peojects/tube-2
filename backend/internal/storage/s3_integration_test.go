package storage

import (
	"bytes"
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

// Integration tests of the S3 client against a real S3-compatible server (MinIO in CI, any other for local runs).
// They are skipped unless TEST_S3_ENDPOINT is set:
//
//	TEST_S3_ENDPOINT=http://127.0.0.1:9000 TEST_S3_ACCESS_KEY=... TEST_S3_SECRET_KEY=... TEST_S3_BUCKET=redxtube \
//	  go test ./internal/storage -run TestS3 -v
//
// The bucket must exist. Every test works under its own random prefix and removes it afterwards.

func s3Config(t *testing.T) Config {
	t.Helper()
	ep := os.Getenv("TEST_S3_ENDPOINT")
	if ep == "" {
		t.Skip("TEST_S3_ENDPOINT not set")
	}
	get := func(k, def string) string {
		if v := os.Getenv(k); v != "" {
			return v
		}
		return def
	}
	return Config{Endpoint: ep, Region: get("TEST_S3_REGION", "us-east-1"), AccessKey: get("TEST_S3_ACCESS_KEY", "minioadmin"),
		SecretKey: get("TEST_S3_SECRET_KEY", "minioadmin"), Bucket: get("TEST_S3_BUCKET", "redxtube"), PathStyle: true}
}

func s3Client(t *testing.T) (*Client, string) {
	t.Helper()
	c, err := New(s3Config(t))
	if err != nil {
		t.Fatal(err)
	}
	var rnd [6]byte
	_, _ = rand.Read(rnd[:])
	prefix := fmt.Sprintf("itest/%x/", rnd)
	t.Cleanup(func() { _, _ = c.DeletePrefix(context.Background(), prefix) })
	return c, prefix
}

// putPresigned does what the browser does: a plain PUT to the presigned URL, nothing else.
func putPresigned(t *testing.T, url string, body []byte) (*http.Response, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPut, url, bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	return resp, string(b)
}

func randomBytes(n int) []byte {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return b
}

func TestS3PingChecksTheBucket(t *testing.T) {
	c, _ := s3Client(t)
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("Ping of the configured bucket: %v", err)
	}
	cfg := s3Config(t)
	cfg.Bucket = "no-such-bucket-" + fmt.Sprint(time.Now().UnixNano())
	missing, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := missing.Ping(context.Background()); err == nil {
		t.Fatal("Ping of a missing bucket must fail")
	}
}

func TestS3MultipartLifecycle(t *testing.T) {
	ctx := context.Background()
	c, prefix := s3Client(t)
	key := prefix + "original.mp4"
	sizes := []int{5 << 20, 5 << 20, 1<<20 + 17} // S3 minimum part size, except the last
	data := [][]byte{randomBytes(sizes[0]), randomBytes(sizes[1]), randomBytes(sizes[2])}

	id, err := c.CreateMultipart(ctx, key, "video/mp4")
	if err != nil || id == "" {
		t.Fatalf("CreateMultipart: %q %v", id, err)
	}
	etags := make([]string, len(data))
	for i, d := range data {
		resp, body := putPresigned(t, c.PresignUploadPart(key, id, i+1, time.Hour), d)
		if resp.StatusCode != 200 {
			t.Fatalf("presigned PUT of part %d: %d %s", i+1, resp.StatusCode, body)
		}
		etags[i] = strings.Trim(resp.Header.Get("ETag"), `"`)
		if etags[i] == "" {
			t.Fatalf("part %d: the response has no ETag, the browser could not complete the upload", i+1)
		}
	}

	parts, err := c.ListParts(ctx, key, id)
	if err != nil || len(parts) != 3 {
		t.Fatalf("ListParts: %v %v", parts, err)
	}
	for i, p := range parts {
		// The service compares what the browser sends (ETag without quotes) with what ListParts reports.
		if p.Number != i+1 || p.Size != int64(sizes[i]) || strings.Trim(p.ETag, `"`) != etags[i] {
			t.Errorf("part %d: listed %+v, uploaded size %d etag %s", i+1, p, sizes[i], etags[i])
		}
	}

	if err := c.CompleteMultipart(ctx, key, id, parts); err != nil {
		t.Fatalf("CompleteMultipart: %v", err)
	}
	info, err := c.Head(ctx, key)
	total := int64(sizes[0] + sizes[1] + sizes[2])
	if err != nil || info.Size != total {
		t.Fatalf("Head after completion: %+v %v, want size %d", info, err, total)
	}
	head, err := c.GetRange(ctx, key, 0, 63)
	if err != nil || !bytes.Equal(head, data[0][:64]) {
		t.Fatalf("GetRange(0,63): %d bytes, equal=%v, err=%v", len(head), bytes.Equal(head, data[0][:min(64, len(head))]), err)
	}
	var out bytes.Buffer
	if n, err := c.Download(ctx, key, &out); err != nil || n != total || !bytes.Equal(out.Bytes()[sizes[0]:sizes[0]+16], data[1][:16]) {
		t.Fatalf("Download: %d bytes, err=%v", n, err)
	}

	if err := c.Delete(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := c.Head(ctx, key); !errors.Is(err, ErrNotFound) {
		t.Fatalf("Head after Delete: %v, want ErrNotFound", err)
	}
	if err := c.Delete(ctx, key); err != nil {
		t.Fatalf("Delete of a missing object must succeed (the cleanup relies on it): %v", err)
	}
}

func TestS3CompleteRefusesAWrongETag(t *testing.T) {
	ctx := context.Background()
	c, prefix := s3Client(t)
	key := prefix + "bad.mp4"
	id, err := c.CreateMultipart(ctx, key, "video/mp4")
	if err != nil {
		t.Fatal(err)
	}
	if resp, body := putPresigned(t, c.PresignUploadPart(key, id, 1, time.Hour), randomBytes(1<<20)); resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	if err := c.CompleteMultipart(ctx, key, id, []Part{{Number: 1, ETag: `"00000000000000000000000000000000"`}}); err == nil {
		t.Fatal("completing with an ETag that does not match must fail")
	}
	_ = c.AbortMultipart(ctx, key, id)
}

func TestS3AbortIsIdempotent(t *testing.T) {
	ctx := context.Background()
	c, prefix := s3Client(t)
	key := prefix + "abort.mp4"
	id, err := c.CreateMultipart(ctx, key, "video/mp4")
	if err != nil {
		t.Fatal(err)
	}
	if resp, body := putPresigned(t, c.PresignUploadPart(key, id, 1, time.Hour), randomBytes(1<<20)); resp.StatusCode != 200 {
		t.Fatalf("%d %s", resp.StatusCode, body)
	}
	if err := c.AbortMultipart(ctx, key, id); err != nil {
		t.Fatalf("first abort: %v", err)
	}
	// The janitor, the storage sweeper and a user's cancel may all abort the same upload: S3 and MinIO answer the
	// later ones with 404 NoSuchUpload, which must count as done (otherwise the sweeper retries forever).
	if err := c.AbortMultipart(ctx, key, id); err != nil {
		t.Fatalf("second abort must be a success: %v", err)
	}
	if _, err := c.ListParts(ctx, key, id); err == nil {
		t.Error("ListParts of an aborted upload must fail")
	}
}

// After an abort the part URLs of that upload must stop working with a 4xx (S3 and MinIO: 404 NoSuchUpload). The
// browser does not retry 4xx answers, it recovers through the retry endpoint.
func TestS3AbortedUploadRejectsParts(t *testing.T) {
	ctx := context.Background()
	c, prefix := s3Client(t)
	key := prefix + "aborted.mp4"
	id, err := c.CreateMultipart(ctx, key, "video/mp4")
	if err != nil {
		t.Fatal(err)
	}
	url := c.PresignUploadPart(key, id, 1, time.Hour)
	if err := c.AbortMultipart(ctx, key, id); err != nil {
		t.Fatal(err)
	}
	if resp, body := putPresigned(t, url, randomBytes(1<<20)); resp.StatusCode != 404 {
		t.Errorf("a part URL of an aborted upload answered %d (%s), the browser expects 404", resp.StatusCode, body)
	}
}

// The browser treats a 403 from storage as "the presigned URL expired" (web/src/lib/uploader.ts: no retry of 4xx,
// recovery through POST /upload/{id}/retry). This is the test that proves the assumption on a real server.
func TestS3ExpiredPresignedURLAnswers403(t *testing.T) {
	ctx := context.Background()
	c, prefix := s3Client(t)
	key := prefix + "expired.mp4"
	id, err := c.CreateMultipart(ctx, key, "video/mp4")
	if err != nil {
		t.Fatal(err)
	}
	defer c.AbortMultipart(ctx, key, id)
	url := c.PresignUploadPart(key, id, 1, time.Second)
	time.Sleep(2500 * time.Millisecond)
	if resp, body := putPresigned(t, url, randomBytes(1<<20)); resp.StatusCode != 403 {
		t.Fatalf("an expired presigned URL answered %d (%s); the frontend expects 403", resp.StatusCode, body)
	}
}

func TestS3RejectsWrongCredentials(t *testing.T) {
	cfg := s3Config(t)
	cfg.SecretKey += "-wrong"
	bad, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	// Only a real 4xx answer from the server counts: a refused connection would pass a plain "err != nil" check.
	_, err = bad.CreateMultipart(context.Background(), "itest/x.mp4", "video/mp4")
	var se *Error
	if !errors.As(err, &se) {
		t.Fatalf("expected an HTTP answer from the server, got: %v", err)
	}
	if se.Status != 403 {
		t.Fatalf("a wrong secret key answered %d (%s), want 403 SignatureDoesNotMatch", se.Status, se.Code)
	}
}

func TestS3ObjectsAndPrefixDeletion(t *testing.T) {
	ctx := context.Background()
	c, prefix := s3Client(t)
	for _, k := range []string{"a/1.jpg", "a/2.jpg", "a/sub/3.vtt", "b/1.jpg"} {
		if err := c.PutBytes(ctx, prefix+k, "image/jpeg", "public, max-age=60", []byte("x-"+k)); err != nil {
			t.Fatalf("PutBytes %s: %v", k, err)
		}
	}
	info, err := c.Head(ctx, prefix+"a/1.jpg")
	if err != nil || info.Size != int64(len("x-a/1.jpg")) || info.ContentType != "image/jpeg" {
		t.Fatalf("Head: %+v %v", info, err)
	}
	if n, err := c.DeletePrefix(ctx, prefix+"a/"); err != nil || n != 3 {
		t.Fatalf("DeletePrefix a/: %d %v, want 3", n, err)
	}
	if _, err := c.Head(ctx, prefix+"a/sub/3.vtt"); !errors.Is(err, ErrNotFound) {
		t.Errorf("nested object survived: %v", err)
	}
	if _, err := c.Head(ctx, prefix+"b/1.jpg"); err != nil {
		t.Errorf("another prefix must be untouched: %v", err)
	}
	if n, err := c.DeletePrefix(ctx, prefix+"a/"); err != nil || n != 0 {
		t.Errorf("DeletePrefix is idempotent: %d %v", n, err)
	}
}
