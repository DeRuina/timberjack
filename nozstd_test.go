//go:build timberjack_nozstd

package timberjack

import (
	"bytes"
	"compress/gzip"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestNoZstd_NewZstdWriterUnavailable verifies the stub encoder refuses to
// produce zstd output when built with the timberjack_nozstd tag.
func TestNoZstd_NewZstdWriterUnavailable(t *testing.T) {
	if zstdAvailable {
		t.Fatal("zstdAvailable = true under timberjack_nozstd")
	}
	if _, err := newZstdWriter(io.Discard); err == nil {
		t.Fatal("expected error from newZstdWriter without zstd support, got nil")
	}
}

// TestNoZstd_EffectiveCompressionFallsBackToGzip verifies "zstd" resolves to
// gzip when zstd support is compiled out.
func TestNoZstd_EffectiveCompressionFallsBackToGzip(t *testing.T) {
	l := &Logger{Compression: "zstd"}
	if got := l.effectiveCompression(); got != "gzip" {
		t.Fatalf("effectiveCompression() = %q, want %q", got, "gzip")
	}
}

// TestNoZstd_SizeRotateProducesGzip verifies a Logger configured for zstd
// writes readable .gz backups and no .zst files when zstd is compiled out.
func TestNoZstd_SizeRotateProducesGzip(t *testing.T) {
	dir := t.TempDir()
	logPath := filepath.Join(dir, "app.log")

	oldMB := megabyte
	megabyte = 1
	t.Cleanup(func() { megabyte = oldMB })

	l := &Logger{
		Filename:    logPath,
		MaxSize:     10, // bytes (since megabyte=1)
		Compression: "zstd",
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

	gzFile, err := waitForFileWithSuffix(t, dir, ".log.gz", 2*time.Second)
	if err != nil {
		t.Fatalf("expected a .log.gz rotated file in %s", dir)
	}

	f, err := os.Open(gzFile)
	if err != nil {
		t.Fatalf("open %s: %v", gzFile, err)
	}
	defer f.Close()
	rd, err := gzip.NewReader(f)
	if err != nil {
		t.Fatalf("gzip.NewReader: %v", err)
	}
	got, err := io.ReadAll(rd)
	if err != nil {
		t.Fatalf("read gzip stream: %v", err)
	}
	if !bytes.Equal(got, msg) {
		t.Fatalf("gzip content mismatch: got %q want %q", string(got), string(msg))
	}

	ents, _ := os.ReadDir(dir)
	for _, e := range ents {
		if strings.HasSuffix(e.Name(), zstdSuffix) {
			t.Fatalf("found unexpected zstd file: %s", e.Name())
		}
	}
}
