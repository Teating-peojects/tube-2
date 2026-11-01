package auth

import (
	"context"
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	MinPasswordRunes = 10
	MaxPasswordRunes = 128
	maxPasswordBytes = 512
)

// Hasher hashes passwords with Argon2id (PHC string format). A semaphore
// bounds concurrent hashing: each hash needs `memory` KiB of RAM, so an
// attacker flooding /login must not be able to exhaust server memory.
type Hasher struct {
	memory  uint32 // KiB
	time    uint32
	threads uint8
	sem     chan struct{}
	dummy   string
}

// NewHasher uses OWASP-recommended-or-better parameters (64 MiB, t=3, p=2).
func NewHasher(maxConcurrent int) *Hasher { return NewHasherWithParams(64*1024, 3, 2, maxConcurrent) }

func NewHasherWithParams(memoryKiB, time uint32, threads uint8, maxConcurrent int) *Hasher {
	if maxConcurrent < 1 {
		maxConcurrent = 4
	}
	h := &Hasher{memory: memoryKiB, time: time, threads: threads, sem: make(chan struct{}, maxConcurrent)}
	// A real hash of random data: Verify against it takes the same time as a real
	// check, so unknown accounts are indistinguishable from wrong passwords.
	h.dummy, _ = h.Hash(context.Background(), "dummy-password-"+RandomToken(8))
	return h
}

func (h *Hasher) acquire(ctx context.Context) error {
	select {
	case h.sem <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (h *Hasher) release() { <-h.sem }

func (h *Hasher) Hash(ctx context.Context, password string) (string, error) {
	if len(password) > maxPasswordBytes {
		return "", errors.New("password too long")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	if err := h.acquire(ctx); err != nil {
		return "", err
	}
	defer h.release()
	key := argon2.IDKey([]byte(password), salt, h.time, h.memory, h.threads, 32)
	return fmt.Sprintf("$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s", argon2.Version, h.memory, h.time, h.threads,
		base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}

// Verify checks password against an encoded hash in constant time.
// needsRehash is true when the stored parameters are weaker than the current ones.
func (h *Hasher) Verify(ctx context.Context, password, encoded string) (ok, needsRehash bool, err error) {
	if len(password) > maxPasswordBytes {
		return false, false, nil
	}
	m, t, p, salt, want, err := decode(encoded)
	if err != nil {
		return false, false, err
	}
	if err := h.acquire(ctx); err != nil {
		return false, false, err
	}
	defer h.release()
	got := argon2.IDKey([]byte(password), salt, t, m, p, uint32(len(want)))
	ok = subtle.ConstantTimeCompare(got, want) == 1
	needsRehash = ok && (m < h.memory || t < h.time || p < h.threads)
	return ok, needsRehash, nil
}

// Burn spends the same effort as a real verification and always fails.
func (h *Hasher) Burn(ctx context.Context, password string) {
	_, _, _ = h.Verify(ctx, password, h.dummy)
}

func decode(encoded string) (m, t uint32, p uint8, salt, key []byte, err error) {
	bad := errors.New("invalid password hash")
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" {
		return 0, 0, 0, nil, nil, bad
	}
	var version int
	if _, err := fmt.Sscanf(parts[2], "v=%d", &version); err != nil || version != argon2.Version {
		return 0, 0, 0, nil, nil, bad
	}
	var threads uint32
	if _, err := fmt.Sscanf(parts[3], "m=%d,t=%d,p=%d", &m, &t, &threads); err != nil {
		return 0, 0, 0, nil, nil, bad
	}
	// Guard against absurd stored parameters (memory/CPU exhaustion).
	if m < 8 || m > 256*1024 || t < 1 || t > 10 || threads < 1 || threads > 16 {
		return 0, 0, 0, nil, nil, bad
	}
	p = uint8(threads)
	if salt, err = base64.RawStdEncoding.DecodeString(parts[4]); err != nil || len(salt) < 8 {
		return 0, 0, 0, nil, nil, bad
	}
	if key, err = base64.RawStdEncoding.DecodeString(parts[5]); err != nil || len(key) < 16 {
		return 0, 0, 0, nil, nil, bad
	}
	return m, t, p, salt, key, nil
}

var commonPasswords = map[string]bool{
	"password": true, "password1": true, "password12": true, "password123": true, "passw0rd123": true,
	"1234567890": true, "12345678910": true, "123456789012": true, "1q2w3e4r5t": true, "qwertyuiop": true,
	"qwerty12345": true, "qwerty123456": true, "iloveyou123": true, "letmein1234": true, "welcome1234": true,
	"admin12345": true, "administrator1": true, "abc1234567": true, "abcd123456": true, "redxtube123": true,
	"redxtube1234": true, "redxtube12345": true, "changeme123": true, "trustno1234": true, "monkey12345": true,
}

// ValidatePassword enforces the same rules as the frontend (10-128 chars,
// a letter and a digit) plus checks the frontend cannot do. It returns a
// short, user-safe reason or "" when the password is acceptable.
func ValidatePassword(pw, username, email string) string {
	n := utf8.RuneCountInString(pw)
	switch {
	case !utf8.ValidString(pw):
		return "Invalid characters"
	case n < MinPasswordRunes:
		return "Use at least 10 characters"
	case n > MaxPasswordRunes || len(pw) > maxPasswordBytes:
		return "Use at most 128 characters"
	}
	var letter, digit bool
	for _, r := range pw {
		switch {
		case unicode.IsLetter(r):
			letter = true
		case unicode.IsDigit(r):
			digit = true
		case unicode.IsControl(r):
			return "Invalid characters"
		}
	}
	if !letter {
		return "Include at least one letter"
	}
	if !digit {
		return "Include at least one number"
	}
	low := strings.ToLower(pw)
	if commonPasswords[low] {
		return "That password is too common"
	}
	if username != "" && low == strings.ToLower(username) {
		return "Password can't be your username"
	}
	if email != "" {
		local := strings.ToLower(strings.SplitN(email, "@", 2)[0])
		if low == strings.ToLower(email) || (len(local) >= 6 && low == local) {
			return "Password can't be your email"
		}
	}
	return ""
}
