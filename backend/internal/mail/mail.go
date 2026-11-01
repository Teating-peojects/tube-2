// Package mail sends transactional email (verification codes, password reset).
// Production uses Resend's HTTP API. Sending is asynchronous with retries so a
// slow or failing provider never blocks a request.
package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"sync"
	"time"

	"github.com/redxtube/api/internal/auth"
)

type Message struct {
	To      string
	Subject string
	Text    string
	HTML    string
}

type Mailer interface {
	Send(ctx context.Context, m Message) error
}

// ---------- Resend ----------

type Resend struct {
	APIKey   string // secret: never logged
	From     string
	Endpoint string // default https://api.resend.com/emails
	Client   *http.Client
}

func NewResend(apiKey, from string) *Resend {
	return &Resend{APIKey: apiKey, From: from, Endpoint: "https://api.resend.com/emails", Client: &http.Client{Timeout: 10 * time.Second}}
}

type permanentError struct{ error }

func (p permanentError) Unwrap() error { return p.error }

// IsPermanent reports errors that retrying cannot fix (bad request, bad key).
func IsPermanent(err error) bool { var p permanentError; return errors.As(err, &p) }

func (r *Resend) Send(ctx context.Context, m Message) error {
	body, err := json.Marshal(map[string]any{"from": r.From, "to": []string{m.To}, "subject": m.Subject, "text": m.Text, "html": m.HTML})
	if err != nil {
		return permanentError{err}
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.Endpoint, bytes.NewReader(body))
	if err != nil {
		return permanentError{err}
	}
	req.Header.Set("Authorization", "Bearer "+r.APIKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Idempotency-Key", auth.RandomToken(16)) // a retried send cannot create a duplicate email
	resp, err := r.Client.Do(req)
	if err != nil {
		return fmt.Errorf("resend request: %w", err) // network: retryable. err text never contains the key.
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 == 2 {
		io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<16))
		return nil
	}
	snippet, _ := io.ReadAll(io.LimitReader(resp.Body, 300))
	err = fmt.Errorf("resend responded %d: %s", resp.StatusCode, snippet)
	if resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= 500 {
		return err // retryable
	}
	return permanentError{err}
}

// ---------- development drivers ----------

// Log prints emails (including one-time codes) to the log. Development only:
// config refuses MAIL_DRIVER=log in production.
type Log struct{ L *slog.Logger }

func (l Log) Send(ctx context.Context, m Message) error {
	l.L.InfoContext(ctx, "DEV EMAIL (MAIL_DRIVER=log)", "to", m.To, "subject", m.Subject, "body", m.Text)
	return nil
}

// Discard drops every email (MAIL_DRIVER=none).
type Discard struct{}

func (Discard) Send(context.Context, Message) error { return nil }

// ---------- async wrapper ----------

var ErrQueueFull = errors.New("mail queue full")

type Async struct {
	inner   Mailer
	log     *slog.Logger
	queue   chan Message
	wg      sync.WaitGroup
	backoff []time.Duration
	mu      sync.RWMutex
	closed  bool
}

// NewAsync starts `workers` goroutines. Each message is tried up to
// len(backoff)+1 times, waiting backoff[i] between attempts.
func NewAsync(inner Mailer, log *slog.Logger, workers, queueSize int, backoff []time.Duration) *Async {
	a := &Async{inner: inner, log: log, queue: make(chan Message, queueSize), backoff: backoff}
	for i := 0; i < workers; i++ {
		a.wg.Add(1)
		go a.worker()
	}
	return a
}

// Send enqueues without blocking the request. A full queue is reported so the
// caller can log it; users can always request another code.
func (a *Async) Send(_ context.Context, m Message) error {
	a.mu.RLock()
	defer a.mu.RUnlock()
	if a.closed {
		return errors.New("mailer closed")
	}
	select {
	case a.queue <- m:
		return nil
	default:
		return ErrQueueFull
	}
}

func (a *Async) worker() {
	defer a.wg.Done()
	for m := range a.queue {
		a.deliver(m)
	}
}

func (a *Async) deliver(m Message) {
	var err error
	for attempt := 0; ; attempt++ {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		err = a.inner.Send(ctx, m)
		cancel()
		if err == nil {
			return
		}
		if IsPermanent(err) || attempt >= len(a.backoff) {
			break
		}
		time.Sleep(a.backoff[attempt])
	}
	// The recipient address and message body are deliberately not logged.
	a.log.Error("email delivery failed", "subject", m.Subject, "err", err.Error())
}

// Close stops accepting mail and waits (up to ctx) for queued messages to be sent.
func (a *Async) Close(ctx context.Context) error {
	a.mu.Lock()
	if !a.closed {
		a.closed = true
		close(a.queue)
	}
	a.mu.Unlock()
	done := make(chan struct{})
	go func() { a.wg.Wait(); close(done) }()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}
