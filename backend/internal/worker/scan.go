package worker

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os/exec"
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
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	detach(cmd)
	err := cmd.Run()
	tail := strings.TrimSpace(out.String())
	if len(tail) > 300 {
		tail = tail[:300]
	}
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
