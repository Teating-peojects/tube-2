package mail

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func discardLog() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestResendSendsExpectedRequest(t *testing.T) {
	var gotAuth, gotIdem string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth, gotIdem = r.Header.Get("Authorization"), r.Header.Get("Idempotency-Key")
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(200)
		w.Write([]byte(`{"id":"x"}`))
	}))
	defer srv.Close()
	r := NewResend("re_secret", "REDXTUBE <no-reply@redxtube.com>")
	r.Endpoint = srv.URL
	if err := r.Send(context.Background(), VerifyCode("a@b.test", "123456", 10)); err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer re_secret" || len(gotIdem) < 16 {
		t.Fatalf("headers: %q %q", gotAuth, gotIdem)
	}
	if body["from"] != "REDXTUBE <no-reply@redxtube.com>" || body["to"].([]any)[0] != "a@b.test" || !strings.Contains(body["text"].(string), "123456") {
		t.Fatalf("body: %v", body)
	}
}

func TestResendErrorClassification(t *testing.T) {
	status := 500
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		w.Write([]byte(`{"message":"nope"}`))
	}))
	defer srv.Close()
	r := NewResend("re_secret", "x@y.test")
	r.Endpoint = srv.URL
	for code, permanent := range map[int]bool{500: false, 503: false, 429: false, 400: true, 401: true, 403: true, 422: true} {
		status = code
		err := r.Send(context.Background(), Message{To: "a@b.test", Subject: "s", Text: "t"})
		if err == nil || IsPermanent(err) != permanent {
			t.Errorf("status %d: err=%v permanent=%v want %v", code, err, IsPermanent(err), permanent)
		}
		if err != nil && strings.Contains(err.Error(), "re_secret") {
			t.Errorf("status %d: error leaks the API key", code)
		}
	}
	r.Endpoint = "http://127.0.0.1:1"
	if err := r.Send(context.Background(), Message{To: "a@b.test"}); err == nil || IsPermanent(err) || strings.Contains(err.Error(), "re_secret") {
		t.Fatalf("network errors must be retryable and key-free: %v", err)
	}
}

type flaky struct {
	fails int32
	calls int32
	perm  bool
	mu    sync.Mutex
	sent  []Message
}

func (f *flaky) Send(_ context.Context, m Message) error {
	n := atomic.AddInt32(&f.calls, 1)
	if n <= f.fails {
		if f.perm {
			return permanentError{errors.New("bad")}
		}
		return errors.New("temporary")
	}
	f.mu.Lock()
	f.sent = append(f.sent, m)
	f.mu.Unlock()
	return nil
}

func TestAsyncRetriesThenDelivers(t *testing.T) {
	f := &flaky{fails: 2}
	a := NewAsync(f, discardLog(), 1, 8, []time.Duration{5 * time.Millisecond, 5 * time.Millisecond, 5 * time.Millisecond})
	if err := a.Send(context.Background(), Message{To: "a@b.test", Subject: "s"}); err != nil {
		t.Fatal(err)
	}
	if err := a.Close(context.Background()); err != nil {
		t.Fatal(err)
	}
	if len(f.sent) != 1 || f.calls != 3 {
		t.Fatalf("sent=%d calls=%d", len(f.sent), f.calls)
	}
}

func TestAsyncGivesUpAndSkipsPermanentErrors(t *testing.T) {
	f := &flaky{fails: 100}
	a := NewAsync(f, discardLog(), 1, 8, []time.Duration{time.Millisecond, time.Millisecond})
	a.Send(context.Background(), Message{To: "a@b.test"})
	a.Close(context.Background())
	if f.calls != 3 || len(f.sent) != 0 {
		t.Fatalf("expected 3 attempts then give up: calls=%d", f.calls)
	}
	p := &flaky{fails: 100, perm: true}
	a = NewAsync(p, discardLog(), 1, 8, []time.Duration{time.Millisecond, time.Millisecond})
	a.Send(context.Background(), Message{To: "a@b.test"})
	a.Close(context.Background())
	if p.calls != 1 {
		t.Fatalf("permanent errors must not be retried: %d", p.calls)
	}
}

func TestAsyncQueueFullAndClose(t *testing.T) {
	block := make(chan struct{})
	slow := mailerFunc(func(context.Context, Message) error { <-block; return nil })
	a := NewAsync(slow, discardLog(), 1, 1, nil)
	a.Send(context.Background(), Message{}) // taken by the worker
	time.Sleep(20 * time.Millisecond)
	a.Send(context.Background(), Message{}) // fills the queue
	if err := a.Send(context.Background(), Message{}); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("want ErrQueueFull, got %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	if err := a.Close(ctx); err == nil {
		t.Fatal("Close must report that draining did not finish")
	}
	close(block)
	if err := a.Send(context.Background(), Message{}); err == nil {
		t.Fatal("Send after Close must fail")
	}
}

type mailerFunc func(context.Context, Message) error

func (f mailerFunc) Send(ctx context.Context, m Message) error { return f(ctx, m) }

func TestTemplates(t *testing.T) {
	m := VerifyCode("a@b.test", "042913", 10)
	if m.To != "a@b.test" || !strings.Contains(m.Text, "042913") || !strings.Contains(m.HTML, "042913") || !strings.Contains(m.Text, "10 minutes") {
		t.Fatalf("verify: %+v", m)
	}
	link := `https://redxtube.com/reset-password?token=abc"><script>alert(1)</script>`
	r := ResetPassword("a@b.test", link, 30)
	if strings.Contains(r.HTML, "<script>") {
		t.Fatalf("HTML must be escaped: %s", r.HTML)
	}
	if !strings.Contains(r.Text, link) || !strings.Contains(r.HTML, "Choose a new password") {
		t.Fatalf("reset: %+v", r)
	}
	if p := PasswordChanged("a@b.test"); !strings.Contains(p.Text, "changed") || p.HTML == "" {
		t.Fatalf("changed: %+v", p)
	}
}
