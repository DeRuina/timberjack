//go:build !timberjack_nozstd

package timberjack

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/klauspost/compress/zstd"
)

func readZstdFile(t *testing.T, path string) []byte {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", path, err)
	}
	defer f.Close()
	rd, err := zstd.NewReader(f)
	if err != nil {
		t.Fatalf("zstd.NewReader: %v", err)
	}
	defer rd.Close()
	data, err := io.ReadAll(rd)
	if err != nil {
		t.Fatalf("read zstd stream: %v", err)
	}
	return data
}

func TestZstdCompression_SizeRotate_DefaultNaming(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "app.log")

	oldMB := megabyte
	megabyte = 1
	t.Cleanup(func() { megabyte = oldMB })

	l := &Logger{
		Filename:    logPath,
		MaxSize:     10,     // bytes (since megabyte=1)
		Compression: "zstd", // enable zstd
	}
	defer l.Close()

	// 6 bytes per write; two writes => 12 > 10 => rotation
	msg := []byte("HELLO\n")
	if _, err := l.Write(msg); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := l.Write(msg); err != nil {
		t.Fatalf("second write: %v", err)
	}

	// Wait for mill to compress the rotated file.
	zstFile, err := waitForFileWithSuffix(t, dir, ".log.zst", 2*time.Second)
	if err != nil {
		t.Fatalf("expected a .log.zst rotated file in %s", dir)
	}

	got := readZstdFile(t, zstFile)
	if !bytes.Equal(got, msg) {
		t.Fatalf("zstd content mismatch: got %q want %q", string(got), string(msg))
	}
}

func TestZstdCompression_SizeRotate_AppendAfterExt(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "service.log")

	oldMB := megabyte
	megabyte = 1
	t.Cleanup(func() { megabyte = oldMB })

	l := &Logger{
		Filename:           logPath,
		MaxSize:            9, // two 5-byte writes => 10 > 9 => rotation
		Compression:        "zstd",
		AppendTimeAfterExt: true,
	}
	defer l.Close()

	msg := []byte("LINE\n") // 5 bytes
	if _, err := l.Write(msg); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := l.Write(msg); err != nil {
		t.Fatalf("second write: %v", err)
	}

	// Wait for .zst (append-after-ext form ends with just ".zst").
	zstFile, err := waitForFileWithSuffix(t, dir, ".zst", 2*time.Second)
	if err != nil {
		t.Fatalf("expected a .zst rotated file in %s", dir)
	}
	base := filepath.Base(zstFile)
	if !strings.Contains(base, ".log-") || !strings.Contains(base, "-size") {
		t.Fatalf("unexpected rotated filename %q; want '.log-<ts>-size.zst'", base)
	}

	got := readZstdFile(t, zstFile)
	if !bytes.Equal(got, msg) {
		t.Fatalf("zstd content mismatch: got %q want %q", string(got), string(msg))
	}
}

func TestCompressionPrecedence_ZstdBeatsLegacyCompress(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "prec.log")

	oldMB := megabyte
	megabyte = 1
	t.Cleanup(func() { megabyte = oldMB })

	l := &Logger{
		Filename:    logPath,
		MaxSize:     9,      // two 5-byte writes => 10 > 9 => rotation
		Compression: "zstd", // should win
		Compress:    true,   // legacy would have chosen gzip, but must be ignored
	}
	defer l.Close()

	msg := []byte("LINE\n") // 5 bytes
	if _, err := l.Write(msg); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if _, err := l.Write(msg); err != nil {
		t.Fatalf("second write: %v", err)
	}

	// Should produce .zst, not .gz.
	if _, err := waitForFileWithSuffix(t, dir, ".zst", 2*time.Second); err != nil {
		t.Fatalf("expected .zst file; none found")
	}
	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), ".gz") {
			t.Fatalf("found unexpected gzip file: %s", e.Name())
		}
	}
}

// TestCompressLogFile_ZstdCloseErrorEmptySrc is the zstd counterpart of
// TestCompressLogFile_GzipCloseErrorEmptySrc: empty source, pre-closed fd.
// Covers the "copyErr==nil && closeErr!=nil" zstd path (lines 1239-1241).
func TestCompressLogFile_ZstdCloseErrorEmptySrc(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "empty.log")
	_ = os.WriteFile(src, []byte{}, 0o644)
	dst := filepath.Join(dir, "empty.log.zst")

	closedFile, err := os.CreateTemp(dir, "invalid-fd")
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}
	closedFile.Close()

	origOpenFile := osOpenFile
	osOpenFile = func(_ string, _ int, _ os.FileMode) (*os.File, error) {
		return closedFile, nil
	}
	defer func() { osOpenFile = origOpenFile }()

	l := &Logger{}
	l.resolveConfigLocked()

	if err := l.compressLogFile(src, dst); err == nil {
		t.Fatal("expected error from zstd enc.Close on invalid fd, got nil")
	}
}
