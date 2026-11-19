package handler_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Keeps storage/openapi.yaml and the code from drifting apart, without a YAML dependency (the spec is parsed line by
// line; it is validated as OpenAPI 3.0 separately with openapi-spec-validator):
//   - every operation in the spec is a route registered by handler/upload.go, and the other way round;
//   - every error code the spec documents exists in the code, and every code the upload service can return is documented.

const specPath = "../../../storage/openapi.yaml"

func readSpec(t *testing.T) (ops map[string][]string, codes map[string]bool) {
	t.Helper()
	raw, err := os.ReadFile(specPath)
	if err != nil {
		t.Skipf("OpenAPI file not found (%v): the check runs from a full checkout", err)
	}
	pathRe := regexp.MustCompile(`^  (/\S+):\s*$`)
	methodRe := regexp.MustCompile(`^    (get|post|put|patch|delete):\s*$`)
	codesRe := regexp.MustCompile(`^      x-error-codes: \[(.*)\]\s*$`)
	ops, codes = map[string][]string{}, map[string]bool{}
	var path, op string
	for _, line := range strings.Split(string(raw), "\n") {
		if m := pathRe.FindStringSubmatch(line); m != nil {
			path = m[1]
		} else if m := methodRe.FindStringSubmatch(line); m != nil {
			op = strings.ToUpper(m[1]) + " " + path
			ops[op] = nil
		} else if m := codesRe.FindStringSubmatch(line); m != nil && op != "" {
			for _, c := range strings.Split(m[1], ",") {
				c = strings.TrimSpace(c)
				ops[op] = append(ops[op], c)
				codes[c] = true
			}
		}
	}
	return ops, codes
}

func sources(t *testing.T, dirs ...string) string {
	t.Helper()
	var b strings.Builder
	for _, d := range dirs {
		files, _ := filepath.Glob(filepath.Join("..", d, "*.go"))
		for _, f := range files {
			if strings.HasSuffix(f, "_test.go") {
				continue
			}
			raw, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			b.Write(raw)
		}
	}
	return b.String()
}

func TestOpenAPIRoutesMatchTheHandler(t *testing.T) {
	ops, _ := readSpec(t)
	raw, err := os.ReadFile("upload.go")
	if err != nil {
		t.Fatal(err)
	}
	routes := map[string]bool{}
	for _, m := range regexp.MustCompile(`HandleFunc\("([A-Z]+) "\+v\+"([^"]+)"`).FindAllStringSubmatch(string(raw), -1) {
		routes[m[1]+" /api/v1"+m[2]] = true
	}
	if len(routes) < 8 {
		t.Fatalf("found only %d routes in upload.go: the pattern needs updating", len(routes))
	}
	for op := range ops {
		if op == "GET /ready" { // served by the health handler, covered by TestReadyReportsObjectStorageWithoutFailingReadiness
			continue
		}
		if !routes[op] {
			t.Errorf("the spec documents %q but upload.go does not register it", op)
		}
	}
	for r := range routes {
		if _, ok := ops[r]; !ok {
			t.Errorf("upload.go registers %q but the spec does not document it", r)
		}
	}
}

func TestOpenAPIErrorCodesMatchTheCode(t *testing.T) {
	_, documented := readSpec(t)
	corpus := sources(t, "service", "handler", "middleware", "apperr", "repository")
	for code := range documented {
		if !strings.Contains(corpus, `"`+code+`"`) {
			t.Errorf("the spec documents the error code %q, but no such code exists in the code", code)
		}
	}

	// codes of the other services (auth, social, catalog ...) are not part of the upload API, so only service/upload.go counts
	uploadSrc, _ := os.ReadFile("../service/upload.go")
	var missing []string
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`apperr\.(?:Conflict|NotFound|Forbidden|Unauthorized)\(\s*"([a-z_]+)"`),
		regexp.MustCompile(`apperr\.New\([^,]+,\s*"([a-z_]+)"`),
	} {
		for _, m := range re.FindAllStringSubmatch(string(uploadSrc), -1) {
			if !documented[m[1]] {
				missing = append(missing, m[1])
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("error codes returned by service/upload.go but not documented in the spec: %v", missing)
	}
}
