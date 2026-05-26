package timberjack

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestRotate_OpenNewFails(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping test when running as root")
	}
	badPath := "/bad/path/logfile.log"
	if _, err := os.Stat(badPath); err == nil {
		t.Skip("Skipping test that relies on non-existent path")
	}
	l := &Logger{
		Filename: badPath,
	}
	// force an invalid path to trigger openNew failure
	err := l.rotate("manual")
	if err == nil {
		t.Fatal("expected error from rotate due to invalid openNew")
	}
}

// TestCompressLogFilePreservesMode ensures that a compressed backup keeps the source log file's
// permissions even when a non-zero umask is in effect. compressLogFile creates the destination
// with srcInfo.Mode(), but without an explicit chmod the umask would mask bits — the same problem
// that was fixed for newly created log files in openNew.
func TestCompressLogFilePreservesMode(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping test when running as root") // root bypasses permission bits
	}

	// A umask that would strip group/other bits from the source mode if no chmod is applied.
	oldMask := syscall.Umask(0o077)
	defer syscall.Umask(oldMask)

	dir := t.TempDir()
	src := filepath.Join(dir, "foo.log")
	dst := src + compressSuffix

	if err := os.WriteFile(src, []byte("compress me"), 0o666); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}
	// Ensure the source really has mode 0o666 regardless of the umask set above.
	if err := os.Chmod(src, 0o666); err != nil {
		t.Fatalf("failed to chmod source file: %v", err)
	}

	l := &Logger{Filename: src}
	l.resolveConfigLocked()

	if err := l.compressLogFile(src, dst); err != nil {
		t.Fatalf("compressLogFile failed: %v", err)
	}

	// The compressed file must keep the source mode, not have bits masked out by the umask.
	hasPerm(dst, 0o666, t)
}

func TestCompressLogFile_CopyFails(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping test when running as root")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "bad.log")
	dst := src + ".gz"

	if err := os.WriteFile(src, []byte("data"), 0o200); err != nil { // write-only
		t.Fatalf("failed to create test file: %v", err)
	}
	defer os.Chmod(src, 0o644)

	originalStat := osStat
	osStat = func(name string) (os.FileInfo, error) {
		return os.Stat(src)
	}
	defer func() { osStat = originalStat }()

	l := &Logger{}
	// snapshot patched osStat
	l.resolveConfigLocked()

	err := l.compressLogFile(src, dst)
	if err == nil {
		t.Errorf("expected failure during compression, got: %v", err)
	}
}

func TestOpenNewDefaultPerm(t *testing.T) {
	// Ensure no bits get masked out.
	syscall.Umask(0o000)

	dir := makeTempDir("TestOpenNewDefaultPerm", t)
	defer os.RemoveAll(dir)

	l := &Logger{
		Filename: logFile(dir),
	}
	defer l.Close()

	_, err := l.Write([]byte("foo"))
	isNil(err, t)
	hasPerm(logFile(dir), 0o640, t)
}

func TestOpenNewCustomPerm(t *testing.T) {
	// Ensure no bits get masked out.
	syscall.Umask(0o000)

	dir := makeTempDir("TestOpenNewCustomPerm", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)
	l1 := &Logger{
		Filename: filename,
		FileMode: 0o747,
	}
	t.Cleanup(func() {
		l1.Close()
	})
	_, err := l1.Write([]byte("foo"))
	isNil(err, t)
	hasPerm(filename, 0o747, t)

	filename += ".1"
	l2 := &Logger{
		Filename: filename,
		FileMode: 0o200,
	}
	t.Cleanup(func() {
		l2.Close()
	})
	_, err = l2.Write([]byte("foo"))
	isNil(err, t)
	hasPerm(filename, 0o200, t)

	filename += ".2"
	l3 := &Logger{
		Filename: filename,
		FileMode: 0o666,
	}
	t.Cleanup(func() {
		l3.Close()
	})
	_, err = l3.Write([]byte("foo"))
	isNil(err, t)
	hasPerm(filename, 0o666, t)
}
