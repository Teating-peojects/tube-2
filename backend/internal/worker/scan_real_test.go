package worker

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// TestExecScannerWithARealClamscan runs the scanner hook against the real ClamAV binary when it is installed. The
// signature database is built here (the hash of the standard EICAR test string), so no network or freshclam is needed.
// The EICAR string is assembled at run time so this source file does not trip antivirus software.
func TestExecScannerWithARealClamscan(t *testing.T) {
	if _, err := exec.LookPath("clamscan"); err != nil {
		t.Skip("clamscan not installed")
	}
	dir := t.TempDir()
	eicar := []byte("X5O!P%@AP[4\\PZX54(P^)7CC)7}$" + "EICAR-STANDARD-ANTIVIRUS-TEST-FILE!$H+H*")
	sum := md5.Sum(eicar)
	db := filepath.Join(dir, "test.hdb")
	if err := os.WriteFile(db, []byte(fmt.Sprintf("%s:%d:Test-Signature\n", hex.EncodeToString(sum[:]), len(eicar))), 0o600); err != nil {
		t.Fatal(err)
	}
	bad, clean := filepath.Join(dir, "bad.bin"), filepath.Join(dir, "clean.bin")
	if err := os.WriteFile(bad, eicar, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(clean, []byte("an ordinary file"), 0o600); err != nil {
		t.Fatal(err)
	}
	sc, err := ParseScannerCommand("clamscan --database=" + db + " --no-summary {file}")
	if err != nil {
		t.Fatal(err)
	}
	sc.Timeout = time.Minute
	ctx := context.Background()

	infected, detail, err := sc.Scan(ctx, bad)
	if err != nil || !infected || detail == "" {
		t.Fatalf("the test virus must be reported: infected=%v detail=%q err=%v", infected, detail, err)
	}
	if infected, _, err = sc.Scan(ctx, clean); err != nil || infected {
		t.Fatalf("a clean file must pass: infected=%v err=%v", infected, err)
	}
	// fail closed: a scanner that cannot run (missing database, exit status 2) is an error, never "clean"
	broken, _ := ParseScannerCommand("clamscan --database=" + filepath.Join(dir, "missing.hdb") + " --no-summary {file}")
	broken.Timeout = time.Minute
	if infected, _, err = broken.Scan(ctx, clean); err == nil || infected {
		t.Fatalf("a scanner that fails to run must produce an error, got infected=%v err=%v", infected, err)
	}
}
