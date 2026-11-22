package worker

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Scanner inspects an original before it is allowed any further into the pipeline (B10).
// The worker is fail-closed: when a scanner is configured and it cannot give an answer, the job is
// retried and, once attempts run out, the video FAILS. An unscanned upload is never published.
//
// REDXTUBE does not bundle a CSAM or malware engine. Hash-matching services (PhotoDNA, CSAI Match,
// Cloudflare's CSAM tool, NCMEC lists...) need a vendor agreement and credentials, so they plug in
// here: write a Scanner, or use ExecScanner to call any command line tool.
type Scanner interface {
	// Scan returns infected=true with a short human-readable detail when the file must be removed.
	Scan(ctx context.Context, path string) (infected bool, detail string, err error)
}

// ExecScanner runs an external program. "{file}" in any argument is replaced with the path (there is
// no shell, so the path cannot inject anything). Exit status 0 = clean, 1 = match, anything else
// (including a missing binary or a timeout) = the scanner failed. This is the clamscan convention:
//
//	SCANNER_COMMAND="clamscan --no-summary {file}"
type ExecScanner struct {
	Args    []string
	Timeout time.Duration
	// Env lists the only KEY=VALUE pairs the scanner receives besides a minimal PATH and locale: the worker's own
	// environment (database and storage credentials) is never passed on.
	Env []string
}

func ParseScannerCommand(s string) (*ExecScanner, error) {
	args := strings.Fields(s)
	if len(args) == 0 {
		return nil, errors.New("empty scanner command")
	}
	found := false
	for _, a := range args {
		if strings.Contains(a, "{file}") {
			found = true
		}
	}
	if !found {
		return nil, errors.New(`scanner command must contain "{file}"`)
	}
	return &ExecScanner{Args: args, Timeout: 30 * time.Minute}, nil
}

func (e *ExecScanner) Scan(ctx context.Context, path string) (bool, string, error) {
	args := make([]string, len(e.Args))
	for i, a := range e.Args {
		args[i] = strings.ReplaceAll(a, "{file}", path)
	}
	cctx, cancel := context.WithTimeout(ctx, e.Timeout)
	defer cancel()
	cmd := exec.CommandContext(cctx, args[0], args[1:]...)
	out := &tailWriter{max: 8192}
	cmd.Stdout, cmd.Stderr = out, out
	isolate(cmd, e.Env...)
	err := cmd.Run()
	tail := sanitizeDiag(out.String())
	if err == nil {
		return false, "", nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 && cctx.Err() == nil {
		return true, tail, nil
	}
	if ctx.Err() != nil {
		return false, "", ctx.Err()
	}
	return false, "", fmt.Errorf("scanner failed: %w: %s", err, tail)
}

var scannerEnvKey = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,63}$`)

// ParseScannerEnv parses SCANNER_ENV, a comma-separated list of KEY=VALUE pairs that a scanner is allowed to see
// (for example "CLAMD_CONF=/etc/clamd.conf,LANG=C"). Values must not contain commas. The worker's own environment
// is never passed to a scanner; this list is the only way in.
func ParseScannerEnv(s string) ([]string, error) {
	if strings.TrimSpace(s) == "" {
		return nil, nil
	}
	parts := strings.Split(s, ",")
	if len(parts) > 32 {
		return nil, errors.New("at most 32 variables")
	}
	seen := map[string]bool{}
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		k, v, ok := strings.Cut(p, "=")
		switch {
		case !ok || k == "":
			return nil, fmt.Errorf("%q is not KEY=VALUE", p)
		case !scannerEnvKey.MatchString(k):
			return nil, fmt.Errorf("%q is not a valid variable name", k)
		case seen[k]:
			return nil, fmt.Errorf("%s is listed twice", k)
		case strings.ContainsAny(v, "\x00\r\n"):
			return nil, fmt.Errorf("the value of %s contains a control character", k)
		}
		seen[k] = true
		out = append(out, k+"="+v)
	}
	return out, nil
}
