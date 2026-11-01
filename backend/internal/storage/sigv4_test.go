package storage

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

// The AWS documentation publishes worked SigV4 examples for S3 with fixed keys and dates.
// Matching them byte for byte is the strongest evidence the signing code is correct.
const (
	exAccess = "AKIAIOSFODNN7EXAMPLE"
	exSecret = "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY"
	exRegion = "us-east-1"
)

var exTime = time.Date(2013, 5, 24, 0, 0, 0, 0, time.UTC)

func TestPresignMatchesAWSExample(t *testing.T) {
	// https://docs.aws.amazon.com/AmazonS3/latest/API/sigv4-query-string-auth.html
	got := presign(http.MethodGet, "https://examplebucket.s3.amazonaws.com", "examplebucket.s3.amazonaws.com", "/test.txt",
		nil, nil, exAccess, exSecret, exRegion, 86400*time.Second, exTime)
	want := "https://examplebucket.s3.amazonaws.com/test.txt?X-Amz-Algorithm=AWS4-HMAC-SHA256" +
		"&X-Amz-Credential=AKIAIOSFODNN7EXAMPLE%2F20130524%2Fus-east-1%2Fs3%2Faws4_request" +
		"&X-Amz-Date=20130524T000000Z&X-Amz-Expires=86400&X-Amz-SignedHeaders=host" +
		"&X-Amz-Signature=aeeed9bbccd4d02ee5c0109b86d86835f995330da4c265957d157751f604d404"
	if got != want {
		t.Fatalf("presigned URL mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestHeaderAuthGetWithRangeMatchesAWSExample(t *testing.T) {
	// https://docs.aws.amazon.com/AmazonS3/latest/API/sig-v4-header-based-auth.html (GET Object)
	req, _ := http.NewRequest(http.MethodGet, "https://examplebucket.s3.amazonaws.com/test.txt", nil)
	req.Header.Set("Range", "bytes=0-9")
	signHeaders(req, exAccess, exSecret, exRegion, sha256Hex(nil), exTime, "range")
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request," +
		"SignedHeaders=host;range;x-amz-content-sha256;x-amz-date," +
		"Signature=f0e8bdb87c964420e857bd35b5d6ed310bd44f0170aba48dd91039c6036bdb41"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestHeaderAuthPutObjectMatchesAWSExample(t *testing.T) {
	// Same page, PUT Object: also exercises URI encoding ("$" -> %24) and extra signed headers.
	req, _ := http.NewRequest(http.MethodPut, "https://examplebucket.s3.amazonaws.com/"+uriEncode("test$file.text", false), nil)
	req.Header.Set("date", "Fri, 24 May 2013 00:00:00 GMT")
	req.Header.Set("x-amz-storage-class", "REDUCED_REDUNDANCY")
	signHeaders(req, exAccess, exSecret, exRegion, sha256Hex([]byte("Welcome to Amazon S3.")), exTime, "date", "x-amz-storage-class")
	want := "AWS4-HMAC-SHA256 Credential=AKIAIOSFODNN7EXAMPLE/20130524/us-east-1/s3/aws4_request," +
		"SignedHeaders=date;host;x-amz-content-sha256;x-amz-date;x-amz-storage-class," +
		"Signature=98ad721746da40c64f1a55b78f14c238d841ea1380cd77a1b5971af0ece108bd"
	if got := req.Header.Get("Authorization"); got != want {
		t.Fatalf("Authorization mismatch\n got: %s\nwant: %s", got, want)
	}
}

func TestURIEncode(t *testing.T) {
	cases := []struct {
		in    string
		slash bool
		want  string
	}{
		{"a b/c", false, "a%20b/c"},
		{"a b/c", true, "a%20b%2Fc"},
		{"test$file.text", false, "test%24file.text"},
		{"AZaz09-_.~", true, "AZaz09-_.~"},
		{"é", true, "%C3%A9"},
	}
	for _, c := range cases {
		if got := uriEncode(c.in, c.slash); got != c.want {
			t.Errorf("uriEncode(%q,%v) = %q, want %q", c.in, c.slash, got, c.want)
		}
	}
}

func TestCanonicalQuerySortsAndEncodes(t *testing.T) {
	q := url.Values{"uploadId": {"a b"}, "partNumber": {"2"}, "uploads": {""}}
	got := canonicalQuery(q)
	want := "partNumber=2&uploadId=a%20b&uploads="
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
}

func TestPresignIsDeterministicAndBoundToHost(t *testing.T) {
	a := presign("PUT", "http://localhost:9000", "localhost:9000", "/b/k", url.Values{"partNumber": {"1"}}, nil, exAccess, exSecret, exRegion, time.Hour, exTime)
	b := presign("PUT", "http://localhost:9000", "localhost:9000", "/b/k", url.Values{"partNumber": {"1"}}, nil, exAccess, exSecret, exRegion, time.Hour, exTime)
	c := presign("PUT", "http://other:9000", "other:9000", "/b/k", url.Values{"partNumber": {"1"}}, nil, exAccess, exSecret, exRegion, time.Hour, exTime)
	if a != b {
		t.Fatal("presign must be deterministic")
	}
	sig := func(u string) string { return u[strings.LastIndex(u, "=")+1:] }
	if sig(a) == sig(c) {
		t.Fatal("signature must depend on the host")
	}
}
