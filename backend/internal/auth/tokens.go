package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"math/big"
	"time"
)

// RandomToken returns n random bytes, base64url encoded (no padding).
func RandomToken(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error()) // never continue with weak randomness
	}
	return base64.RawURLEncoding.EncodeToString(b)
}

// HashToken is SHA-256. It is only used for high-entropy random tokens
// (session cookies, reset links) where a fast hash is safe: if the database
// leaks, the raw token cannot be recovered or guessed.
func HashToken(token string) []byte {
	h := sha256.Sum256([]byte(token))
	return h[:]
}

// GenerateOTP returns a uniformly random 6-digit code (leading zeros kept).
func GenerateOTP() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	return fmt.Sprintf("%06d", n.Int64())
}

// Keys are per-purpose secrets derived from the master AUTH_SECRET so that a
// value signed for one purpose can never be replayed for another.
type Keys struct{ Age, OTP, IP []byte }

func DeriveKeys(master []byte) Keys {
	d := func(label string) []byte {
		m := hmac.New(sha256.New, master)
		m.Write([]byte("redxtube/v1/" + label))
		return m.Sum(nil)
	}
	return Keys{Age: d("age-cookie"), OTP: d("email-otp"), IP: d("ip-hash")}
}

// OTPDigest is the digest of a 6-digit email code. A plain hash of 6 digits
// could be brute-forced offline in microseconds; the server-side key prevents
// that, and binding to the user stops codes being moved between accounts.
func OTPDigest(key []byte, userID, code string) []byte {
	return TokenDigest(key, "EMAIL_VERIFY", userID, code)
}

// HashIP returns a keyed hash of an IP for abuse tracking; raw IPs are not
// stored in the age/view tables.
func HashIP(key []byte, ip string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte("ip|" + ip))
	return m.Sum(nil)
}

func Equal(a, b []byte) bool { return subtle.ConstantTimeCompare(a, b) == 1 }

// RandomUUID returns a random (v4) UUID string.
func RandomUUID() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// NewUUIDv7 returns a time-ordered UUID (RFC 9562 v7): 48-bit millisecond
// timestamp + random bits. The database schema uses v7 for hot tables so
// inserts append to the end of the index instead of scattering.
func NewUUIDv7() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		panic("crypto/rand unavailable: " + err.Error())
	}
	ms := uint64(time.Now().UnixMilli())
	b[0], b[1], b[2], b[3], b[4], b[5] = byte(ms>>40), byte(ms>>32), byte(ms>>24), byte(ms>>16), byte(ms>>8), byte(ms)
	b[6] = (b[6] & 0x0f) | 0x70
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}

// TokenDigest is HMAC-SHA256(key, purpose|userID|secret): the value stored in
// verification_tokens.token_hash. Binding purpose and user means a code or link
// issued for one purpose/account can never be replayed for another.
func TokenDigest(key []byte, purpose, userID, secret string) []byte {
	m := hmac.New(sha256.New, key)
	m.Write([]byte(purpose + "|" + userID + "|" + secret))
	return m.Sum(nil)
}
