// Package timberjack provides a rolling logger with size-based and time-based rotation.
//
// timberjack is designed to be a simple, pluggable component in a logging infrastructure.
// It automatically handles file rotation based on configured maximum file size (MaxSize)
// or elapsed time (RotationInterval), without requiring any external dependencies.
//
// Import:
//
//	import "github.com/DeRuina/timberjack"
//
// timberjack is compatible with any logging package that writes to an io.Writer,
// including the standard library's log package.
//
// timberjack assumes that only a single process is writing to the output files.
// Using the same Logger configuration from multiple processes on the same machine
// may result in improper behavior.
//
// Source code: https://github.com/DeRuina/timberjack
package timberjack

import (
	"compress/gzip"
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	backupTimeFormat = "2006-01-02T15-04-05.000"
	compressSuffix   = ".gz"
	defaultMaxSize   = 100
)

// ensure we always implement io.WriteCloser
var _ io.WriteCloser = (*Logger)(nil)

// Logger is an io.WriteCloser that writes to the specified filename.
//
// Logger opens or creates the logfile on the first Write.
// If the file exists and is smaller than MaxSize megabytes, timberjack will open and append to that file.
// If the file's size exceeds MaxSize, or if the configured RotationInterval has elapsed since the last rotation,
// the file is closed, renamed with a timestamp, and a new logfile is created using the original filename.
//
// Thus, the filename you give Logger is always the "current" log file.
//
// Backups use the log file name given to Logger, in the form:
// `name-timestamp.ext` where `name` is the filename without the extension,
// `timestamp` is the time of rotation formatted as `2006-01-02T15-04-05.000`, and `ext` is the original extension.
// For example, if your Logger.Filename is `/var/log/foo/server.log`, a backup created at 6:30pm on Nov 11 2016
// would use the filename `/var/log/foo/server-2016-11-04T18-30-00.000.log`.
//
// # Cleaning Up Old Log Files
//
// Whenever a new logfile is created, old log files may be deleted based on MaxBackups and MaxAge.
// The most recent files (according to the timestamp) will be retained up to MaxBackups (or all files if MaxBackups is 0).
// Any files with a timestamp older than MaxAge days are deleted, regardless of MaxBackups.
// Note that the timestamp is the rotation time, not necessarily the last write time.
//
// If MaxBackups and MaxAge are both 0, no old log files will be deleted.
//
// timberjack assumes only a single process is writing to the log files at a time.
type Logger struct {
	// Filename is the file to write logs to.  Backup log files will be retained
	// in the same directory.  It uses <processname>-timberjack.log in
	// os.TempDir() if empty.
	Filename string `json:"filename" yaml:"filename"`

	// MaxSize is the maximum size in megabytes of the log file before it gets
	// rotated. It defaults to 100 megabytes.
	MaxSize int `json:"maxsize" yaml:"maxsize"`

	// MaxAge is the maximum number of days to retain old log files based on the
	// timestamp encoded in their filename.  Note that a day is defined as 24
	// hours and may not exactly correspond to calendar days due to daylight
	// savings, leap seconds, etc. The default is not to remove old log files
	// based on age.
	MaxAge int `json:"maxage" yaml:"maxage"`

	// MaxBackups is the maximum number of old log files to retain.  The default
	// is to retain all old log files (though MaxAge may still cause them to get
	// deleted.)
	MaxBackups int `json:"maxbackups" yaml:"maxbackups"`

	// LocalTime determines if the time used for formatting the timestamps in
	// backup files is the computer's local time.  The default is to use UTC
	// time.
	LocalTime bool `json:"localtime" yaml:"localtime"`

	// Compress determines if the rotated log files should be compressed
	// using gzip. The default is not to perform compression.
	Compress bool `json:"compress" yaml:"compress"`

	// RotationInterval is the maximum duration between log rotations.
	// If the elapsed time since the last rotation exceeds this interval,
	// the log file is rotated, even if the file size has not reached MaxSize.
	// The minimum recommended value is 1 minute. If set to 0, time-based rotation is disabled.
	//
	// Example: RotationInterval = time.Hour * 24 will rotate logs daily.
	RotationInterval time.Duration `json:"rotationinterval" yaml:"rotationinterval"`

	// RotationFileSuffixTimeFormat specifies the layout used to format the timestamp
	// appended to rotated file names. If not set, the default format
	// "2006-01-02T15-04-05.000" will be used.
	//
	// Example: RotationFileSuffixTimeFormat = `2006-01-02-15-04-05` will generate rotated
	// backup files as <logfilename>-2006-01-02-15-04-05-<rotationCriteron>-timberjack.log.
	// Here, rotationCriterion could be `time` or `size`.
	RotationFileSuffixTimeFormat string `json:"rotationfilesuffixtimeformat" yaml:"rotationfilesuffixtimeformat"`

	// Internal
	size             int64
	file             *os.File
	lastRotationTime time.Time // lastRotationTime records the last time a rotation happened.
	logStartTime     time.Time // start time of the current logging period.

	mu        sync.Mutex
	millCh    chan bool
	startMill sync.Once

	isSuffixTimeFormatValidated bool // this flag helps prevent repeated validation checks on supplied layout
}

var (
	// currentTime exists so it can be mocked out by tests.
	currentTime = time.Now

	// os_Stat exists so it can be mocked out by tests.
	osStat = os.Stat

	// megabyte is the conversion factor between MaxSize and bytes.  It is a
	// variable so tests can mock it out and not need to write megabytes of data
	// to disk.
	megabyte = 1024 * 1024
)

// Write implements io.Writer.
// It writes the provided bytes to the current log file.
// If the log file exceeds MaxSize after writing, or if the configured RotationInterval has elapsed
// since the last rotation, the file is closed, renamed to include a timestamp, and a new log file is created
// using the original filename.
// If the size of a single write exceeds MaxSize, the write is rejected and an error is returned.
func (l *Logger) Write(p []byte) (n int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	writeLen := int64(len(p))
	if writeLen > l.max() {
		return 0, fmt.Errorf("write length %d exceeds maximum file size %d", writeLen, l.max())
	}

	if l.file == nil {
		if err = l.openExistingOrNew(len(p)); err != nil {
			return 0, err
		}
		l.lastRotationTime = currentTime()
	}

	// Time-based rotation
	if l.RotationInterval > 0 && currentTime().Sub(l.lastRotationTime) >= l.RotationInterval {
		if err := l.rotate(); err != nil {
			return 0, err
		}
		l.lastRotationTime = currentTime()
	}

	// Size-based rotation
	if l.size+writeLen > l.max() {
		if err := l.rotate(); err != nil {
			return 0, err
		}
	}

	n, err = l.file.Write(p)
	l.size += int64(n)
	return n, err
}

// Close implements io.Closer, and closes the current logfile.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.close()
}

// close closes the file if it is open.
func (l *Logger) close() error {
	if l.file == nil {
		return nil
	}
	err := l.file.Close()
	l.file = nil
	return err
}

// Rotate causes Logger to close the existing log file and immediately create a
// new one.  This is a helper function for applications that want to initiate
// rotations outside of the normal rotation rules, such as in response to
// SIGHUP.  After rotating, this initiates compression and removal of old log
// files according to the configuration.
func (l *Logger) Rotate() error {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.rotate()
}

// rotate closes the current file, moves it aside with a timestamp in the name,
// (if it exists), opens a new file with the original filename, and then runs
// post-rotation processing and removal.
func (l *Logger) rotate() error {
	if err := l.close(); err != nil {
		return err
	}
	if err := l.openNew(); err != nil {
		return err
	}
	l.mill()
	return nil
}

// getRotationFileSuffixTimeFormat returns format of timestamp that is used as suffix
// of the rotated file.
// If RotationFileSuffixTimeFormat field is not set during Logger creation, then
// default value of `2006-01-02T15-04-05.000`
func (l *Logger) getRotationFileSuffixTimeFormat() string {
	if !l.isSuffixTimeFormatValidated {
		if isValidSuffixTimeFormat(l.RotationFileSuffixTimeFormat) {
			l.isSuffixTimeFormatValidated = true
			return l.RotationFileSuffixTimeFormat
		}
		// supplied time format is invalid. So replacing it with backup format
		l.isSuffixTimeFormatValidated = true
		l.RotationFileSuffixTimeFormat = backupTimeFormat
	}
	return l.RotationFileSuffixTimeFormat
}

// openNew creates a new log file for writing.
// If an old log file already exists, it is moved aside by renaming it with a timestamp.
// This method assumes that the old file has already been closed.
func (l *Logger) openNew() error {
	err := os.MkdirAll(l.dir(), 0755)
	if err != nil {
		return fmt.Errorf("can't make directories for new logfile: %s", err)
	}

	name := l.filename()
	mode := os.FileMode(0600)
	info, err := osStat(name)
	if err == nil {
		mode = info.Mode()

		reason := "size"
		if l.shouldTimeRotate() {
			reason = "time"
		}

		l.logStartTime = currentTime()
		rotationSuffixTimeFormat := l.getRotationFileSuffixTimeFormat()
		newname := backupName(name, l.LocalTime, reason, l.logStartTime, rotationSuffixTimeFormat)
		if err := os.Rename(name, newname); err != nil {
			return fmt.Errorf("can't rename log file: %s", err)
		}

		if err := chown(name, info); err != nil {
			return err
		}
	} else {
		l.logStartTime = currentTime()
	}

	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("can't open new logfile: %s", err)
	}
	l.file = f
	l.size = 0
	return nil
}

// shouldTimeRotate checks if the time-based rotation interval has elapsed
// since the last rotation.
func (l *Logger) shouldTimeRotate() bool {
	if l.RotationInterval == 0 {
		return false
	}
	return time.Since(l.lastRotationTime) >= l.RotationInterval
}

// backupName creates a new backup filename by inserting a timestamp and a rotation reason
// ("time" or "size") between the filename prefix and the extension.
// It uses the local time if requested (otherwise UTC).
func backupName(name string, local bool, reason string, t time.Time, fileTimeFormat string) string {
	dir := filepath.Dir(name)
	filename := filepath.Base(name)
	ext := filepath.Ext(filename)
	prefix := filename[:len(filename)-len(ext)]

	if !local {
		t = t.UTC()
	}
	timestamp := t.Format(fileTimeFormat)
	return filepath.Join(dir, fmt.Sprintf("%s-%s-%s%s", prefix, timestamp, reason, ext))
}

// openExistingOrNew opens the existing logfile if it exists and the current write
// would not cause it to exceed MaxSize. If the file does not exist, or if writing
// would exceed MaxSize, the current file is rotated and a new logfile is created.
func (l *Logger) openExistingOrNew(writeLen int) error {
	l.mill()

	filename := l.filename()
	info, err := osStat(filename)
	if os.IsNotExist(err) {
		return l.openNew()
	}
	if err != nil {
		return fmt.Errorf("error getting log file info: %s", err)
	}

	if info.Size()+int64(writeLen) >= l.max() {
		return l.rotate()
	}

	file, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return l.openNew()
	}
	l.file = file
	l.size = info.Size()
	l.logStartTime = currentTime()
	return nil
}

// filename returns the current log filename, using the configured Filename,
// or a default based on the process name if Filename is empty.
func (l *Logger) filename() string {
	if l.Filename != "" {
		return l.Filename
	}
	name := filepath.Base(os.Args[0]) + "-timberjack.log"
	return filepath.Join(os.TempDir(), name)
}

// millRunOnce performs one cycle of compression and removal of old log files.
// If compression is enabled, uncompressed backups are compressed using gzip.
// Old backup files are deleted to enforce MaxBackups and MaxAge limits.
func (l *Logger) millRunOnce() error {
	if l.MaxBackups == 0 && l.MaxAge == 0 && !l.Compress {
		return nil
	}

	files, err := l.oldLogFiles()
	if err != nil {
		return err
	}

	var compress, remove []logInfo

	if l.MaxBackups > 0 && l.MaxBackups < len(files) {
		preserved := make(map[string]bool)
		var remaining []logInfo
		for _, f := range files {
			fn := strings.TrimSuffix(f.Name(), compressSuffix)
			preserved[fn] = true

			if len(preserved) > l.MaxBackups {
				remove = append(remove, f)
			} else {
				remaining = append(remaining, f)
			}
		}
		files = remaining
	}

	if l.MaxAge > 0 {
		diff := time.Duration(int64(24*time.Hour) * int64(l.MaxAge))
		cutoff := currentTime().Add(-1 * diff)

		var remaining []logInfo
		for _, f := range files {
			if f.timestamp.Before(cutoff) {
				remove = append(remove, f)
			} else {
				remaining = append(remaining, f)
			}
		}
		files = remaining
	}

	if l.Compress {
		for _, f := range files {
			if !strings.HasSuffix(f.Name(), compressSuffix) {
				compress = append(compress, f)
			}
		}
	}

	for _, f := range remove {
		_ = os.Remove(filepath.Join(l.dir(), f.Name()))
	}
	for _, f := range compress {
		fn := filepath.Join(l.dir(), f.Name())
		_ = compressLogFile(fn, fn+compressSuffix)
	}

	return nil
}

// millRun runs in a goroutine to manage post-rotation compression and removal
// of old log files.
func (l *Logger) millRun() {
	for range l.millCh {
		_ = l.millRunOnce()
	}
}

// mill performs post-rotation compression and removal of stale log files,
// starting the mill goroutine if necessary.
func (l *Logger) mill() {
	l.startMill.Do(func() {
		l.millCh = make(chan bool, 1)
		go l.millRun()
	})
	select {
	case l.millCh <- true:
	default:
	}
}

// oldLogFiles returns the list of backup log files stored in the same
// directory as the current log file, sorted by ModTime
func (l *Logger) oldLogFiles() ([]logInfo, error) {
	entries, err := os.ReadDir(l.dir())
	if err != nil {
		return nil, fmt.Errorf("can't read log file directory: %s", err)
	}
	logFiles := []logInfo{}

	prefix, ext := l.prefixAndExt()

	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		if t, err := l.timeFromName(info.Name(), prefix, ext); err == nil {
			logFiles = append(logFiles, logInfo{t, info})
			continue
		}
		if t, err := l.timeFromName(info.Name(), prefix, ext+compressSuffix); err == nil {
			logFiles = append(logFiles, logInfo{t, info})
			continue
		}
	}

	sort.Sort(byFormatTime(logFiles))

	return logFiles, nil
}

// timeFromName extracts the formatted timestamp from the backup filename by stripping off
// the filename's prefix, rotation reason suffix ("-time" or "-size"), and extension.
// This ensures correct parsing even if filenames include additional information after the timestamp.
func (l *Logger) timeFromName(filename, prefix, ext string) (time.Time, error) {
	if !strings.HasPrefix(filename, prefix) {
		return time.Time{}, errors.New("mismatched prefix")
	}
	if !strings.HasSuffix(filename, ext) {
		return time.Time{}, errors.New("mismatched extension")
	}

	base := filename[len(prefix) : len(filename)-len(ext)]

	idx := strings.LastIndex(base, "-")
	if idx == -1 {
		return time.Time{}, errors.New("invalid filename format")
	}

	ts := base[:idx]
	return time.Parse(backupTimeFormat, ts)
}

// max returns the maximum size in bytes of log files before rolling.
func (l *Logger) max() int64 {
	if l.MaxSize == 0 {
		return int64(defaultMaxSize * megabyte)
	}
	return int64(l.MaxSize) * int64(megabyte)
}

// dir returns the directory for the current filename.
func (l *Logger) dir() string {
	return filepath.Dir(l.filename())
}

// prefixAndExt returns the filename part and extension part from the Logger's
// filename.
func (l *Logger) prefixAndExt() (prefix, ext string) {
	filename := filepath.Base(l.filename())
	ext = filepath.Ext(filename)
	prefix = filename[:len(filename)-len(ext)] + "-"
	return prefix, ext
}

// isValidSuffixTimeFormat determines the validity of the `layout` as a format for time.Time.
// Counts the number of digits after `.` in the layout format and compares till that precision.
//
// Internally, it generates a time.Time value with required precision.
// This generated time.Time value is used to format time in the given `layout`.
// Using the formatted string, we construct a time.Time value to compare with original time.Time
func isValidSuffixTimeFormat(layout string) bool {
	if layout == "" {
		return false
	}
	// 2025-05-22 23:41:59.987654321 +0000 UTC
	now := time.Date(2025, 05, 22, 23, 41, 59, 987_654_321, time.UTC)

	layoutPrecision := countDigitsAfterDot(layout)

	now, err := truncateFractional(now, layoutPrecision)

	if err != nil {
		return false
	}
	formatted := now.Format(layout)
	parsedT, err := time.Parse(layout, formatted)
	return parsedT.Equal(now) && err == nil
}

// countDigitsAfterDot returns the number of consecutive digit characters
// immediately following the first '.' in the input.
// It skips all characters before the '.' and stops counting at the first non-digit
// character after the '.'.

// Example: `prefix.0012304123suffix` would return 10
// Example: `prefix.0012304_middle_123_suffix` would return 7
func countDigitsAfterDot(layout string) int {
	for i, ch := range layout {
		if ch == '.' {
			count := 0
			for _, c := range layout[i+1:] {
				if unicode.IsDigit(c) {
					count++
				} else {
					break
				}
			}
			return count
		}
	}
	return 0 // no '.' found or no digits after dot
}

// truncateFractional truncates time t to n fractional digits of seconds.
// n=0 → truncate to seconds, n=3 → milliseconds, n=6 → microseconds, etc.
func truncateFractional(t time.Time, n int) (time.Time, error) {
	if n < 0 || n > 9 {
		return time.Now(), errors.New("n must be between 0 and 9")
	}
	// number of nanoseconds to keep
	factor := math.Pow10(9 - n) // e.g. for n=3, factor=10^(9-3)=1,000,000

	nanos := t.Nanosecond()
	truncatedNanos := int((int64(nanos) / int64(factor)) * int64(factor))

	return time.Date(
		t.Year(), t.Month(), t.Day(),
		t.Hour(), t.Minute(), t.Second(),
		truncatedNanos,
		t.Location(),
	), nil
}

// compressLogFile compresses the given log file, removing the
// uncompressed log file if successful.
func compressLogFile(src, dst string) (err error) {
	f, err := os.Open(src)
	if err != nil {
		return fmt.Errorf("failed to open log file: %v", err)
	}
	defer f.Close()

	fi, err := osStat(src)
	if err != nil {
		return fmt.Errorf("failed to stat log file: %v", err)
	}

	if err := chown(dst, fi); err != nil {
		return fmt.Errorf("failed to chown compressed log file: %v", err)
	}

	gzf, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fi.Mode())
	if err != nil {
		return fmt.Errorf("failed to open compressed log file: %v", err)
	}
	defer gzf.Close()

	gz := gzip.NewWriter(gzf)

	defer func() {
		if err != nil {
			os.Remove(dst)
			err = fmt.Errorf("failed to compress log file: %v", err)
		}
	}()

	if _, err := io.Copy(gz, f); err != nil {
		return err
	}
	if err := gz.Close(); err != nil {
		return err
	}
	if err := gzf.Close(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Remove(src); err != nil {
		return err
	}

	return nil
}

// logInfo is a convenience struct to return the filename and its embedded
// timestamp.
type logInfo struct {
	timestamp time.Time
	os.FileInfo
}

// byFormatTime sorts by newest time formatted in the name.
type byFormatTime []logInfo

func (b byFormatTime) Less(i, j int) bool {
	return b[i].timestamp.After(b[j].timestamp)
}

func (b byFormatTime) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}

func (b byFormatTime) Len() int {
	return len(b)
}
