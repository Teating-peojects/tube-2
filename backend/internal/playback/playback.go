// Package playback issues and verifies short-lived HLS access tokens (B7).
//
// The token is the first path segment of the stream URL:
//
//	https://stream.example.com/<exp>.<sig>/hls/<videoId>/master.m3u8
//
// HLS players resolve the relative URLs inside a playlist against the playlist
// URL, so every rendition playlist and segment inherits the same token with no
// playlist rewriting. The signature covers the video id and the expiry, so a
// token for one video opens nothing else. Tokens are only ever handed out by
// the API after the age gate and the visibility rules, and they expire.
package playback

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// DefaultTTL is how long a stream URL stays valid. Long enough for a feature
// film plus pauses; short enough that a leaked URL stops working the same day.
const DefaultTTL = 6 * time.Hour

var (
	// Only these object names can ever be served: master, rendition playlists
	// and numbered segments. Everything else (originals, thumbnails, arbitrary
	// keys) is unreachable through the stream path by construction.
	objectRe = regexp.MustCompile(`^hls/([0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12})/(?:master\.m3u8|(?:360p|480p|720p|1080p)/(?:index\.m3u8|seg_[0-9]{5}\.ts))$`)
	uuidRe   = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

	ErrInvalid = errors.New("playback: invalid or expired token")
)

// Signer creates stream URLs.
type Signer struct {
	key  []byte
	base string
	ttl  time.Duration
	now  func() time.Time
}

// NewSigner returns a signer. base is the public stream origin, e.g. https://stream.redxtube.com.
// key must be at least 32 bytes.
func NewSigner(key []byte, base string, ttl time.Duration) (*Signer, error) {
	if len(key) < 32 {
		return nil, errors.New("playback: signing key must be at least 32 bytes")
	}
	if ttl <= 0 {
		ttl = DefaultTTL
	}
	return &Signer{key: key, base: strings.TrimRight(base, "/"), ttl: ttl, now: time.Now}, nil
}

func mac(key []byte, videoID string, exp int64) string {
	m := hmac.New(sha256.New, key)
	m.Write([]byte("pb1|" + videoID + "|" + strconv.FormatInt(exp, 10)))
	return hex.EncodeToString(m.Sum(nil)[:16]) // 128 bits is plenty for a bearer token
}

// Token returns the path token for a video.
func (s *Signer) Token(videoID string) string {
	exp := s.now().Add(s.ttl).Unix()
	return strconv.FormatInt(exp, 10) + "." + mac(s.key, videoID, exp)
}

// URL returns the full stream URL for a video whose master playlist is at
// masterKey (for example "hls/<id>/master.m3u8"). It returns "" when the key
// is not a servable object or does not belong to videoID.
func (s *Signer) URL(videoID, masterKey string) string {
	vid, ok := ObjectVideo(masterKey)
	if !ok || vid != videoID {
		return ""
	}
	return s.base + "/" + s.Token(videoID) + "/" + masterKey
}

// ObjectVideo extracts the video id from a servable object key.
func ObjectVideo(key string) (string, bool) {
	m := objectRe.FindStringSubmatch(key)
	if m == nil {
		return "", false
	}
	return m[1], true
}

// Verify checks a token against the requested object key and returns the video id.
func Verify(key []byte, token, objectKey string, now time.Time) (string, error) {
	videoID, ok := ObjectVideo(objectKey)
	if !ok || !uuidRe.MatchString(videoID) {
		return "", ErrInvalid
	}
	dot := strings.IndexByte(token, '.')
	if dot < 1 || dot == len(token)-1 {
		return "", ErrInvalid
	}
	exp, err := strconv.ParseInt(token[:dot], 10, 64)
	if err != nil || exp <= now.Unix() {
		return "", ErrInvalid
	}
	want := mac(key, videoID, exp)
	if !hmac.Equal([]byte(want), []byte(token[dot+1:])) {
		return "", ErrInvalid
	}
	return videoID, nil
}
