package main

import (
	"bytes"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The committed spec must match the code. Add a route and this fails until `go run ./cmd/openapi`.
func TestSpecIsUpToDate(t *testing.T) {
	want, err := Build("../..")
	if err != nil {
		t.Fatal(err)
	}
	have, err := os.ReadFile("../../docs/openapi.json")
	if err != nil {
		t.Fatalf("docs/openapi.json missing: run `go run ./cmd/openapi`: %v", err)
	}
	if !bytes.Equal(have, want) {
		t.Fatal("docs/openapi.json is out of date: run `go run ./cmd/openapi` (from backend/) and commit it")
	}
}

func TestSpecShape(t *testing.T) {
	b, err := Build("../..")
	if err != nil {
		t.Fatal(err)
	}
	var spec struct {
		OpenAPI string                               `json:"openapi"`
		Paths   map[string]map[string]map[string]any `json:"paths"`
	}
	if err := json.Unmarshal(b, &spec); err != nil {
		t.Fatal(err)
	}
	ops := 0
	ids := map[string]bool{}
	for p, methods := range spec.Paths {
		for m, op := range methods {
			ops++
			id, _ := op["operationId"].(string)
			if id == "" || ids[id] {
				t.Errorf("%s %s: missing or duplicate operationId %q", m, p, id)
			}
			ids[id] = true
		}
	}
	if spec.OpenAPI != "3.0.3" || ops < 90 {
		t.Fatalf("openapi=%s ops=%d (the API has ~100 routes)", spec.OpenAPI, ops)
	}
	// guards are detected from the code, not typed by hand
	for _, c := range []struct{ path, method, scheme string }{
		{"/api/v1/videos/{id}", "get", "ageCookie"},
		{"/api/v1/auth/me", "get", "sessionCookie"},
		{"/api/v1/admin/actions", "post", "sessionCookie"},
		{"/api/v1/studio/videos", "get", "sessionCookie"},
	} {
		op := spec.Paths[c.path][c.method]
		if op == nil {
			t.Errorf("route %s %s missing", c.method, c.path)
			continue
		}
		if !strings.Contains(toJSON(op["security"]), c.scheme) {
			t.Errorf("%s %s: security %s lacks %s", c.method, c.path, toJSON(op["security"]), c.scheme)
		}
	}
	if op := spec.Paths["/api/v1/age/verify"]["post"]; toJSON(op["security"]) != "[]" {
		t.Errorf("POST /age/verify must be open: %s", toJSON(op["security"]))
	}
	if spec.Paths["/stream/{token}/{key}"]["get"] == nil || spec.Paths["/health"]["get"] == nil {
		t.Error("stream and health routes must be documented")
	}
}

func toJSON(v any) string { b, _ := json.Marshal(v); return string(b) }
