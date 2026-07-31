package timberjack

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Benchmarks for the retention pass.
//
// oldLogFiles is run by millRunOnce, which happens after every rotation and on every
// openExistingOrNew. Its cost is therefore paid once per rotation and once per Logger
// that opens a file, and it scales with the number of entries in the output directory.
//
//	go test -run '^$' -bench 'BenchmarkOldLogFiles|BenchmarkMillRunOnce' -benchmem -count=6 . > before.txt
//	benchstat before.txt after.txt

// seedBackups fills dir with backups files named the way rotation names them, plus others
// files that do not match the backup pattern at all.
func seedBackups(tb testing.TB, dir, base string, backups, others int) {
	tb.Helper()

	ext := filepath.Ext(base)
	prefix := base[:len(base)-len(ext)] + "-"

	ts := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := 0; i < backups; i++ {
		name := prefix + ts.Add(time.Duration(i)*time.Minute).Format(backupTimeFormat) + "-size" + ext
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o600); err != nil {
			tb.Fatal(err)
		}
	}
	for i := 0; i < others; i++ {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("unrelated-%d.txt", i)), nil, 0o600); err != nil {
			tb.Fatal(err)
		}
	}
}

// BenchmarkOldLogFiles measures listing and sorting the backups in the log directory.
//
// The last case is the interesting one: those 900 files are not backups and are discarded,
// so any per-entry work done before the name is checked is pure waste.
func BenchmarkOldLogFiles(b *testing.B) {
	cases := []struct {
		name            string
		backups, others int
	}{
		{name: "10_backups", backups: 10},
		{name: "100_backups", backups: 100},
		{name: "1000_backups", backups: 1000},
		{name: "100_backups_900_unrelated", backups: 100, others: 900},
	}

	for _, tc := range cases {
		tc := tc
		b.Run(tc.name, func(b *testing.B) {
			dir := b.TempDir()
			const base = "server.log"
			seedBackups(b, dir, base, tc.backups, tc.others)

			l := &Logger{Filename: filepath.Join(dir, base)}
			l.resolveConfigLocked()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				files, err := l.oldLogFiles()
				if err != nil {
					b.Fatal(err)
				}
				// Guards against a change that speeds this up by finding fewer backups.
				if len(files) != tc.backups {
					b.Fatalf("got %d backups, want %d", len(files), tc.backups)
				}
			}
		})
	}
}

// BenchmarkMillRunOnce measures a whole retention pass. MaxBackups is set above the number
// of seeded files so that nothing is ever removed and every iteration does the same work.
func BenchmarkMillRunOnce(b *testing.B) {
	for _, backups := range []int{100, 1000} {
		backups := backups
		b.Run(fmt.Sprintf("%d_backups", backups), func(b *testing.B) {
			dir := b.TempDir()
			const base = "server.log"
			seedBackups(b, dir, base, backups, 0)

			l := &Logger{
				Filename:   filepath.Join(dir, base),
				MaxBackups: backups + 1,
			}
			l.resolveConfigLocked()

			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if err := l.millRunOnce(); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
