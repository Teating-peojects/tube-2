package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"regexp"
	"strconv"
	"strings"
	"time"
)

var subjectRe = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)

// AgeSigner issues and verifies the age-verification cookie. The cookie is
// signed (HMAC-SHA256), so a client cannot forge or extend it by editing
// a plain "xh_age_ok=1" value.
//
//	v1.<subject uuid>.<expiry unix>.<base64url mac>
//
// subject is the anonymous visitor id, or the user id for signed-in accounts.
type AgeSigner struct {
	key []byte
	now func() time.Time
}

func NewAgeSigner(key []byte) AgeSigner { return AgeSigner{key: key, now: time.Now} }

func (s AgeSigner) mac(subject string, exp int64) string {
	m := hmac.New(sha256.New, s.key)
	m.Write([]byte("age|v1|" + subject + "|" + strconv.FormatInt(exp, 10)))
	return base64.RawURLEncoding.EncodeToString(m.Sum(nil))
}

func (s AgeSigner) Issue(subject string, ttl time.Duration) (token string, expires time.Time) {
	expires = s.now().Add(ttl).UTC().Truncate(time.Second)
	exp := expires.Unix()
	return "v1." + subject + "." + strconv.FormatInt(exp, 10) + "." + s.mac(subject, exp), expires
}

// Verify returns the subject of a valid, unexpired token.
func (s AgeSigner) Verify(token string) (subject string, ok bool) {
	parts := strings.Split(token, ".")
	if len(parts) != 4 || parts[0] != "v1" || !subjectRe.MatchString(parts[1]) {
		return "", false
	}
	exp, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || exp <= s.now().Unix() {
		return "", false
	}
	if !hmac.Equal([]byte(parts[3]), []byte(s.mac(parts[1], exp))) {
		return "", false
	}
	return parts[1], true
}
