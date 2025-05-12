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
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
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

	// RotateAtMinutes defines specific minutes within an hour (0-59) to trigger a rotation.
	// For example, []int{0} for top of the hour, []int{0, 30} for top and half-past the hour.
	// Rotations are aligned to the clock minute (second 0).
	// This operates in addition to RotationInterval and MaxSize.
	// If both RotateAtMinutes and RotationInterval are configured, rotation will occur
	// if either condition is met.
	RotateAtMinutes []int `json:"rotateAtMinutes" yaml:"rotateAtMinutes"`

	// Internal
	size             int64
	file             *os.File
	lastRotationTime time.Time // lastRotationTime records the last time a rotation happened.
	logStartTime     time.Time // start time of the current logging period.

	mu sync.Mutex

	// For mill goroutine (backups, compression)
	millCh    chan bool
	startMill sync.Once

	// For scheduled rotation goroutine
	startScheduledRotationOnce sync.Once
	scheduledRotationQuitCh    chan struct{}
	scheduledRotationWg        sync.WaitGroup
	processedRotateAtMinutes   []int // Internal storage for sorted and validated RotateAtMinutes
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
// since the last rotation, or if a scheduled rotation time (RotateAtMinutes) has been reached,
// the file is closed, renamed to include a timestamp, and a new log file is created
// using the original filename.
// If the size of a single write exceeds MaxSize, the write is rejected and an error is returned.
func (l *Logger) Write(p []byte) (n int, err error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Ensure scheduled rotation loop is running if configured
	l.ensureScheduledRotationLoopRunning()

	writeLen := int64(len(p))
	if writeLen > l.max() {
		return 0, fmt.Errorf("write length %d exceeds maximum file size %d", writeLen, l.max())
	}

	if l.file == nil {
		if err = l.openExistingOrNew(len(p)); err != nil {
			return 0, err
		}
		// Initialize lastRotationTime only if it's zero (e.g. first ever open)
		// This prevents overwriting it if openExistingOrNew re-opened without rotation.
		if l.lastRotationTime.IsZero() {
			l.lastRotationTime = currentTime()
		}
	}

	// Time-based rotation
	if l.RotationInterval > 0 && currentTime().Sub(l.lastRotationTime) >= l.RotationInterval {
		if err := l.rotate("time"); err != nil {
			return 0, fmt.Errorf("interval rotation failed: %w", err)
		}
		l.lastRotationTime = currentTime()
	}

	// Size-based rotation
	if l.size+writeLen > l.max() {
		if err := l.rotate("size"); err != nil {
			return 0, fmt.Errorf("size rotation failed: %w", err)
		}
	}

	n, err = l.file.Write(p)
	l.size += int64(n)
	return n, err
}

// location returns the time.Location to use for timestamps.
func (l *Logger) location() *time.Location {
	if l.LocalTime {
		return time.Local
	}
	return time.UTC
}

// ensureScheduledRotationLoopRunning starts the scheduled rotation goroutine if configured and not already running.
func (l *Logger) ensureScheduledRotationLoopRunning() {
	if len(l.RotateAtMinutes) == 0 {
		return
	}

	l.startScheduledRotationOnce.Do(func() {
		// Validate and sort RotateAtMinutes once
		seenMinutes := make(map[int]bool)
		for _, m := range l.RotateAtMinutes {
			if m >= 0 && m <= 59 && !seenMinutes[m] { // Ensure minutes are valid and unique
				l.processedRotateAtMinutes = append(l.processedRotateAtMinutes, m)
				seenMinutes[m] = true
			}
		}
		if len(l.processedRotateAtMinutes) == 0 {
			fmt.Fprintf(os.Stderr, "timberjack: [%s] No valid minutes specified for RotateAtMinutes.\n", l.Filename)
			return // No valid minutes to schedule
		}
		sort.Ints(l.processedRotateAtMinutes) // Sort for predictable order

		l.scheduledRotationQuitCh = make(chan struct{})
		l.scheduledRotationWg.Add(1)
		go l.runScheduledRotations()
	})
}

// runScheduledRotations is the main loop for handling rotations at specific minute marks.
func (l *Logger) runScheduledRotations() {
	defer l.scheduledRotationWg.Done()

	if len(l.processedRotateAtMinutes) == 0 { // Should have been caught by ensureScheduledRotationLoopRunning
		return
	}

	timer := time.NewTimer(0) // Timer will be reset correctly in the loop
	if !timer.Stop() {
		// Drain the channel if the timer fired prematurely (e.g., duration was 0)
		select {
		case <-timer.C:
		default:
		}
	}

	for {
		now := currentTime() // Use the mockable currentTime
		nowInLocation := now.In(l.location())
		nextRotationAbsoluteTime := time.Time{}
		foundNextSlot := false

	determineNextSlot:
		for hourOffset := 0; hourOffset <= 24; hourOffset++ { // Check current hour, then next, up to 24 hours for robustness
			// Base time for the hour we are checking
			hourToCheck := time.Date(nowInLocation.Year(), nowInLocation.Month(), nowInLocation.Day(), nowInLocation.Hour(), 0, 0, 0, l.location()).Add(time.Duration(hourOffset) * time.Hour)

			for _, minuteMark := range l.processedRotateAtMinutes {
				candidateTime := time.Date(hourToCheck.Year(), hourToCheck.Month(), hourToCheck.Day(), hourToCheck.Hour(), minuteMark, 0, 0, l.location())

				if candidateTime.After(now) { // Found the earliest future slot
					nextRotationAbsoluteTime = candidateTime
					foundNextSlot = true
					break determineNextSlot
				}
			}
		}

		if !foundNextSlot {
			// This should not happen if processedRotateAtMinutes is valid and not empty,
			// as we search up to 24 hours ahead. Could indicate an issue with time or logic.
			fmt.Fprintf(os.Stderr, "timberjack: [%s] Could not determine next scheduled rotation time for %v with marks %v. Retrying calculation in 1 minute.\n", l.Filename, nowInLocation, l.processedRotateAtMinutes)
			select {
			case <-time.After(time.Minute): // Wait a bit before retrying calculation
				continue // Restart the outer loop
			case <-l.scheduledRotationQuitCh:
				return // Exit if closing
			}
		}

		sleepDuration := nextRotationAbsoluteTime.Sub(now)
		// fmt.Fprintf(os.Stderr, "timberjack: [%s] Next scheduled rotation: %v (sleeping for %v)\n", l.Filename, nextRotationAbsoluteTime, sleepDuration) // Debug
		timer.Reset(sleepDuration)

		select {
		case <-timer.C:
			// It's time for a scheduled rotation
			l.mu.Lock()
			// Only rotate if the last rotation time was before this scheduled mark.
			// This prevents redundant rotations if a size/interval based rotation happened
			// very close to, but just before, this scheduled time, for the same mark.
			if l.lastRotationTime.Before(nextRotationAbsoluteTime) {
				if err := l.rotate("time"); err != nil {
					fmt.Fprintf(os.Stderr, "timberjack: [%s] scheduled rotation failed: %v\n", l.Filename, err)
				} else {
					l.lastRotationTime = currentTime()
				}
			} else {
				// fmt.Fprintf(os.Stderr, "timberjack: [%s] Scheduled rotation for %v skipped, last rotation at %v was already past this mark.\n", l.Filename, nextRotationAbsoluteTime, l.lastRotationTime) // Debug
			}
			l.mu.Unlock()
			// Loop will continue and recalculate the next slot from the new "now"

		case <-l.scheduledRotationQuitCh:
			if !timer.Stop() {
				// If Stop() returns false, the timer has already fired or been stopped.
				// If it fired, its channel might have a value, so drain it.
				select {
				case <-timer.C:
				default:
				}
			}
			return // Exit goroutine
		}
	}
}

// Close implements io.Closer, and closes the current logfile.
func (l *Logger) Close() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	// Stop and wait for the scheduled rotation goroutine
	if l.scheduledRotationQuitCh != nil {
		// Check if quit channel is already closed to prevent panic
		// This select is a non-blocking read; if it's closed, it returns immediately.
		alreadyClosed := false
		select {
		case _, ok := <-l.scheduledRotationQuitCh:
			if !ok { // Channel is closed
				alreadyClosed = true
			}
		default: // Channel is open or nil (but we check for nil above)
		}

		if !alreadyClosed {
			close(l.scheduledRotationQuitCh)
		}
		l.scheduledRotationWg.Wait() // Wait for the goroutine to finish
	}

	// Stop the mill goroutine (original logic implicitly handled by millCh)
	// For robust shutdown, millCh would ideally be closed and waited upon with a WaitGroup too.
	// This is not explicitly shown in original Close and is outside current scope.
	// if l.millCh != nil {
	// close(l.millCh) // This would stop the millRun goroutine if it ranges on millCh
	// }

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
	// For manual rotation, we can use "manual" or "time".
	// Using "manual" makes it distinct in the filename if desired.
	// If you prefer it to look like other time-based rotations, use "time".
	return l.rotate("manual")
}

// rotate closes the current file, moves it aside with a timestamp in the name,
// (if it exists), opens a new file with the original filename, and then runs
// post-rotation processing and removal.
// l.mu must be held by the caller.
// Takes an explicit reason for the rotation.
func (l *Logger) rotate(reason string) error {
	if err := l.close(); err != nil {
		return err
	}
	// Pass the reason to openNew
	if err := l.openNew(reason); err != nil {
		return err
	}
	l.mill()
	return nil
}

// openNew creates a new log file for writing.
// If an old log file already exists, it is moved aside by renaming it with a timestamp.
// This method assumes that l.mu is held and the old file has already been closed.
func (l *Logger) openNew(reasonForBackup string) error {
	err := os.MkdirAll(l.dir(), 0755)
	if err != nil {
		return fmt.Errorf("can't make directories for new logfile: %s", err)
	}

	name := l.filename()
	mode := os.FileMode(0600)
	info, err := osStat(name)
	rotationTimeForBackup := currentTime() // Use a consistent time for this rotation event.

	if err == nil { // File exists, so we are rotating it
		mode = info.Mode()

		// THE REASON IS NOW PASSED IN DIRECTLY AND USED FOR THE BACKUP FILENAME
		newname := backupName(name, l.LocalTime, reasonForBackup, rotationTimeForBackup)
		if errRename := os.Rename(name, newname); errRename != nil {
			return fmt.Errorf("can't rename log file: %s", errRename)
		}

		// chown for the *new* log file, if applicable (platform-dependent)
		// Note: chown was moved after successful rename in original timberjack,
		// and should apply to the *new* file if it's about setting default perms,
		// or to the *renamed* file if it's about preserving old perms.
		// Original timberjack calls chown(name, info) which implies for the NEW file path
		// using old file's info if it existed.
		if errChown := chown(name, info); errChown != nil {
			// Log this error, but it might not be fatal for rotation itself
			fmt.Fprintf(os.Stderr, "timberjack: [%s] failed to chown new log file %s: %v\n", l.Filename, name, errChown)
		}
	}
	// else: file doesn't exist, so it's the first open or previous was deleted.
	// In this case, no backup is made, so reasonForBackup isn't used for naming.

	// This is the start time of the new/current log file segment.
	l.logStartTime = rotationTimeForBackup

	f, err := os.OpenFile(name, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return fmt.Errorf("can't open new logfile: %s", err)
	}
	l.file = f
	l.size = 0 // Reset size for the new file
	return nil
}

// shouldTimeRotate checks if the time-based rotation interval has elapsed
// since the last rotation.
func (l *Logger) shouldTimeRotate() bool {
	return l.shouldTimeRotateUsing(currentTime())
}

// shouldTimeRotateUsing checks interval against a specific time. (NEW HELPER for openNew)
func (l *Logger) shouldTimeRotateUsing(t time.Time) bool {
	if l.RotationInterval == 0 {
		return false
	}
	// Check if lastRotationTime is initialized; if not, assume no time rotation is due yet.
	if l.lastRotationTime.IsZero() {
		return false
	}
	return t.Sub(l.lastRotationTime) >= l.RotationInterval
}

// backupName creates a new backup filename by inserting a timestamp and a rotation reason
// ("time" or "size") between the filename prefix and the extension.
// It uses the local time if requested (otherwise UTC).
func backupName(name string, local bool, reason string, t time.Time) string {
	dir := filepath.Dir(name)
	filename := filepath.Base(name)
	ext := filepath.Ext(filename)
	prefix := filename[:len(filename)-len(ext)]

	loc := time.UTC
	if local {
		loc = time.Local
	}
	timestamp := t.In(loc).Format(backupTimeFormat)
	return filepath.Join(dir, fmt.Sprintf("%s-%s-%s%s", prefix, timestamp, reason, ext))
}

// openExistingOrNew opens the existing logfile if it exists and the current write
// would not cause it to exceed MaxSize. If the file does not exist, or if writing
// would exceed MaxSize, the current file is rotated and a new logfile is created.
// l.mu must be held by the caller.
func (l *Logger) openExistingOrNew(writeLen int) error {
	l.mill()

	filename := l.filename()
	info, err := osStat(filename)
	if os.IsNotExist(err) {
		// File doesn't exist, so openNew is creating a new file, not a backup.
		// The 'reason' here won't affect a backup filename. "initial" or "new" is fine.
		return l.openNew("initial")
	}
	if err != nil {
		return fmt.Errorf("error getting log file info: %s", err)
	}

	if info.Size()+int64(writeLen) >= l.max() {
		return l.rotate("size")
	}

	file, err := os.OpenFile(filename, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		// If opening existing fails, try to create a new one.
		// Again, 'reason' here won't affect a backup filename.
		return l.openNew("initial")
	}
	l.file = file
	l.size = info.Size()
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
		// Files are sorted newest first. We want to keep MaxBackups newest files.
		for i, f := range files {
			if i < l.MaxBackups {
				fn := strings.TrimSuffix(f.Name(), compressSuffix)
				preserved[fn] = true
				remaining = append(remaining, f)
			} else {
				// If already have MaxBackups, the rest are candidates for removal
				remove = append(remove, f)
			}
		}
		files = remaining
	}

	if l.MaxAge > 0 {
		diff := time.Duration(int64(24*time.Hour) * int64(l.MaxAge))
		cutoff := currentTime().Add(-1 * diff)

		var remaining []logInfo
		// Add to 'remove' list, don't overwrite files selected by MaxBackups logic.
		var currentRemoveMap = make(map[string]bool)
		for _, f := range remove {
			currentRemoveMap[f.Name()] = true
		}

		for _, f := range files { // files here are those not removed by MaxBackups
			if f.timestamp.Before(cutoff) {
				if !currentRemoveMap[f.Name()] { // Avoid duplicates
					remove = append(remove, f)
					currentRemoveMap[f.Name()] = true
				}
			} else {
				remaining = append(remaining, f)
			}
		}
		files = remaining // files now contains only those to be kept after age check (and MaxBackups)
	}

	if l.Compress {
		// Compress files that are to be kept (not in the 'remove' list)
		// and are not already compressed.
		var stillToKeepAndCompress []logInfo
		var currentRemoveMap = make(map[string]bool)
		for _, f := range remove {
			currentRemoveMap[f.Name()] = true
		}

		for _, f := range files { // These are files to be kept after MaxBackups and MaxAge
			if !strings.HasSuffix(f.Name(), compressSuffix) && !currentRemoveMap[f.Name()] {
				compress = append(compress, f)
			} else {
				stillToKeepAndCompress = append(stillToKeepAndCompress, f)
			}
		}
		// files = stillToKeepAndCompress // Not strictly necessary to reassign files here
	}

	for _, f := range remove {
		errRemove := os.Remove(filepath.Join(l.dir(), f.Name()))
		if errRemove != nil && !os.IsNotExist(errRemove) {
			fmt.Fprintf(os.Stderr, "timberjack: [%s] failed to remove old log file %s: %v\n", l.Filename, f.Name(), errRemove)
		}
	}
	for _, f := range compress { // Compress files that were kept but are not yet compressed
		fn := filepath.Join(l.dir(), f.Name())
		errCompress := compressLogFile(fn, fn+compressSuffix)
		if errCompress != nil {
			fmt.Fprintf(os.Stderr, "timberjack: [%s] failed to compress log file %s: %v\n", l.Filename, f.Name(), errCompress)
		}
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
			continue // Skip files we can't stat
		}

		// Attempt to parse timestamp from filename
		if t, err := l.timeFromName(info.Name(), prefix, ext); err == nil {
			logFiles = append(logFiles, logInfo{t, info})
			continue
		}
		// Attempt to parse timestamp from compressed filename
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

	// name-timestamp-reason.ext
	// We need to extract 'timestamp' part.
	trimmed := filename[len(prefix) : len(filename)-len(ext)]

	// The format is prefix + YYYY-MM-DDTHH-MM-SS.mmm + "-" + reason + ext
	// Find the last hyphen, which should precede the reason ("size" or "time").
	lastHyphenIdx := strings.LastIndex(trimmed, "-")
	if lastHyphenIdx == -1 {
		// This could happen if the filename format is just "name-timestamp.ext" without a reason.
		// For robustness, try parsing 'trimmed' directly if no reason suffix is found.
		// However, the backupName function always adds a reason.
		return time.Time{}, errors.New("malformed backup filename: missing reason separator")
	}

	timestampPart := trimmed[:lastHyphenIdx]
	// reasonPart := trimmed[lastHyphenIdx+1:] // Not used here, but this is how to get it.

	loc := time.UTC  // Timestamps in filenames are UTC by default unless LocalTime is true for backupName
	if l.LocalTime { // This is a guess; backupName uses l.LocalTime to decide UTC/Local for formatting.
		loc = time.Local
	}

	return time.ParseInLocation(backupTimeFormat, timestampPart, loc)
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

	// Ensure the compressed file has the same ownership and permissions
	// This chown call might be problematic if dst doesn't exist yet or user lacks perms.
	// Let's defer chown until after successful compression and file creation.

	gzf, err := os.OpenFile(dst, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, fi.Mode())
	if err != nil {
		return fmt.Errorf("failed to open compressed log file: %v", err)
	}
	defer func() { // Ensure gzf is closed, and dst is removed on error
		if errClose := gzf.Close(); err == nil && errClose != nil {
			err = fmt.Errorf("failed to close compressed file: %w", errClose)
		}
		if err != nil {
			if removeErr := os.Remove(dst); removeErr != nil && !os.IsNotExist(removeErr) {
				// Log or wrap error about failing to remove partial compressed file
				fmt.Fprintf(os.Stderr, "timberjack: failed to remove partially compressed file %s: %v\n", dst, removeErr)
			}
		}
	}()

	gz := gzip.NewWriter(gzf)

	if _, errCopy := io.Copy(gz, f); errCopy != nil {
		// gz.Close() might also return an error, but errCopy is primary.
		_ = gz.Close() // Attempt to close to flush, error ignored if already have one.
		return fmt.Errorf("failed to copy to gzip writer: %w", errCopy)
	}
	if errClose := gz.Close(); errClose != nil {
		return fmt.Errorf("failed to close gzip writer: %w", errClose)
	}

	// Close original file before attempting to chown or remove src
	if errClose := f.Close(); errClose != nil {
		// This is less critical if compression succeeded, but good to note.
		fmt.Fprintf(os.Stderr, "timberjack: failed to close source file %s after compression: %v\n", src, errClose)
	}

	// Apply chown to the newly created compressed file
	if errChown := chown(dst, fi); errChown != nil {
		// This is not fatal for compression itself, but log it.
		fmt.Fprintf(os.Stderr, "timberjack: failed to chown compressed log file %s: %v\n", dst, errChown)
	}

	if errRemove := os.Remove(src); errRemove != nil {
		return fmt.Errorf("failed to remove original log file %s after compression: %w", src, errRemove)
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
	// Ensure both timestamps are valid before comparing
	if b[i].timestamp.IsZero() && !b[j].timestamp.IsZero() {
		return false // Treat zero time as oldest
	}
	if !b[i].timestamp.IsZero() && b[j].timestamp.IsZero() {
		return true
	}
	if b[i].timestamp.IsZero() && b[j].timestamp.IsZero() {
		return false // Equal if both are zero
	}
	return b[i].timestamp.After(b[j].timestamp)
}

func (b byFormatTime) Swap(i, j int) {
	b[i], b[j] = b[j], b[i]
}

func (b byFormatTime) Len() int {
	return len(b)
}
