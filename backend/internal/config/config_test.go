package config

import (
	"strings"
	"testing"
	"time"
)

func env(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

const secret = "0123456789abcdef0123456789abcdef-secret"

func devBase() map[string]string {
	return map[string]string{"DATABASE_URL": "postgres://x", "REDIS_URL": "redis://x"}
}

func prodBase() map[string]string {
	return map[string]string{
		"APP_ENV": "production", "DATABASE_URL": "postgres://x", "REDIS_URL": "redis://x",
		"CORS_ALLOWED_ORIGINS": "https://redxtube.com", "APP_URL": "https://redxtube.com", "AUTH_SECRET": secret,
		"MAIL_DRIVER": "resend", "RESEND_API_KEY": "re_test", "MAIL_FROM": "REDXTUBE <no-reply@redxtube.com>",
		"COOKIE_DOMAIN": ".redxtube.com",
	}
}

func TestLoadDevDefaults(t *testing.T) {
	c, err := Load(env(devBase()))
	if err != nil {
		t.Fatal(err)
	}
	if c.Env != "development" || c.Port != "8080" || c.DBMaxConns != 10 || c.AppURL != "http://localhost:3000" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if c.CookieSecure || c.SessionCookieName != "xh_session" || c.Mail.Driver != "log" {
		t.Fatalf("unexpected auth defaults: %+v", c)
	}
	if len(c.AuthSecret) != 32 || !c.AuthSecretEphemeral {
		t.Fatal("dev must get an ephemeral random secret")
	}
	c2, _ := Load(env(devBase()))
	if string(c.AuthSecret) == string(c2.AuthSecret) {
		t.Fatal("ephemeral secrets must differ per load")
	}
}

func TestLoadRequiresDependencies(t *testing.T) {
	_, err := Load(env(nil))
	if err == nil || !strings.Contains(err.Error(), "DATABASE_URL") || !strings.Contains(err.Error(), "REDIS_URL") {
		t.Fatalf("expected both missing-var errors, got %v", err)
	}
}

func TestLoadValidProduction(t *testing.T) {
	c, err := Load(env(prodBase()))
	if err != nil {
		t.Fatal(err)
	}
	if !c.CookieSecure || c.CookieDomain != ".redxtube.com" || string(c.AuthSecret) != secret || c.AuthSecretEphemeral {
		t.Fatalf("production config wrong: %+v", c)
	}
}

func TestLoadProductionRules(t *testing.T) {
	cases := map[string]struct {
		mutate func(map[string]string)
		want   string
	}{
		"cors required":      {func(m map[string]string) { delete(m, "CORS_ALLOWED_ORIGINS") }, "CORS_ALLOWED_ORIGINS"},
		"cors https":         {func(m map[string]string) { m["CORS_ALLOWED_ORIGINS"] = "http://redxtube.com" }, "https"},
		"app url required":   {func(m map[string]string) { delete(m, "APP_URL") }, "APP_URL"},
		"app url https":      {func(m map[string]string) { m["APP_URL"] = "http://redxtube.com" }, "https"},
		"secret required":    {func(m map[string]string) { delete(m, "AUTH_SECRET") }, "AUTH_SECRET is required"},
		"secret too short":   {func(m map[string]string) { m["AUTH_SECRET"] = "short" }, "at least 32"},
		"log mailer banned":  {func(m map[string]string) { m["MAIL_DRIVER"] = "log" }, "not allowed in production"},
		"none mailer banned": {func(m map[string]string) { m["MAIL_DRIVER"] = "none" }, "not allowed in production"},
		"resend needs key":   {func(m map[string]string) { delete(m, "RESEND_API_KEY") }, "RESEND_API_KEY"},
		"resend needs from":  {func(m map[string]string) { delete(m, "MAIL_FROM") }, "MAIL_FROM"},
		"cdn must be https":  {func(m map[string]string) { m["CDN_URL"] = "http://cdn.redxtube.com" }, "CDN_URL"},
	}
	for name, c := range cases {
		m := prodBase()
		c.mutate(m)
		if _, err := Load(env(m)); err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s: want error containing %q, got %v", name, c.want, err)
		}
	}
}

func TestLoadRejectsBadValues(t *testing.T) {
	cases := map[string]map[string]string{
		"bad env":        {"APP_ENV": "prod"},
		"bad port":       {"PORT": "99999"},
		"bad level":      {"LOG_LEVEL": "loud"},
		"bad conns":      {"DB_MAX_CONNS": "0"},
		"origin w/path":  {"CORS_ALLOWED_ORIGINS": "https://a.com/x"},
		"bad cookie dom": {"COOKIE_DOMAIN": "https://redxtube.com"},
		"bad cookie nm":  {"SESSION_COOKIE_NAME": "bad name;"},
		"bad mail":       {"MAIL_DRIVER": "smtp"},
		"bad proxy flag": {"TRUST_PROXY_HEADERS": "maybe"},
		"app url path":   {"APP_URL": "https://a.com/app"},
	}
	for name, extra := range cases {
		m := devBase()
		for k, v := range extra {
			m[k] = v
		}
		if _, err := Load(env(m)); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestStorageOptionalAndAllOrNothing(t *testing.T) {
	c, err := Load(env(devBase()))
	if err != nil || c.Storage.Enabled() {
		t.Fatalf("storage must be optional: %+v %v", c.Storage, err)
	}
	m := devBase()
	m["STORAGE_ENDPOINT"] = "http://minio:9000"
	_, err = Load(env(m))
	for _, k := range []string{"STORAGE_ACCESS_KEY", "STORAGE_SECRET_KEY", "STORAGE_BUCKET"} {
		if err == nil || !strings.Contains(err.Error(), k) {
			t.Fatalf("a partial storage config must name %s, got %v", k, err)
		}
	}
	m["STORAGE_ACCESS_KEY"], m["STORAGE_SECRET_KEY"], m["STORAGE_BUCKET"] = "ak", "sk", "redxtube"
	c, err = Load(env(m))
	if err != nil || !c.Storage.Enabled() || !c.Storage.PathStyle || c.Storage.Region != "us-east-1" {
		t.Fatalf("valid storage config rejected: %+v %v", c.Storage, err)
	}
}

func TestStorageAddressingAndValidation(t *testing.T) {
	base := devBase()
	base["STORAGE_ACCESS_KEY"], base["STORAGE_SECRET_KEY"], base["STORAGE_BUCKET"] = "ak", "sk", "redxtube"
	with := func(k, v string) map[string]string {
		m := map[string]string{}
		for a, b := range base {
			m[a] = b
		}
		m[k] = v
		return m
	}
	m := with("STORAGE_ENDPOINT", "https://s3.eu-west-1.amazonaws.com/")
	if c, err := Load(env(m)); err != nil || c.Storage.PathStyle || c.Storage.Endpoint != "https://s3.eu-west-1.amazonaws.com" {
		t.Fatalf("AWS must default to virtual-hosted style: %+v %v", c.Storage, err)
	}
	m["STORAGE_PATH_STYLE"] = "true"
	if c, _ := Load(env(m)); !c.Storage.PathStyle {
		t.Fatal("STORAGE_PATH_STYLE must override the default")
	}
	for _, bad := range []map[string]string{
		with("STORAGE_ENDPOINT", "minio:9000"),
		with("STORAGE_ENDPOINT", "http://minio:9000/bucket"),
		func() map[string]string {
			x := with("STORAGE_ENDPOINT", "http://minio:9000")
			x["STORAGE_BUCKET"] = "Bad Bucket!"
			return x
		}(),
		func() map[string]string {
			x := with("STORAGE_ENDPOINT", "http://minio:9000")
			x["STORAGE_PATH_STYLE"] = "maybe"
			return x
		}(),
	} {
		if _, err := Load(env(bad)); err == nil {
			t.Errorf("expected an error for %v", bad)
		}
	}
}

func TestStorageHTTPSRequiredInProduction(t *testing.T) {
	m := prodBase()
	m["STORAGE_ENDPOINT"], m["STORAGE_ACCESS_KEY"], m["STORAGE_SECRET_KEY"], m["STORAGE_BUCKET"] = "http://s3.example.com", "ak", "sk", "redxtube"
	if _, err := Load(env(m)); err == nil || !strings.Contains(err.Error(), "STORAGE_ENDPOINT must be https") {
		t.Fatalf("plain http storage must be refused in production, got %v", err)
	}
	m["STORAGE_ENDPOINT"] = "https://s3.example.com"
	m["STORAGE_PUBLIC_ENDPOINT"] = "http://files.example.com"
	if _, err := Load(env(m)); err == nil || !strings.Contains(err.Error(), "STORAGE_PUBLIC_ENDPOINT must be https") {
		t.Fatalf("plain http public endpoint must be refused in production, got %v", err)
	}
	m["STORAGE_PUBLIC_ENDPOINT"] = "https://files.example.com"
	m["STREAM_BASE_URL"] = "https://api.redxtube.com/stream" // required with storage in production (B7)
	if _, err := Load(env(m)); err != nil {
		t.Fatalf("valid production storage rejected: %v", err)
	}
}

func TestPlaybackConfig(t *testing.T) {
	m := devBase()
	m["STREAM_BASE_URL"] = "http://localhost:8080/stream/"
	c, err := Load(env(m))
	if err != nil || c.StreamBaseURL != "http://localhost:8080/stream" {
		t.Fatalf("%v %q", err, c.StreamBaseURL)
	}
	for name, v := range map[string]string{"scheme": "ftp://x/stream", "no host": "https:///stream", "query": "https://x/stream?a=b"} {
		m := devBase()
		m["STREAM_BASE_URL"] = v
		if _, err := Load(env(m)); err == nil || !strings.Contains(err.Error(), "STREAM_BASE_URL") {
			t.Errorf("%s accepted: %v", name, err)
		}
	}
	m = devBase()
	m["PLAYBACK_SIGNING_KEY"] = "too-short"
	if _, err := Load(env(m)); err == nil || !strings.Contains(err.Error(), "PLAYBACK_SIGNING_KEY") {
		t.Errorf("short key accepted: %v", err)
	}
	m["PLAYBACK_SIGNING_KEY"] = secret
	if c, err := Load(env(m)); err != nil || string(c.PlaybackKey) != secret {
		t.Errorf("valid key: %v", err)
	}

	// production: HLS must never be served from a public bucket
	p := prodBase()
	p["STORAGE_ENDPOINT"], p["STORAGE_ACCESS_KEY"], p["STORAGE_SECRET_KEY"], p["STORAGE_BUCKET"] = "https://s3.example.com", "k", "s", "redxtube"
	if _, err := Load(env(p)); err == nil || !strings.Contains(err.Error(), "STREAM_BASE_URL is required") {
		t.Errorf("production with storage and no stream URL: %v", err)
	}
	p["STREAM_BASE_URL"] = "http://api.redxtube.com/stream"
	if _, err := Load(env(p)); err == nil || !strings.Contains(err.Error(), "https in production") {
		t.Errorf("http stream URL in production: %v", err)
	}
	p["STREAM_BASE_URL"] = "https://api.redxtube.com/stream"
	if _, err := Load(env(p)); err != nil {
		t.Errorf("valid production config: %v", err)
	}
}

func TestMetricsAddr(t *testing.T) {
	m := devBase()
	m["METRICS_ADDR"] = "127.0.0.1:9090"
	if c, err := Load(env(m)); err != nil || c.MetricsAddr != "127.0.0.1:9090" {
		t.Fatalf("%v %q", err, c.MetricsAddr)
	}
	for _, bad := range []string{"9090", "host:0", "host:99999", "host:abc"} {
		m["METRICS_ADDR"] = bad
		if _, err := Load(env(m)); err == nil || !strings.Contains(err.Error(), "METRICS_ADDR") {
			t.Errorf("%q accepted: %v", bad, err)
		}
	}
	if c, _ := Load(env(devBase())); c.MetricsAddr != "" {
		t.Error("metrics must be off by default")
	}
}

func TestUploadLimitsDefaultsAndOverrides(t *testing.T) {
	c, err := Load(env(devBase()))
	if err != nil {
		t.Fatal(err)
	}
	if u := c.Upload; u.MaxBytes != 5<<30 || u.PartSize != 16<<20 || u.MaxOpen != 5 || u.SessionTTL != 24*time.Hour || u.MaxRetries != 3 {
		t.Fatalf("defaults changed: %+v", u)
	}
	m := devBase()
	m["UPLOAD_MAX_BYTES"] = "1073741824"
	m["UPLOAD_PART_SIZE_BYTES"] = "8388608"
	m["UPLOAD_MAX_OPEN"] = "3"
	m["UPLOAD_SESSION_TTL"] = "48h"
	m["UPLOAD_MAX_RETRIES"] = "5"
	c, err = Load(env(m))
	if err != nil {
		t.Fatal(err)
	}
	if u := c.Upload; u.MaxBytes != 1<<30 || u.PartSize != 8<<20 || u.MaxOpen != 3 || u.SessionTTL != 48*time.Hour || u.MaxRetries != 5 {
		t.Fatalf("overrides not applied: %+v", u)
	}
}

func TestUploadLimitsRejectBadValues(t *testing.T) {
	bad := map[string][]string{
		"UPLOAD_MAX_BYTES":       {"abc", "0", "1048575", "5368709121"},
		"UPLOAD_PART_SIZE_BYTES": {"1048576", "5242879", "1073741825", "-1"},
		"UPLOAD_MAX_OPEN":        {"0", "51", "many"},
		"UPLOAD_SESSION_TTL":     {"30m", "169h", "soon", "0"},
		"UPLOAD_MAX_RETRIES":     {"-1", "11", "often"},
	}
	for name, values := range bad {
		for _, v := range values {
			m := devBase()
			m[name] = v
			if _, err := Load(env(m)); err == nil || !strings.Contains(err.Error(), name) {
				t.Errorf("%s=%q should be rejected naming the variable, got %v", name, v, err)
			}
		}
	}
	// Boundary values are accepted.
	m := devBase()
	m["UPLOAD_MAX_BYTES"], m["UPLOAD_PART_SIZE_BYTES"], m["UPLOAD_MAX_OPEN"], m["UPLOAD_SESSION_TTL"], m["UPLOAD_MAX_RETRIES"] = "5368709120", "5242880", "50", "168h", "10"
	if _, err := Load(env(m)); err != nil {
		t.Errorf("boundary values rejected: %v", err)
	}
	m["UPLOAD_MAX_RETRIES"] = "0" // 0 switches retry off
	if c, err := Load(env(m)); err != nil || c.Upload.MaxRetries != 0 {
		t.Errorf("UPLOAD_MAX_RETRIES=0 must be accepted and kept: %v %+v", err, c.Upload)
	}
}
