package worker

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func script(t *testing.T, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "scan.sh")
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestExecScannerExitCodes(t *testing.T) {
	sh := script(t, `grep -q EICAR "$1" && { echo "Eicar-Test-Signature FOUND"; exit 1; }; [ -f "$1" ] || exit 2; exit 0`)
	sc, err := ParseScannerCommand(sh + " {file}")
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	// a path with spaces and shell metacharacters must be passed as one argument, never interpreted
	evil := filepath.Join(dir, "a b; touch pwned #.mp4")
	clean, bad := evil, filepath.Join(dir, "bad.mp4")
	_ = os.WriteFile(clean, []byte("just a video"), 0o600)
	_ = os.WriteFile(bad, []byte("xx EICAR xx"), 0o600)

	if inf, _, err := sc.Scan(context.Background(), clean); err != nil || inf {
		t.Fatalf("clean: infected=%v err=%v", inf, err)
	}
	if _, err := os.Stat(filepath.Join(dir, "pwned")); err == nil {
		t.Fatal("shell injection through the file name")
	}
	inf, detail, err := sc.Scan(context.Background(), bad)
	if err != nil || !inf || detail == "" {
		t.Fatalf("infected: %v %q %v", inf, detail, err)
	}
	if _, _, err := sc.Scan(context.Background(), filepath.Join(dir, "missing")); err == nil {
		t.Fatal("exit status 2 must be an error, not a clean verdict")
	}
	missing := &ExecScanner{Args: []string{"/no/such/scanner", "{file}"}, Timeout: time.Second}
	if inf, _, err := missing.Scan(context.Background(), clean); err == nil || inf {
		t.Fatal("a missing scanner binary must be an error (fail closed), never clean")
	}
}

func TestExecScannerTimeoutKillsProcessGroup(t *testing.T) {
	marker := filepath.Join(t.TempDir(), "child-survived")
	sh := script(t, `(sleep 3; touch `+marker+`) & sleep 60`)
	sc := &ExecScanner{Args: []string{sh, "{file}"}, Timeout: 300 * time.Millisecond}
	start := time.Now()
	inf, _, err := sc.Scan(context.Background(), "x")
	if err == nil || inf {
		t.Fatalf("timeout must be an error: infected=%v err=%v", inf, err)
	}
	if d := time.Since(start); d > 5*time.Second {
		t.Fatalf("hung scanner held the worker for %v", d)
	}
	time.Sleep(3500 * time.Millisecond)
	if _, err := os.Stat(marker); err == nil {
		t.Fatal("a child of the scanner survived the kill")
	}
}

func TestParseScannerCommand(t *testing.T) {
	for _, bad := range []string{"", "   ", "clamscan --no-summary"} {
		if _, err := ParseScannerCommand(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
	if sc, err := ParseScannerCommand("clamscan --no-summary {file}"); err != nil || sc.Args[0] != "clamscan" {
		t.Fatalf("%v", err)
	}
}

type stubScanner struct {
	infected bool
	err      error
	calls    int
}

func (s *stubScanner) Scan(context.Context, string) (bool, string, error) {
	s.calls++
	return s.infected, "Test-Signature FOUND", s.err
}

func (f *fakeStore) RemoveVideo(_ context.Context, _ Job, reason string) error {
	f.removed = reason
	return nil
}

func TestPipelineScannerIsFailClosed(t *testing.T) {
	needFFmpeg(t)
	const vid = "0190aaaa-bbbb-7ccc-8ddd-222222222222"
	src := makeVideo(t, "-f", "lavfi", "-i", "testsrc=size=320x240:rate=10", "-t", "2", "-c:v", "libx264", "-pix_fmt", "yuv420p")
	key := "uploads/" + vid + "/original.mp4"
	run := func(sc Scanner) (*fakeStore, error) {
		mem := newMem()
		mem.objs[key] = src
		st := &fakeStore{video: VideoRow{CreatorID: "c", Status: "PROCESSING", Moderation: "OK", OriginalKey: key, Size: int64(len(src))}}
		p := newPipe(t, st, mem)
		p.Scanner = sc
		return st, p.Run(context.Background(), Job{ID: "j", VideoID: vid, Type: "METADATA"})
	}

	// clean: pipeline continues
	if st, err := run(&stubScanner{}); err != nil || len(st.done) != 1 {
		t.Fatalf("clean scan blocked the pipeline: %v", err)
	}
	// match: video removed, no metadata saved, permanent + silent
	st, err := run(&stubScanner{infected: true})
	var je *JobError
	if !errors.As(err, &je) || je.Code != "blocked_content" || !je.Permanent || !je.Silent || st.removed == "" || len(st.done) != 0 {
		t.Fatalf("match: %v removed=%q done=%v", err, st.removed, st.done)
	}
	// scanner down: retryable error, nothing advances
	st, err = run(&stubScanner{err: errors.New("clamd unreachable")})
	if !errors.As(err, &je) || je.Permanent || je.Code != "scanner_unavailable" || len(st.done) != 0 {
		t.Fatalf("scanner error must be transient and block the upload: %v", err)
	}
}
