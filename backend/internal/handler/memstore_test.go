package handler_test

import (
	"bytes"
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"io"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redxtube/api/internal/storage"
)

// memStore is an in-memory object store with the multipart semantics of S3/MinIO that the upload code relies on:
// parts are stored under an upload id, the ETag is the part's MD5, completion checks ETags and assembles the object,
// and a finished or aborted upload answers 404 NoSuchUpload. The test plays the browser through put().
type memStore struct {
	mu      sync.Mutex
	seq     int
	uploads map[string]*memUpload
	objects map[string]memObject
	deleted []string // prefixes passed to DeletePrefix

	failListParts bool  // a storage outage while the upload is verified
	pingErr       error // what Ping reports
}

type memUpload struct {
	key, contentType string
	parts            map[int][]byte
}
type memObject struct {
	data        []byte
	contentType string
}

func newMemStore() *memStore {
	return &memStore{uploads: map[string]*memUpload{}, objects: map[string]memObject{}}
}

func etagOf(b []byte) string { s := md5.Sum(b); return hex.EncodeToString(s[:]) }

func (m *memStore) CreateMultipart(_ context.Context, key, contentType string) (string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.seq++
	id := fmt.Sprintf("mp-%d", m.seq)
	m.uploads[id] = &memUpload{key: key, contentType: contentType, parts: map[int][]byte{}}
	return id, nil
}

func (m *memStore) PresignUploadPart(key, id string, n int, _ time.Duration) string {
	return fmt.Sprintf("https://mem.test/%s?uploadId=%s&partNumber=%d", key, id, n)
}

// put is the browser: a plain PUT to a presigned part URL. It returns the HTTP status and the ETag (no quotes).
func (m *memStore) put(rawURL string, body []byte) (int, string) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return 400, ""
	}
	n, _ := strconv.Atoi(u.Query().Get("partNumber"))
	m.mu.Lock()
	defer m.mu.Unlock()
	up := m.uploads[u.Query().Get("uploadId")]
	if up == nil || n < 1 {
		return 404, "" // NoSuchUpload: aborted, completed or never existed
	}
	up.parts[n] = append([]byte(nil), body...)
	return 200, etagOf(body)
}

func (m *memStore) ListParts(_ context.Context, _, id string) ([]storage.Part, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.failListParts {
		return nil, &storage.Error{Status: 503, Code: "SlowDown", Message: "storage is down"}
	}
	up := m.uploads[id]
	if up == nil {
		return nil, &storage.Error{Status: 404, Code: "NoSuchUpload", Message: "The specified upload does not exist."}
	}
	out := make([]storage.Part, 0, len(up.parts))
	for n, b := range up.parts {
		out = append(out, storage.Part{Number: n, ETag: `"` + etagOf(b) + `"`, Size: int64(len(b))})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Number < out[j].Number })
	return out, nil
}

func (m *memStore) CompleteMultipart(_ context.Context, key, id string, parts []storage.Part) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	up := m.uploads[id]
	if up == nil {
		return &storage.Error{Status: 404, Code: "NoSuchUpload", Message: "The specified upload does not exist."}
	}
	var all bytes.Buffer
	for _, p := range parts {
		b, ok := up.parts[p.Number]
		if !ok || strings.Trim(p.ETag, `"`) != etagOf(b) {
			return &storage.Error{Status: 400, Code: "InvalidPart", Message: "One or more of the specified parts could not be found."}
		}
		all.Write(b)
	}
	m.objects[key] = memObject{data: all.Bytes(), contentType: up.contentType}
	delete(m.uploads, id)
	return nil
}

// AbortMultipart is idempotent, like the real client (404 NoSuchUpload is success there).
func (m *memStore) AbortMultipart(_ context.Context, _, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.uploads, id)
	return nil
}

func (m *memStore) PresignPut(key, _ string, _ int64, _ time.Duration) string {
	return "https://mem.test/" + key + "?put"
}

func (m *memStore) Head(_ context.Context, key string) (storage.ObjectInfo, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objects[key]
	if !ok {
		return storage.ObjectInfo{}, storage.ErrNotFound
	}
	return storage.ObjectInfo{Size: int64(len(o.data)), ContentType: o.contentType}, nil
}

func (m *memStore) GetRange(_ context.Context, key string, start, end int64) ([]byte, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objects[key]
	if !ok {
		return nil, storage.ErrNotFound
	}
	if end >= int64(len(o.data)) {
		end = int64(len(o.data)) - 1
	}
	return append([]byte(nil), o.data[start:end+1]...), nil
}

func (m *memStore) Download(_ context.Context, key string, w io.Writer) (int64, error) {
	m.mu.Lock()
	o, ok := m.objects[key]
	m.mu.Unlock()
	if !ok {
		return 0, storage.ErrNotFound
	}
	n, err := w.Write(o.data)
	return int64(n), err
}

func (m *memStore) Put(_ context.Context, key, contentType, _ string, r io.Reader, _ int64) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}
	return m.PutBytes(context.Background(), key, contentType, "", b)
}

func (m *memStore) PutBytes(_ context.Context, key, contentType, _ string, b []byte) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = memObject{data: append([]byte(nil), b...), contentType: contentType}
	return nil
}

func (m *memStore) Delete(_ context.Context, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.objects, key)
	return nil
}

func (m *memStore) DeletePrefix(_ context.Context, prefix string) (int, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.deleted = append(m.deleted, prefix)
	n := 0
	for k := range m.objects {
		if strings.HasPrefix(k, prefix) {
			delete(m.objects, k)
			n++
		}
	}
	return n, nil
}

func (m *memStore) Ping(context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.pingErr
}

// ---- inspection helpers for tests ----

func (m *memStore) hasObject(key string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	_, ok := m.objects[key]
	return ok
}
func (m *memStore) object(key string) (memObject, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	o, ok := m.objects[key]
	return o, ok
}
func (m *memStore) openUploads() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.uploads)
}
func (m *memStore) deletedPrefixes() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]string(nil), m.deleted...)
}
func (m *memStore) set(f func(*memStore)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	f(m)
}

var _ storage.API = (*memStore)(nil)
