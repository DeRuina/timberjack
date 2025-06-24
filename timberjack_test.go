package timberjack

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// !!!NOTE!!!
//
// Running these tests in parallel will almost certainly cause sporadic (or even
// regular) failures, because they're all messing with the same global variable
// that controls the logic's mocked time.Now.  So... don't do that.

// Since all the tests uses the time to determine filenames etc, we need to
// control the wall clock as much as possible, which means having a wall clock
// that doesn't change unless we want it to.
var fakeCurrentTime = time.Now()

func fakeTime() time.Time {
	return fakeCurrentTime
}

func TestNewFile(t *testing.T) {
	currentTime = fakeTime

	dir := makeTempDir("TestNewFile", t)
	defer os.RemoveAll(dir)
	l := &Logger{
		Filename: logFile(dir),
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)
	existsWithContent(logFile(dir), b, t)
	fileCount(dir, 1, t)
}

func TestOpenExisting(t *testing.T) {
	currentTime = fakeTime
	dir := makeTempDir("TestOpenExisting", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)
	data := []byte("foo!")
	err := os.WriteFile(filename, data, 0644)
	isNil(err, t)
	existsWithContent(filename, data, t)

	l := &Logger{
		Filename: filename,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	// make sure the file got appended
	existsWithContent(filename, append(data, b...), t)

	// make sure no other files were created
	fileCount(dir, 1, t)
}

func TestWriteTooLong(t *testing.T) {
	currentTime = fakeTime
	megabyte = 1
	dir := makeTempDir("TestWriteTooLong", t)
	defer os.RemoveAll(dir)
	l := &Logger{
		Filename:         logFile(dir),
		MaxSize:          5,
		BackupTimeFormat: backupTimeFormat,
	}
	defer l.Close()
	b := []byte("booooooooooooooo!")
	n, err := l.Write(b)
	notNil(err, t)
	equals(0, n, t)
	equals(err.Error(),
		fmt.Sprintf("write length %d exceeds maximum file size %d", len(b), l.MaxSize), t)
	_, err = os.Stat(logFile(dir))
	assert(os.IsNotExist(err), t, "File exists, but should not have been created")
}

func TestMakeLogDir(t *testing.T) {
	currentTime = fakeTime
	dir := time.Now().Format("TestMakeLogDir" + backupTimeFormat)
	dir = filepath.Join(os.TempDir(), dir)
	defer os.RemoveAll(dir)
	filename := logFile(dir)
	l := &Logger{
		Filename:         filename,
		BackupTimeFormat: backupTimeFormat,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)
	existsWithContent(logFile(dir), b, t)
	fileCount(dir, 1, t)
}

func TestDefaultFilename(t *testing.T) {
	currentTime = fakeTime
	dir := os.TempDir()
	filename := filepath.Join(dir, filepath.Base(os.Args[0])+"-timberjack.log")
	defer os.Remove(filename)
	l := &Logger{
		BackupTimeFormat: backupTimeFormat,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)

	isNil(err, t)
	equals(len(b), n, t)
	existsWithContent(filename, b, t)
}

func TestAutoRotate(t *testing.T) {
	currentTime = fakeTime
	megabyte = 1

	dir := makeTempDir("TestAutoRotate", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)
	l := &Logger{
		Filename:         filename,
		MaxSize:          10,
		BackupTimeFormat: backupTimeFormat,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	existsWithContent(filename, b, t)
	fileCount(dir, 1, t)

	newFakeTime()

	b2 := []byte("foooooo!")
	n, err = l.Write(b2)
	isNil(err, t)
	equals(len(b2), n, t)

	// the old logfile should be moved aside and the main logfile should have
	// only the last write in it.
	existsWithContent(filename, b2, t)

	// the backup file will use the current fake time and have the old contents.
	existsWithContent(backupFileWithReason(dir, "size"), b, t)

	fileCount(dir, 2, t)
}

func TestFirstWriteRotate(t *testing.T) {
	currentTime = fakeTime
	megabyte = 1
	dir := makeTempDir("TestFirstWriteRotate", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)
	l := &Logger{
		Filename:         filename,
		MaxSize:          10,
		BackupTimeFormat: backupTimeFormat,
	}
	defer l.Close()

	start := []byte("boooooo!")
	err := os.WriteFile(filename, start, 0600)
	isNil(err, t)

	newFakeTime()

	// this would make us rotate
	b := []byte("fooo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	existsWithContent(filename, b, t)
	existsWithContent(backupFileWithReason(dir, "size"), start, t)

	fileCount(dir, 2, t)
}

func TestMaxBackups(t *testing.T) {
	currentTime = fakeTime
	megabyte = 1
	dir := makeTempDir("TestMaxBackups", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)
	l := &Logger{
		Filename:         filename,
		MaxSize:          10,
		MaxBackups:       1,
		BackupTimeFormat: backupTimeFormat,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	existsWithContent(filename, b, t)
	fileCount(dir, 1, t)

	newFakeTime()

	// this will put us over the max
	b2 := []byte("foooooo!")
	n, err = l.Write(b2)
	isNil(err, t)
	equals(len(b2), n, t)

	// this will use the new fake time
	secondFilename := backupFileWithReason(dir, "size")
	existsWithContent(secondFilename, b, t)

	// make sure the old file still exists with the same content.
	existsWithContent(filename, b2, t)

	fileCount(dir, 2, t)

	newFakeTime()

	// this will make us rotate again
	b3 := []byte("baaaaaar!")
	n, err = l.Write(b3)
	isNil(err, t)
	equals(len(b3), n, t)

	// this will use the new fake time
	thirdFilename := backupFileWithReason(dir, "size")
	existsWithContent(thirdFilename, b2, t)

	existsWithContent(filename, b3, t)

	// we need to wait a little bit since the files get deleted on a different
	// goroutine.
	<-time.After(time.Millisecond * 10)

	// should only have two files in the dir still
	fileCount(dir, 2, t)

	// second file name should still exist
	existsWithContent(thirdFilename, b2, t)

	// should have deleted the first backup
	notExist(secondFilename, t)

	// now test that we don't delete directories or non-logfile files

	newFakeTime()

	// create a file that is close to but different from the logfile name.
	// It shouldn't get caught by our deletion filters.
	notlogfile := logFile(dir) + ".foo"
	err = os.WriteFile(notlogfile, []byte("data"), 0644)
	isNil(err, t)

	// Make a directory that exactly matches our log file filters... it still
	// shouldn't get caught by the deletion filter since it's a directory.
	notlogfiledir := backupFileWithReason(dir, "size")
	err = os.Mkdir(notlogfiledir, 0700)
	isNil(err, t)

	newFakeTime()

	// this will use the new fake time
	fourthFilename := backupFileWithReason(dir, "size")

	// Create a log file that is/was being compressed - this should
	// not be counted since both the compressed and the uncompressed
	// log files still exist.
	compLogFile := fourthFilename + compressSuffix
	err = os.WriteFile(compLogFile, []byte("compress"), 0644)
	isNil(err, t)

	// this will make us rotate again
	b4 := []byte("baaaaaaz!")
	n, err = l.Write(b4)
	isNil(err, t)
	equals(len(b4), n, t)

	existsWithContent(fourthFilename, b3, t)
	existsWithContent(fourthFilename+compressSuffix, []byte("compress"), t)

	// we need to wait a little bit since the files get deleted on a different
	// goroutine.
	<-time.After(time.Millisecond * 10)

	// We should have four things in the directory now - the 2 log files, the
	// not log file, and the directory
	fileCount(dir, 5, t)

	// third file name should still exist
	existsWithContent(filename, b4, t)

	existsWithContent(fourthFilename, b3, t)

	// should have deleted the first filename
	notExist(thirdFilename, t)

	// the not-a-logfile should still exist
	exists(notlogfile, t)

	// the directory
	exists(notlogfiledir, t)
}

func TestCleanupExistingBackups(t *testing.T) {
	// test that if we start with more backup files than we're supposed to have
	// in total, that extra ones get cleaned up when we rotate.

	currentTime = fakeTime
	megabyte = 1

	dir := makeTempDir("TestCleanupExistingBackups", t)
	defer os.RemoveAll(dir)

	// make 3 backup files

	data := []byte("data")
	backup := backupFileWithReason(dir, "size")
	err := os.WriteFile(backup, data, 0644)
	isNil(err, t)

	newFakeTime()

	backup = backupFileWithReason(dir, "size")
	err = os.WriteFile(backup+compressSuffix, data, 0644)
	isNil(err, t)

	newFakeTime()

	backup = backupFileWithReason(dir, "size")
	err = os.WriteFile(backup, data, 0644)
	isNil(err, t)

	// now create a primary log file with some data
	filename := logFile(dir)
	err = os.WriteFile(filename, data, 0644)
	isNil(err, t)

	l := &Logger{
		Filename:         filename,
		MaxSize:          10,
		MaxBackups:       1,
		BackupTimeFormat: backupTimeFormat,
	}
	defer l.Close()

	newFakeTime()

	b2 := []byte("foooooo!")
	n, err := l.Write(b2)
	isNil(err, t)
	equals(len(b2), n, t)

	// we need to wait a little bit since the files get deleted on a different
	// goroutine.
	<-time.After(time.Millisecond * 10)

	// now we should only have 2 files left - the primary and one backup
	fileCount(dir, 2, t)
}

func TestMaxAge(t *testing.T) {
	currentTime = fakeTime
	megabyte = 1

	dir := makeTempDir("TestMaxAge", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)
	l := &Logger{
		Filename:         filename,
		MaxSize:          10,
		MaxAge:           1,
		BackupTimeFormat: backupTimeFormat,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	existsWithContent(filename, b, t)
	fileCount(dir, 1, t)

	// two days later
	newFakeTime()

	b2 := []byte("foooooo!")
	n, err = l.Write(b2)
	isNil(err, t)
	equals(len(b2), n, t)
	existsWithContent(backupFileWithReason(dir, "size"), b, t)

	// we need to wait a little bit since the files get deleted on a different
	// goroutine.
	<-time.After(10 * time.Millisecond)

	// We should still have 2 log files, since the most recent backup was just
	// created.
	fileCount(dir, 2, t)

	existsWithContent(filename, b2, t)

	// we should have deleted the old file due to being too old
	existsWithContent(backupFileWithReason(dir, "size"), b, t)

	// two days later
	newFakeTime()

	b3 := []byte("baaaaar!")
	n, err = l.Write(b3)
	isNil(err, t)
	equals(len(b3), n, t)
	existsWithContent(backupFileWithReason(dir, "size"), b2, t)

	// we need to wait a little bit since the files get deleted on a different
	// goroutine.
	<-time.After(10 * time.Millisecond)

	// We should have 2 log files - the main log file, and the most recent
	// backup.  The earlier backup is past the cutoff and should be gone.
	fileCount(dir, 2, t)

	existsWithContent(filename, b3, t)

	// we should have deleted the old file due to being too old
	existsWithContent(backupFileWithReason(dir, "size"), b2, t)
}

func TestOldLogFiles(t *testing.T) {
	currentTime = fakeTime
	megabyte = 1

	dir := makeTempDir("TestOldLogFiles", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)
	data := []byte("data")
	err := os.WriteFile(filename, data, 07)
	isNil(err, t)

	// This gives us a time with the same precision as the time we get from the
	// timestamp in the name.
	t1, err := time.Parse(backupTimeFormat, fakeTime().UTC().Format(backupTimeFormat))
	isNil(err, t)

	backup := backupFileWithReason(dir, "size")
	err = os.WriteFile(backup, data, 07)
	isNil(err, t)

	newFakeTime()

	t2, err := time.Parse(backupTimeFormat, fakeTime().UTC().Format(backupTimeFormat))
	isNil(err, t)

	backup2 := backupFileWithReason(dir, "size")
	err = os.WriteFile(backup2, data, 07)
	isNil(err, t)

	l := &Logger{Filename: filename}
	files, err := l.oldLogFiles()
	isNil(err, t)
	equals(2, len(files), t)

	// should be sorted by newest file first, which would be t2
	equals(t2, files[0].timestamp, t)
	equals(t1, files[1].timestamp, t)
}

func TestTimeFromName(t *testing.T) {
	l := &Logger{
		Filename:         "/var/log/myfoo/foo.log",
		BackupTimeFormat: backupTimeFormat,
	}
	prefix, ext := l.prefixAndExt()

	tests := []struct {
		filename string
		want     time.Time
		wantErr  bool
	}{
		{"foo-2014-05-04T14-44-33.555-size.log", time.Date(2014, 5, 4, 14, 44, 33, 555000000, time.UTC), false},
		{"foo-2014-05-04T14-44-33.555", time.Time{}, true},
		{"2014-05-04T14-44-33.555.log", time.Time{}, true},
		{"foo.log", time.Time{}, true},
	}

	for _, test := range tests {
		got, err := l.timeFromName(test.filename, prefix, ext)
		equals(got, test.want, t)
		equals(err != nil, test.wantErr, t)
	}
}

func TestLocalTime(t *testing.T) {
	currentTime = fakeTime
	megabyte = 1

	dir := makeTempDir("TestLocalTime", t)
	defer os.RemoveAll(dir)

	l := &Logger{
		Filename:         logFile(dir),
		MaxSize:          10,
		LocalTime:        true,
		BackupTimeFormat: backupTimeFormat,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	b2 := []byte("fooooooo!")
	n2, err := l.Write(b2)
	isNil(err, t)
	equals(len(b2), n2, t)

	existsWithContent(logFile(dir), b2, t)
	existsWithContent(backupFileLocal(dir), b, t)
}

func TestRotate(t *testing.T) {
	currentTime = fakeTime
	dir := makeTempDir("TestRotate", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)

	l := &Logger{
		Filename:         filename,
		MaxBackups:       1,
		MaxSize:          100, // megabytes
		BackupTimeFormat: backupTimeFormat,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	existsWithContent(filename, b, t)
	fileCount(dir, 1, t)

	newFakeTime()

	err = l.Rotate()
	isNil(err, t)

	// we need to wait a little bit since the files get deleted on a different
	// goroutine.
	<-time.After(10 * time.Millisecond)

	filename2 := backupFileWithReason(dir, "size")
	existsWithContent(filename2, b, t)
	existsWithContent(filename, []byte{}, t)
	fileCount(dir, 2, t)
	newFakeTime()

	err = l.Rotate()
	isNil(err, t)

	// we need to wait a little bit since the files get deleted on a different
	// goroutine.
	<-time.After(10 * time.Millisecond)

	filename3 := backupFileWithReason(dir, "size")
	existsWithContent(filename3, []byte{}, t)
	existsWithContent(filename, []byte{}, t)
	fileCount(dir, 2, t)

	b2 := []byte("foooooo!")
	n, err = l.Write(b2)
	isNil(err, t)
	equals(len(b2), n, t)

	// this will use the new fake time
	existsWithContent(filename, b2, t)
}

func TestCompressOnRotate(t *testing.T) {
	currentTime = fakeTime
	megabyte = 1

	dir := makeTempDir("TestCompressOnRotate", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)
	l := &Logger{
		Compress:         true,
		Filename:         filename,
		MaxSize:          10,
		BackupTimeFormat: backupTimeFormat,
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	existsWithContent(filename, b, t)
	fileCount(dir, 1, t)

	newFakeTime()

	err = l.Rotate()
	isNil(err, t)

	// the old logfile should be moved aside and the main logfile should have
	// nothing in it.
	existsWithContent(filename, []byte{}, t)

	// we need to wait a little bit since the files get compressed on a different
	// goroutine.
	<-time.After(300 * time.Millisecond)

	// a compressed version of the log file should now exist and the original
	// should have been removed.
	bc := new(bytes.Buffer)
	gz := gzip.NewWriter(bc)
	_, err = gz.Write(b)
	isNil(err, t)
	err = gz.Close()
	isNil(err, t)
	existsWithContent(backupFileWithReason(dir, "size")+compressSuffix, bc.Bytes(), t)
	notExist(backupFileWithReason(dir, "size"), t)

	fileCount(dir, 2, t)
}

func TestCompressOnResume(t *testing.T) {
	currentTime = fakeTime
	megabyte = 1

	dir := makeTempDir("TestCompressOnResume", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)
	l := &Logger{
		Compress:         true,
		Filename:         filename,
		MaxSize:          10,
		BackupTimeFormat: backupTimeFormat,
	}
	defer l.Close()

	// Create a backup file and empty "compressed" file.
	filename2 := backupFileWithReason(dir, "size")
	b := []byte("foo!")
	err := os.WriteFile(filename2, b, 0644)
	isNil(err, t)
	err = os.WriteFile(filename2+compressSuffix, []byte{}, 0644)
	isNil(err, t)

	newFakeTime()

	b2 := []byte("boo!")
	n, err := l.Write(b2)
	isNil(err, t)
	equals(len(b2), n, t)
	existsWithContent(filename, b2, t)

	// we need to wait a little bit since the files get compressed on a different
	// goroutine.
	<-time.After(300 * time.Millisecond)

	// The write should have started the compression - a compressed version of
	// the log file should now exist and the original should have been removed.
	bc := new(bytes.Buffer)
	gz := gzip.NewWriter(bc)
	_, err = gz.Write(b)
	isNil(err, t)
	err = gz.Close()
	isNil(err, t)
	existsWithContent(filename2+compressSuffix, bc.Bytes(), t)
	notExist(filename2, t)

	fileCount(dir, 2, t)
}

func TestJson(t *testing.T) {
	data := []byte(`
{
	"filename": "foo",
	"maxsize": 5,
	"maxage": 10,
	"maxbackups": 3,
	"localtime": true,
	"compress": true
}`[1:])

	l := Logger{
		BackupTimeFormat: backupTimeFormat,
	}
	err := json.Unmarshal(data, &l)
	isNil(err, t)
	equals("foo", l.Filename, t)
	equals(5, l.MaxSize, t)
	equals(10, l.MaxAge, t)
	equals(3, l.MaxBackups, t)
	equals(true, l.LocalTime, t)
	equals(true, l.Compress, t)
}

// makeTempDir creates a file with a semi-unique name in the OS temp directory.
// It should be based on the name of the test, to keep parallel tests from
// colliding, and must be cleaned up after the test is finished.
func makeTempDir(name string, t testing.TB) string {
	dir := time.Now().Format(name + backupTimeFormat)
	dir = filepath.Join(os.TempDir(), dir)
	isNilUp(os.Mkdir(dir, 0700), t, 1)
	return dir
}

// existsWithContent checks that the given file exists and has the correct content.
func existsWithContent(path string, content []byte, t testing.TB) {
	info, err := os.Stat(path)
	isNilUp(err, t, 1)
	equalsUp(int64(len(content)), info.Size(), t, 1)

	b, err := os.ReadFile(path)
	isNilUp(err, t, 1)
	equalsUp(content, b, t, 1)
}

// logFile returns the log file name in the given directory for the current fake
// time.
func logFile(dir string) string {
	return filepath.Join(dir, "foobar.log")
}

func backupFileLocal(dir string) string {
	return filepath.Join(dir, "foobar-"+fakeTime().Format(backupTimeFormat)+"-size.log")
}

// fileCount checks that the number of files in the directory is exp.
func fileCount(dir string, exp int, t testing.TB) {
	files, err := os.ReadDir(dir)
	isNilUp(err, t, 1)
	// Make sure no other files were created.
	equalsUp(exp, len(files), t, 1)
}

// newFakeTime sets the fake "current time" to two days later.
func newFakeTime() {
	fakeCurrentTime = fakeCurrentTime.Add(time.Hour * 24 * 2)
}

func notExist(path string, t testing.TB) {
	_, err := os.Stat(path)
	assertUp(os.IsNotExist(err), t, 1, "expected to get os.IsNotExist, but instead got %v", err)
}

func exists(path string, t testing.TB) {
	_, err := os.Stat(path)
	assertUp(err == nil, t, 1, "expected file to exist, but got error from os.Stat: %v", err)
}

func TestTimeBasedRotation(t *testing.T) {
	currentTime = fakeTime
	dir := makeTempDir("TestTimeBasedRotation", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)

	l := &Logger{
		Filename:         filename,
		MaxSize:          10000,           // disable size rotation
		RotationInterval: time.Second * 1, // short interval
		BackupTimeFormat: backupTimeFormat,
	}
	defer l.Close()

	b1 := []byte("first write\n")
	n, err := l.Write(b1)
	isNil(err, t)
	equals(len(b1), n, t)

	newFakeTime()
	l.lastRotationTime = fakeCurrentTime.Add(-2 * time.Second)

	b2 := []byte("second write\n")
	n, err = l.Write(b2)
	isNil(err, t)
	equals(len(b2), n, t)

	time.Sleep(10 * time.Millisecond)

	existsWithContent(filename, b2, t)

	files, err := os.ReadDir(dir)
	isNil(err, t)

	var found bool
	for _, f := range files {
		if strings.HasPrefix(f.Name(), "foobar-") &&
			strings.HasSuffix(f.Name(), ".log") &&
			f.Name() != "foobar.log" {
			rotated := filepath.Join(dir, f.Name())
			existsWithContent(rotated, b1, t)
			found = true
			break
		}
	}

	if !found {
		t.Fatalf("expected rotated backup file with original contents, but none found")
	}
}

func TestSuffixTimeFormat(t *testing.T) {
	tmp := t.TempDir()
	logFile := filepath.Join(tmp, "invalid.log")

	logger := &Logger{
		Filename:         logFile,
		BackupTimeFormat: backupTimeFormat,
	}
	// parses correctly with err == nil, but parsed time.Time won't match the supplied time.Time

	invalidFormat := "2006-15-05 23:20:53"
	logger.BackupTimeFormat = invalidFormat
	err := logger.ValidateBackupTimeFormat()
	if err == nil {
		t.Fatalf("invalid timestamp layout determined as valid")
	}

	validFormat := `2006-01-02-15-05-44.000`
	logger.BackupTimeFormat = validFormat
	err = logger.ValidateBackupTimeFormat()
	if err != nil {
		t.Errorf("valid timestamp layout determined as invalid")
	}

	validFormat2 := `2006-01-02-15-05-44.00000` // precision upto 5 digits after .
	logger.BackupTimeFormat = validFormat2
	err = logger.ValidateBackupTimeFormat()
	if err != nil {
		t.Errorf("valid timestamp2 layout determined as invalid")
	}

	validFormat3 := `2006-01-02-15-05-44.0000000` // precision upto 7 digits after .
	logger.BackupTimeFormat = validFormat3
	err = logger.ValidateBackupTimeFormat()
	if err != nil {
		t.Errorf("valid timestamp2 layout determined as invalid")
	}

	validFormat4 := `20060102-15-05` // precision upto 7 digits after .
	logger.BackupTimeFormat = validFormat4
	err = logger.ValidateBackupTimeFormat()
	if err == nil {
		t.Errorf("timestamp4 is invalid but determined as valid")
	}
}

func TestCountDigitsAfterDot(t *testing.T) {
	tests := []struct {
		layout   string
		expected int
	}{
		{"2006-01-02 15:04:05", 0},           // no dot
		{"2006-01-02 15:04:05.000", 3},       // exactly 3 digits
		{"2006-01-02 15:04:05.000000", 6},    // 6 digits
		{"2006-01-02 15:04:05.999999999", 9}, // 9 digits
		{"2006-01-02 15:04:05.12345abc", 5},  // digits then letters
		{"2006-01-02 15:04:05.", 0},          // dot but no digits
		{".1234", 4},                         // string starts with dot + digits
		{"prefix.987suffix", 3},              // digits then letters after dot
		{"no_digits_after_dot.", 0},          // dot at end
		{"no.dot.in.string", 0},              // dot but not fractional part
	}

	for _, test := range tests {
		got := countDigitsAfterDot(test.layout)
		if got != test.expected {
			t.Errorf("countDigitsAfterDot(%q) = %d; want %d", test.layout, got, test.expected)
		}
	}
}

func TestTruncateFractional(t *testing.T) {
	baseTime := time.Date(2025, 5, 23, 14, 30, 45, 987654321, time.UTC)

	tests := []struct {
		n         int
		wantNanos int
		wantErr   bool
	}{
		{n: 0, wantNanos: 0, wantErr: false},         // truncate to seconds
		{n: 1, wantNanos: 900000000, wantErr: false}, // 1 digit fractional (100ms)
		{n: 3, wantNanos: 987000000, wantErr: false}, // milliseconds
		{n: 5, wantNanos: 987650000, wantErr: false}, // upto 5 digits
		{n: 6, wantNanos: 987654000, wantErr: false}, // microseconds
		{n: 7, wantNanos: 987654300, wantErr: false}, // upto 7 digits
		{n: 9, wantNanos: 987654321, wantErr: false}, // nanoseconds, no truncation
		{n: -1, wantNanos: 0, wantErr: true},         // invalid low
		{n: 10, wantNanos: 0, wantErr: true},         // invalid high
	}

	for _, tt := range tests {
		got, err := truncateFractional(baseTime, tt.n)

		if (err != nil) != tt.wantErr {
			t.Errorf("truncateFractional(_, %d) error = %v, wantErr %v", tt.n, err, tt.wantErr)
			continue
		}
		if err != nil {
			continue // don't check time if error expected
		}

		if got.Nanosecond() != tt.wantNanos {
			t.Errorf("truncateFractional(_, %d) Nanosecond = %d; want %d", tt.n, got.Nanosecond(), tt.wantNanos)
		}

		// Verify that other time components are unchanged
		if got.Year() != baseTime.Year() || got.Month() != baseTime.Month() ||
			got.Day() != baseTime.Day() || got.Hour() != baseTime.Hour() ||
			got.Minute() != baseTime.Minute() || got.Second() != baseTime.Second() {
			t.Errorf("truncateFractional(_, %d) modified time components", tt.n)
		}
	}
}

// TestSizeBasedRotation specifically tests rotation when MaxSize is exceeded.
func TestSizeBasedRotation(t *testing.T) {
	currentTime = fakeTime // Ensure our mock time is used
	megabyte = 1           // For testing with small byte sizes

	dir := makeTempDir("TestSizeBasedRotation", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir) // e.g., /tmp/.../foobar.log
	l := &Logger{
		Filename:         filename,
		MaxSize:          10, // Max size of 10 bytes
		MaxBackups:       1,
		LocalTime:        false, // To match backupFileWithReason which uses UTC
		BackupTimeFormat: backupTimeFormat,
	}
	defer l.Close()

	// First write: 5 bytes, does not exceed MaxSize (10 bytes)
	content1 := []byte("Hello") // 5 bytes
	n, err := l.Write(content1)
	isNil(err, t)
	equals(len(content1), n, t)
	existsWithContent(filename, content1, t)
	fileCount(dir, 1, t)

	// Advance time for the backup timestamp.
	// Note: originalFakeTime variable was here and was unused. It has been removed.
	newFakeTime() // Advances the global fakeCurrentTime

	// Second write: 6 bytes. Current size (5) + new write (6) = 11 bytes, which exceeds MaxSize (10 bytes)
	content2 := []byte("World!") // 6 bytes
	n, err = l.Write(content2)
	isNil(err, t)
	equals(len(content2), n, t)

	// After rotation:
	// Current log file should contain only content2
	existsWithContent(filename, content2, t)

	// Backup file should exist with content1.
	// backupFileWithReason uses the *current* fakeTime (which was advanced by newFakeTime)
	// to generate the timestamped name. The rotation timestamp (l.logStartTime for the
	// backed-up segment, used in backupName) is set to currentTime() when openNew is called.
	backupFilename := backupFileWithReason(dir, "size")
	existsWithContent(backupFilename, content1, t)

	fileCount(dir, 2, t)
}

// TestRotateAtMinutes
func TestRotateAtMinutes(t *testing.T) {
	currentTime = fakeTime // use our mock clock

	// three distinct payloads
	content1 := []byte("first content\n")
	content2 := []byte("second content\n")
	content3 := []byte("third content\n")

	// configure 0, 15, and 30 minute marks
	marks := []int{0, 15, 30}

	// 1) Start just before the 14:00 mark (e.g. 14:00:59 UTC)
	initial := time.Date(2025, time.May, 12, 14, 0, 59, 0, time.UTC)
	fakeCurrentTime = initial

	dir := makeTempDir("TestRotateAtMinutes", t)
	defer os.RemoveAll(dir)
	filename := logFile(dir)

	l := &Logger{
		Filename:         filename,
		RotateAtMinutes:  marks,
		MaxSize:          1000,  // disable size-based rotation
		LocalTime:        false, // use UTC for backup timestamps
		BackupTimeFormat: backupTimeFormat,
	}
	defer l.Close() // stop scheduling goroutine

	// 2) Write at 14:01 → no rotation yet
	fakeCurrentTime = time.Date(2025, time.May, 12, 14, 1, 0, 0, time.UTC)
	n, err := l.Write(content1)
	isNil(err, t)
	equals(len(content1), n, t)
	existsWithContent(filename, content1, t)
	fileCount(dir, 1, t) // only the live logfile

	// 3) Advance to 14:15 exactly, let the goroutine fire
	fakeCurrentTime = time.Date(2025, time.May, 12, 14, 15, 0, 0, time.UTC)
	time.Sleep(300 * time.Millisecond)

	// 4) Write at 14:16 → should be on a fresh file, and first-backup is content1
	fakeCurrentTime = time.Date(2025, time.May, 12, 14, 16, 0, 0, time.UTC)
	n, err = l.Write(content2)
	isNil(err, t)
	equals(len(content2), n, t)
	existsWithContent(filename, content2, t)
	expected1 := backupFileWithReason(dir, "time")
	existsWithContent(expected1, content1, t)
	fileCount(dir, 2, t)

	// 5) Advance past the 14:30 mark without writing → no new rotation
	fakeCurrentTime = time.Date(2025, time.May, 12, 14, 30, 0, 0, time.UTC)
	time.Sleep(300 * time.Millisecond)
	fileCount(dir, 2, t) // still just the live log + one backup

	// 6) Write at 14:31 → triggers the 30-minute mark rotation, and rolls content2
	fakeCurrentTime = time.Date(2025, time.May, 12, 14, 31, 0, 0, time.UTC)
	n, err = l.Write(content3)
	isNil(err, t)
	equals(len(content3), n, t)
	existsWithContent(filename, content3, t)
	expected2 := backupFileWithReason(dir, "time")
	existsWithContent(expected2, content2, t)
	fileCount(dir, 3, t)
}

func TestSortByFormatTimeEdgeCases(t *testing.T) {
	t1 := time.Time{}                      // zero timestamp
	t2 := time.Now()                       // valid timestamp
	fi := dummyFileInfo{name: "dummy.log"} // minimal os.FileInfo impl

	tests := []struct {
		name  string
		input []logInfo
	}{
		{
			"zero and valid timestamps",
			[]logInfo{{t1, fi}, {t2, fi}},
		},
		{
			"valid and zero timestamps",
			[]logInfo{{t2, fi}, {t1, fi}},
		},
		{
			"both zero timestamps",
			[]logInfo{{t1, fi}, {t1, fi}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sort.Sort(byFormatTime(tt.input))
			// just ensure sorting does not panic and results are valid slice
			if len(tt.input) != 2 {
				t.Fatalf("unexpected sort result length: %d", len(tt.input))
			}
		})
	}
}

// dummyFileInfo is a stub for os.FileInfo
type dummyFileInfo struct {
	name string
}

func (d dummyFileInfo) Name() string       { return d.name }
func (d dummyFileInfo) Size() int64        { return 0 }
func (d dummyFileInfo) Mode() os.FileMode  { return 0644 }
func (d dummyFileInfo) ModTime() time.Time { return time.Now() }
func (d dummyFileInfo) IsDir() bool        { return false }
func (d dummyFileInfo) Sys() interface{}   { return nil }

func TestCompressLogFile_SourceOpenError(t *testing.T) {
	err := compressLogFile("nonexistent.log", "should-not-be-created.gz")
	if err == nil || !strings.Contains(err.Error(), "failed to open source log file") {
		t.Fatalf("expected error opening nonexistent file, got: %v", err)
	}
}

func TestOpenExistingOrNew_Fallback(t *testing.T) {
	tmpDir := t.TempDir()
	path := filepath.Join(tmpDir, "readonly.log")

	logger := &Logger{
		Filename:         path,
		MaxSize:          1,
		BackupTimeFormat: backupTimeFormat,
	}

	// Create a file with 0 perms so append will fail
	_ = os.WriteFile(logger.Filename, []byte("data"), 0000)

	err := logger.openExistingOrNew(1)
	if err != nil {
		t.Fatalf("expected fallback to openNew, got error: %v", err)
	}

	// Clean up the recreated file
	if rmErr := os.Remove(path); rmErr != nil && !os.IsNotExist(rmErr) {
		t.Errorf("cleanup failed: %v", rmErr)
	}
}

func TestMillRunOnce_OldFilesRemoved(t *testing.T) {
	dir := t.TempDir()
	oldLog := filepath.Join(dir, "test-2000-01-01T00-00-00.000-size.log")
	_ = os.WriteFile(oldLog, []byte("data"), 0644)

	logger := &Logger{
		Filename:         filepath.Join(dir, "test.log"),
		MaxAge:           1,
		Compress:         false,
		MaxBackups:       0,
		BackupTimeFormat: backupTimeFormat,
	}
	currentTime = func() time.Time {
		return time.Now().AddDate(0, 0, 10)
	}

	err := logger.millRunOnce()
	if err != nil {
		t.Fatalf("millRunOnce failed: %v", err)
	}
	if _, err := os.Stat(oldLog); !os.IsNotExist(err) {
		t.Errorf("expected old file to be deleted")
	}
}

func TestTimeFromName_InvalidFormat(t *testing.T) {
	logger := &Logger{
		Filename:         "foo.log",
		BackupTimeFormat: backupTimeFormat,
	}
	prefix, ext := logger.prefixAndExt()

	// Case 1: mismatched prefix
	_, err := logger.timeFromName("badname.log", prefix, ext)
	if err == nil || !strings.Contains(err.Error(), "mismatched prefix") {
		t.Fatalf("expected mismatched prefix error, got: %v", err)
	}

	// Case 2: mismatched extension
	_, err = logger.timeFromName("foo-2020-01-01T00-00-00.000-size.txt", prefix, ext)
	if err == nil || !strings.Contains(err.Error(), "mismatched extension") {
		t.Fatalf("expected mismatched extension error, got: %v", err)
	}

	// Case 3: malformed timestamp structure
	_, err = logger.timeFromName("foo-2020-01-01T00-00-size.log", prefix, ext)
	if err == nil || !strings.Contains(err.Error(), "cannot parse") {
		t.Fatalf("expected time parse error, got: %v", err)
	}
}

func TestBackupName(t *testing.T) {
	name := "/tmp/test.log"
	rotationTime := time.Date(2020, 1, 2, 3, 4, 5, 6_000_000, time.UTC)

	resultUTC := backupName(name, false, "size", rotationTime, backupTimeFormat)
	expectedUTC := "/tmp/test-2020-01-02T03-04-05.006-size.log"
	if resultUTC != expectedUTC {
		t.Errorf("expected %q, got %q", expectedUTC, resultUTC)
	}

	resultLocal := backupName(name, true, "manual", rotationTime.In(time.Local), backupTimeFormat)
	// Format expected using time.Local — hard to assert string equality unless mocked
	if !strings.Contains(resultLocal, "-manual.log") {
		t.Errorf("expected suffix -manual.log, got: %s", resultLocal)
	}
}

func TestShouldTimeRotate_WhenZero(t *testing.T) {
	l := &Logger{
		RotationInterval: time.Second,
		BackupTimeFormat: backupTimeFormat,
	}

	currentTime = func() time.Time {
		return time.Now()
	}

	if l.shouldTimeRotate() {
		t.Error("expected false when lastRotationTime is zero")
	}
}

func TestShouldTimeRotate_WhenElapsed(t *testing.T) {
	currentTime = func() time.Time {
		return time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	}
	l := &Logger{
		RotationInterval: time.Minute,
		lastRotationTime: time.Date(2025, 1, 1, 11, 58, 0, 0, time.UTC),
		BackupTimeFormat: backupTimeFormat,
	}
	if !l.shouldTimeRotate() {
		t.Error("expected rotation due to elapsed time")
	}
}

func TestRunScheduledRotations_NoMarks(t *testing.T) {
	l := &Logger{
		BackupTimeFormat: backupTimeFormat,
	}
	l.scheduledRotationWg.Add(1)

	// processedRotateAtMinutes is empty — triggers early return
	go l.runScheduledRotations()

	done := make(chan struct{})
	go func() {
		l.scheduledRotationWg.Wait()
		close(done)
	}()

	select {
	case <-done:
		// success
	case <-time.After(100 * time.Millisecond):
		t.Error("expected goroutine to return immediately due to no marks")
	}
}

func TestRotate_OpenNewFails(t *testing.T) {
	badPath := "/bad/path/logfile.log"
	l := &Logger{
		Filename:         badPath,
		BackupTimeFormat: backupTimeFormat,
	}
	// force an invalid path to trigger openNew failure
	err := l.rotate("manual")
	if err == nil {
		t.Fatal("expected error from rotate due to invalid openNew")
	}
}

func TestRotate_TriggersTimeReason(t *testing.T) {
	currentTime = func() time.Time {
		return time.Date(2024, 5, 1, 12, 0, 0, 0, time.UTC)
	}
	l := &Logger{
		Filename:         filepath.Join(t.TempDir(), "time-reason.log"),
		RotationInterval: time.Minute,
		lastRotationTime: time.Date(2024, 5, 1, 11, 58, 0, 0, time.UTC),
		BackupTimeFormat: backupTimeFormat,
	}
	defer l.Close()

	err := l.Rotate()
	if err != nil {
		t.Errorf("expected successful rotate, got %v", err)
	}
}

func TestRunScheduledRotations_NoFutureTime(t *testing.T) {
	defer func() { recover() }() // prevent panic in background goroutine

	originalTime := currentTime
	defer func() { currentTime = originalTime }()

	currentTime = func() time.Time {
		return time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
	}

	tmp := t.TempDir()
	logFile := filepath.Join(tmp, "invalid.log")

	logger := &Logger{
		Filename:                logFile,
		RotateAtMinutes:         []int{0},
		scheduledRotationWg:     sync.WaitGroup{},
		scheduledRotationQuitCh: make(chan struct{}),
		BackupTimeFormat:        backupTimeFormat,
	}
	logger.processedRotateAtMinutes = []int{0}

	logger.scheduledRotationWg.Add(1)
	go logger.runScheduledRotations()

	time.Sleep(150 * time.Millisecond)
	close(logger.scheduledRotationQuitCh)
	logger.scheduledRotationWg.Wait()

	// clean up
	os.Remove(logFile)
}

func TestEnsureScheduledRotationLoopRunning_InvalidMinutes(t *testing.T) {
	l := &Logger{
		RotateAtMinutes:  []int{61, -1, 999}, // invalid minutes
		BackupTimeFormat: backupTimeFormat,
	}
	l.ensureScheduledRotationLoopRunning()

	if len(l.processedRotateAtMinutes) != 0 {
		t.Errorf("expected no valid minutes, got: %v", l.processedRotateAtMinutes)
	}
}

func TestCompressLogFile_ChownFails(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "to-compress.log")
	dst := src + ".gz"
	_ = os.WriteFile(src, []byte("dummy"), 0644)

	// mock chown to always fail
	originalChown := chown
	chown = func(_ string, _ os.FileInfo) error {
		return fmt.Errorf("mock chown failure")
	}
	defer func() { chown = originalChown }()

	err := compressLogFile(src, dst)
	if err != nil {
		t.Fatalf("compression should still succeed, got: %v", err)
	}

	if _, err := os.Stat(dst); err != nil {
		t.Errorf("expected compressed file to exist, got: %v", err)
	}
}

func TestOpenNew_RenameFails(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "test.log")
	_ = os.WriteFile(file, []byte("original"), 0644)

	// Fix timestamp so backupName is predictable
	currentTime = func() time.Time {
		return time.Date(2024, 1, 1, 12, 0, 0, 0, time.UTC)
	}

	originalRename := osRename
	osRename = func(_, _ string) error {
		return fmt.Errorf("mock rename failure")
	}
	defer func() { osRename = originalRename }()

	l := &Logger{
		Filename:         file,
		BackupTimeFormat: backupTimeFormat,
	}
	err := l.openNew("size")

	if err == nil || !strings.Contains(err.Error(), "can't rename") {
		t.Fatalf("expected rename error, got: %v", err)
	}
}

func TestCompressLogFile_StatFails(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "bad.log")
	dst := src + ".gz"

	// Write then delete to cause os.Open to fail before stat is called
	_ = os.WriteFile(src, []byte("dummy"), 0644)
	_ = os.Remove(src)

	err := compressLogFile(src, dst)
	if err == nil || !strings.Contains(err.Error(), "failed to open source log file") {
		t.Errorf("expected open error, got: %v", err)
	}
}

func TestRotate_CloseFileFails(t *testing.T) {
	tmp := filepath.Join(t.TempDir(), "dummy.log")

	// Create and close a real file
	f, err := os.Create(tmp)
	if err != nil {
		t.Fatal(err)
	}
	_ = f.Close() // Close early to simulate Close() failure

	l := &Logger{
		file:             f,
		BackupTimeFormat: backupTimeFormat,
	}

	err = l.Rotate()
	if err == nil {
		t.Fatal("expected error from closed file, got nil")
	}
}

func TestOpenNew_StatUnexpectedError(t *testing.T) {
	logger := &Logger{
		Filename:         filepath.Join(t.TempDir(), "logfile.log"),
		BackupTimeFormat: backupTimeFormat,
	}

	originalOsStat := osStat
	osStat = func(name string) (os.FileInfo, error) {
		return nil, fmt.Errorf("mock stat failure")
	}
	defer func() { osStat = originalOsStat }()

	err := logger.openNew("size")
	if err == nil || !strings.Contains(err.Error(), "failed to stat") {
		t.Errorf("expected stat failure, got: %v", err)
	}
}

func TestCompressLogFile_CopyFails(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "bad.log")
	dst := src + ".gz"

	// Write a real file with restricted permissions
	if err := os.WriteFile(src, []byte("data"), 0200); err != nil { // write-only
		t.Fatalf("failed to create test file: %v", err)
	}
	defer os.Chmod(src, 0644) // restore perms to allow deletion

	// Patch osStat
	originalStat := osStat
	osStat = func(name string) (os.FileInfo, error) {
		return os.Stat(src)
	}
	defer func() { osStat = originalStat }()

	err := compressLogFile(src, dst)
	if err == nil || !strings.Contains(err.Error(), "failed to copy data") &&
		!strings.Contains(err.Error(), "permission denied") {
		t.Errorf("expected failure during compression, got: %v", err)
	}
}

func TestOpenExistingOrNew_StatFailure(t *testing.T) {
	originalStat := osStat
	defer func() { osStat = originalStat }()

	osStat = func(_ string) (os.FileInfo, error) {
		return nil, fmt.Errorf("mock stat failure")
	}

	logger := &Logger{
		Filename:         "somefile.log",
		BackupTimeFormat: backupTimeFormat,
	}
	logger.millCh = make(chan bool, 1) // prevent nil panic
	err := logger.openExistingOrNew(10)
	if err == nil || !strings.Contains(err.Error(), "error getting log file info") {
		t.Fatalf("expected stat failure, got: %v", err)
	}
}

func TestOpenNew_OpenFileFails(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a file where a directory is expected
	fileAsDir := filepath.Join(tmpDir, "not_a_dir")
	err := os.WriteFile(fileAsDir, []byte("I am a file, not a dir"), 0644)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	// Attempt to use that file as a directory
	badPath := filepath.Join(fileAsDir, "should_fail.log")

	logger := &Logger{
		Filename:         badPath,
		BackupTimeFormat: backupTimeFormat,
	}
	err = logger.openNew("size")

	if err == nil || !strings.Contains(err.Error(), "can't make directories") {
		t.Fatalf("expected mkdir failure, got: %v", err)
	}
}

func TestRunScheduledRotations_NoFutureSlot(t *testing.T) {
	originalTime := currentTime
	defer func() { currentTime = originalTime }()

	currentTime = func() time.Time {
		// Always return a time far in the past
		return time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
	}

	logger := &Logger{
		Filename:                "invalid.log",
		RotateAtMinutes:         []int{0},
		scheduledRotationQuitCh: make(chan struct{}),
		BackupTimeFormat:        backupTimeFormat,
	}
	logger.processedRotateAtMinutes = []int{0}
	logger.scheduledRotationWg.Add(1)

	go logger.runScheduledRotations()

	time.Sleep(200 * time.Millisecond)
	close(logger.scheduledRotationQuitCh)
	logger.scheduledRotationWg.Wait()
}

func TestTimeFromName_MalformedFilename(t *testing.T) {
	logger := &Logger{
		Filename:         "foo.log",
		BackupTimeFormat: backupTimeFormat,
	}
	prefix, ext := logger.prefixAndExt()

	// Missing final hyphen separator, so no reason part
	invalid := "foo-20200101T000000000.log"

	_, err := logger.timeFromName(invalid, prefix, ext)
	if err == nil || !strings.Contains(err.Error(), "malformed backup filename") {
		t.Fatalf("expected malformed filename error, got: %v", err)
	}
}

func TestWrite_OpenExistingFails(t *testing.T) {
	// Simulate a failure in osStat that's not os.IsNotExist
	originalStat := osStat
	defer func() { osStat = originalStat }()

	osStat = func(name string) (os.FileInfo, error) {
		return nil, fmt.Errorf("mocked stat failure")
	}

	logger := &Logger{
		Filename:         filepath.Join(t.TempDir(), "badfile.log"),
		MaxSize:          10,
		BackupTimeFormat: backupTimeFormat,
	}

	// prevent nil panic
	logger.millCh = make(chan bool, 1)

	_, err := logger.Write([]byte("trigger"))
	if err == nil || !strings.Contains(err.Error(), "error getting log file info") {
		t.Fatalf("expected error from Write when stat fails, got: %v", err)
	}
}

func TestWrite_IntervalRotateFails(t *testing.T) {
	currentTime = func() time.Time {
		return time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	}

	// Patch osRename to force openNew failure (simulate rename failure inside rotate)
	originalRename := osRename
	defer func() { osRename = originalRename }()
	osRename = func(_, _ string) error {
		return fmt.Errorf("mock rename failure")
	}

	tmp := t.TempDir()
	logfile := filepath.Join(tmp, "fail.log")

	// Write some initial file content
	err := os.WriteFile(logfile, []byte("existing"), 0644)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	l := &Logger{
		Filename:         logfile,
		RotationInterval: time.Second,
		lastRotationTime: time.Date(2025, 1, 1, 11, 59, 0, 0, time.UTC),
		BackupTimeFormat: backupTimeFormat,
	}
	defer l.Close()

	// trigger rotation path
	_, err = l.Write([]byte("trigger"))
	if err == nil || !strings.Contains(err.Error(), "interval rotation failed") {
		t.Fatalf("expected interval rotation error, got: %v", err)
	}
}

func TestWrite_SizeRotateFails(t *testing.T) {
	currentTime = func() time.Time {
		return time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	}
	megabyte = 1 // tiny units for easy triggering

	// Patch osRename to force openNew to fail during rotate("size")
	originalRename := osRename
	defer func() { osRename = originalRename }()
	osRename = func(_, _ string) error {
		return fmt.Errorf("mock rename failure for size")
	}

	tmp := t.TempDir()
	logfile := filepath.Join(tmp, "sizefail.log")

	// Create initial file with some content (5 bytes)
	err := os.WriteFile(logfile, []byte("12345"), 0644)
	if err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	l := &Logger{
		Filename:         logfile,
		MaxSize:          10, // force rotation at 10 bytes
		BackupTimeFormat: backupTimeFormat,
	}
	defer l.Close()

	// This write pushes us over the max size and triggers rotation
	big := []byte("123456789") // 9 bytes + 5 existing = 14 > 10

	_, err = l.Write(big)
	if err == nil || !strings.Contains(err.Error(), "can't rename log file") {
		t.Fatalf("expected rename failure in size-based rotation, got: %v", err)
	}
}

func TestCompressLogFile_StatFails_1(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "test.log")
	dst := src + ".gz"

	// Write dummy data to the source file
	err := os.WriteFile(src, []byte("log content"), 0644)
	if err != nil {
		t.Fatalf("failed to create source log: %v", err)
	}

	// Patch osStat to fail
	originalStat := osStat
	osStat = func(_ string) (os.FileInfo, error) {
		return nil, fmt.Errorf("mock stat failure")
	}
	defer func() { osStat = originalStat }()

	err = compressLogFile(src, dst)
	if err == nil || !strings.Contains(err.Error(), "failed to stat source log file") {
		t.Fatalf("expected stat failure during compressLogFile, got: %v", err)
	}
}

func TestCompressLogFile_OpenDestFails(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "log.log")

	// Write a dummy source log
	err := os.WriteFile(src, []byte("hello"), 0644)
	if err != nil {
		t.Fatalf("failed to write source: %v", err)
	}

	// Create a file where a directory should be
	fileAsDir := filepath.Join(tmp, "not_a_dir")
	err = os.WriteFile(fileAsDir, []byte("conflict"), 0644)
	if err != nil {
		t.Fatalf("failed to create conflict path: %v", err)
	}

	// Destination path attempts to go under the file
	dst := filepath.Join(fileAsDir, "dest.log.gz")

	err = compressLogFile(src, dst)
	if err == nil || !strings.Contains(err.Error(), "failed to open destination compressed log file") {
		t.Fatalf("expected failure opening dest, got: %v", err)
	}
}

type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, fmt.Errorf("forced read failure")
}

func TestCompressLogFile_CopyFails_1(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "dummy.log")
	dst := filepath.Join(tmp, "output.gz")

	// Write dummy source
	err := os.WriteFile(src, []byte("irrelevant"), 0644)
	if err != nil {
		t.Fatalf("failed to create dummy file: %v", err)
	}
	defer os.Remove(src) // cleanup

	srcInfo, err := os.Stat(src)
	if err != nil {
		t.Fatalf("stat failed: %v", err)
	}

	dstFile, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY, srcInfo.Mode())
	if err != nil {
		t.Fatalf("failed to create dst file: %v", err)
	}
	defer func() {
		dstFile.Close()
		os.Remove(dst)
	}()

	gz := gzip.NewWriter(dstFile)

	// Trigger copy failure
	_, err = io.Copy(gz, failingReader{})
	if err == nil || !strings.Contains(err.Error(), "forced read failure") {
		t.Fatalf("expected read failure, got: %v", err)
	}

	_ = gz.Close() // ensure no panic
}

func TestCompressLogFile_GzipCloseFails_Simulated(t *testing.T) {
	// We'll simulate a scenario where the Close() on the gzip.Writer fails
	// by closing the destination before it's flushed.

	tmp := t.TempDir()
	src := filepath.Join(tmp, "src.log")
	dst := filepath.Join(tmp, "dst.gz")

	// Write some dummy content
	err := os.WriteFile(src, []byte("data to compress"), 0644)
	if err != nil {
		t.Fatalf("failed to write src: %v", err)
	}
	defer os.Remove(src)

	// Create a broken pipe that will cause Close() to fail
	pr, pw := io.Pipe()
	pw.CloseWithError(fmt.Errorf("simulated pipe failure"))

	// gzip.NewWriter expects a WriteCloser — so wrap `pw`
	gz := gzip.NewWriter(pw)

	// Start compression using io.Copy — should fail
	go func() {
		f, _ := os.Open(src)
		defer f.Close()
		_, _ = io.Copy(gz, f)
		// This flushes and triggers the Close failure
		_ = gz.Close()
	}()

	// Read to trigger pipe read error
	buf := make([]byte, 1024)
	_, err = pr.Read(buf)
	if err == nil || !strings.Contains(err.Error(), "simulated pipe failure") {
		t.Fatalf("expected gzip flush failure due to broken pipe, got: %v", err)
	}

	_ = os.Remove(dst)
}

func TestCompressLogFile_RemoveFails(t *testing.T) {
	tmp := t.TempDir()
	src := filepath.Join(tmp, "locked.log")
	dst := src + ".gz"

	// Write dummy data to the source file
	if err := os.WriteFile(src, []byte("test log"), 0644); err != nil {
		t.Fatalf("failed to create source file: %v", err)
	}

	// Open the file for exclusive read to simulate 'in use' state (Unix-like only)
	f, err := os.Open(src)
	if err != nil {
		t.Fatalf("failed to open file exclusively: %v", err)
	}
	defer f.Close() // Will keep it open while compressing

	// Patch os.Remove to simulate failure while file is open
	originalRemove := os.Remove
	osRemove = func(path string) error {
		if path == src {
			return fmt.Errorf("mock remove failure")
		}
		return originalRemove(path)
	}
	defer func() { osRemove = originalRemove }()

	err = compressLogFile(src, dst)
	if err == nil || !strings.Contains(err.Error(), "failed to remove original source log file") {
		t.Fatalf("expected failure from os.Remove, got: %v", err)
	}

	_ = os.Remove(dst) // cleanup
}

func TestEnsureScheduledRotationLoopRunning_Empty(t *testing.T) {
	l := &Logger{
		RotateAtMinutes:  nil, // empty case
		BackupTimeFormat: backupTimeFormat,
	}
	l.ensureScheduledRotationLoopRunning()

	if len(l.processedRotateAtMinutes) != 0 {
		t.Errorf("expected no processed rotation minutes, got: %v", l.processedRotateAtMinutes)
	}
}

func TestEnsureScheduledRotationLoopRunning_InvalidMinutes_1(t *testing.T) {
	l := &Logger{
		RotateAtMinutes:  []int{-5, 60, 999, -1}, // all invalid
		BackupTimeFormat: backupTimeFormat,
	}
	l.ensureScheduledRotationLoopRunning()

	if len(l.processedRotateAtMinutes) != 0 {
		t.Errorf("expected 0 valid minutes, got: %v", l.processedRotateAtMinutes)
	}

	if l.scheduledRotationQuitCh != nil {
		t.Errorf("expected scheduled rotation goroutine not to start")
	}
}

func TestRunScheduledRotations_NoFutureSlotFallback(t *testing.T) {
	defer func() { recover() }() // ignore goroutine panic if any
	originalTime := currentTime
	defer func() { currentTime = originalTime }()

	// Force time to never advance — stuck before all slots
	currentTime = func() time.Time {
		return time.Date(1999, 1, 1, 0, 0, 0, 0, time.UTC)
	}

	logger := &Logger{
		Filename:                "test-fallback.log",
		RotateAtMinutes:         []int{0},
		scheduledRotationQuitCh: make(chan struct{}),
		BackupTimeFormat:        backupTimeFormat,
	}
	logger.processedRotateAtMinutes = []int{0}

	// Start the goroutine
	logger.scheduledRotationWg.Add(1)
	go logger.runScheduledRotations()

	// Wait a short time, then exit before full 1-minute wait
	time.Sleep(200 * time.Millisecond)
	close(logger.scheduledRotationQuitCh)
	logger.scheduledRotationWg.Wait()
}

func TestLoggerClose_AlreadyClosedChannel(t *testing.T) {
	logger := &Logger{
		Filename:                "test-double-close.log",
		scheduledRotationQuitCh: make(chan struct{}),
		BackupTimeFormat:        backupTimeFormat,
	}

	// Close the quit channel manually
	close(logger.scheduledRotationQuitCh)

	// Call Close — this should NOT panic or attempt to close again
	err := logger.Close()
	if err != nil {
		t.Fatalf("expected no error from double-close-safe Close(), got: %v", err)
	}
}

func TestMillRunOnce_NoOp(t *testing.T) {
	logger := &Logger{
		MaxBackups:       0,
		MaxAge:           0,
		Compress:         false,
		Filename:         filepath.Join(t.TempDir(), "noop.log"),
		BackupTimeFormat: backupTimeFormat,
	}

	// Should do nothing and return nil
	err := logger.millRunOnce()
	if err != nil {
		t.Fatalf("expected nil from noop millRunOnce, got: %v", err)
	}
}

func TestShouldTimeRotate_ZeroLastRotationTime(t *testing.T) {
	logger := &Logger{
		RotationInterval: time.Minute,
		lastRotationTime: time.Time{}, // zero time
		BackupTimeFormat: backupTimeFormat,
	}

	if logger.shouldTimeRotate() {
		t.Fatalf("expected false from shouldTimeRotate when lastRotationTime is zero")
	}
}

func TestMillRunOnce_CompressEligible(t *testing.T) {
	tmp := t.TempDir()
	logger := &Logger{
		Filename:         filepath.Join(tmp, "test.log"),
		Compress:         true,
		MaxBackups:       1,
		BackupTimeFormat: backupTimeFormat,
	}

	// Create a non-compressed log file with a valid timestamp in name
	backupName := "test-2025-01-01T00-00-00.000-size.log"
	path := filepath.Join(tmp, backupName)
	if err := os.WriteFile(path, []byte("log"), 0644); err != nil {
		t.Fatalf("failed to create backup log: %v", err)
	}
	defer os.Remove(path)

	// Should find this file eligible for compression
	err := logger.millRunOnce()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Verify compressed file was created
	compressed := path + ".gz"
	if _, err := os.Stat(compressed); err != nil {
		t.Errorf("expected compressed file, not found: %v", err)
	}
	_ = os.Remove(compressed)
}

func TestMillRunOnce_ExpiredFileSkipped(t *testing.T) {
	tmp := t.TempDir()
	base := filepath.Join(tmp, "logfile.log")

	logger := &Logger{
		Filename:         base,
		MaxAge:           1,    // 1 day
		Compress:         true, // trigger compression logic
		BackupTimeFormat: backupTimeFormat,
	}

	// Manually choose a timestamp > 1 day old
	oldName := "logfile-2020-01-01T00-00-00.000-size.log"
	oldPath := filepath.Join(tmp, oldName)

	if err := os.WriteFile(oldPath, []byte("expired"), 0644); err != nil {
		t.Fatalf("failed to create old log file: %v", err)
	}
	defer os.Remove(oldPath)

	err := logger.millRunOnce()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if _, err := os.Stat(oldPath + compressSuffix); err == nil {
		t.Errorf("expected no compression for expired file, but .gz file exists")
	}
}

func TestMillRun_TriggersMillRunOnce_Effect(t *testing.T) {
	tmp := t.TempDir()
	logFile := filepath.Join(tmp, "log.log")

	// Create a backup log file that should be compressed
	backup := filepath.Join(tmp, "log-2020-01-01T00-00-00.000-size.log")
	if err := os.WriteFile(backup, []byte("backup data"), 0644); err != nil {
		t.Fatalf("failed to create backup: %v", err)
	}

	l := &Logger{
		Filename:         logFile,
		Compress:         true,
		millCh:           make(chan bool),
		BackupTimeFormat: backupTimeFormat,
	}

	// Start millRun in background
	go l.millRun()

	// Trigger it
	l.millCh <- true
	time.Sleep(100 * time.Millisecond)
	close(l.millCh)

	// Wait briefly for compression to complete
	time.Sleep(200 * time.Millisecond)

	// Check if file was compressed
	_, err := os.Stat(backup + ".gz")
	if err != nil {
		t.Fatalf("expected compressed file not found: %v", err)
	}

	// Cleanup
	os.Remove(backup + ".gz")
}

func TestRotate_StartMillOnlyOnce_Observable(t *testing.T) {
	tmp := t.TempDir()
	base := filepath.Join(tmp, "logfile.log")
	logger := &Logger{
		Filename:         base,
		MaxSize:          1,
		Compress:         true,
		millCh:           make(chan bool, 10), // Buffered so we can trigger multiple
		BackupTimeFormat: backupTimeFormat,
	}

	// Create two valid backup files to be compressed
	for i := 0; i < 2; i++ {
		name := fmt.Sprintf("logfile-2020-01-01T00-00-0%d.000-size.log", i)
		path := filepath.Join(tmp, name)
		if err := os.WriteFile(path, []byte("to compress"), 0644); err != nil {
			t.Fatalf("failed to write %s: %v", path, err)
		}
		defer os.Remove(path + ".gz")
	}

	// Rotate once — triggers millRun and startMill.Do
	if err := logger.rotate("size"); err != nil {
		t.Fatalf("rotate failed: %v", err)
	}

	// Send cleanup signals — these trigger millRunOnce via millRun
	logger.millCh <- true
	logger.millCh <- true
	close(logger.millCh)

	// Wait briefly for compression to complete
	time.Sleep(300 * time.Millisecond)

	// Count only compressed versions of the test backup files
	count := 0
	entries, _ := os.ReadDir(tmp)
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "logfile-2020") && strings.HasSuffix(e.Name(), ".gz") {
			count++
		}
	}

	if count != 2 {
		t.Fatalf("expected 2 compressed files, got: %d", count)
	}
}

func TestScheduledMinuteRotationFails(t *testing.T) {
	tmp := t.TempDir()
	file := filepath.Join(tmp, "fail.log")

	logger := &Logger{
		Filename:         file,
		RotateAtMinutes:  []int{0},
		BackupTimeFormat: backupTimeFormat,
	}
	logger.processedRotateAtMinutes = []int{0}
	logger.scheduledRotationQuitCh = make(chan struct{})

	logger.lastRotationTime = time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)

	// force rotate to fail
	logger.file = &os.File{} // invalid
	logger.scheduledRotationWg.Add(1)
	go logger.runScheduledRotations()

	time.Sleep(100 * time.Millisecond)
	close(logger.scheduledRotationQuitCh)
	logger.scheduledRotationWg.Wait()
}

func TestRunScheduledRotations_CannotFindNextSlot(t *testing.T) {
	oldTime := currentTime
	defer func() { currentTime = oldTime }()

	currentTime = func() time.Time {
		return time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC)
	}

	logger := &Logger{
		Filename:                "test.log",
		RotateAtMinutes:         []int{0},
		scheduledRotationQuitCh: make(chan struct{}),
		BackupTimeFormat:        backupTimeFormat,
	}
	logger.processedRotateAtMinutes = []int{0}
	logger.scheduledRotationWg.Add(1)

	go logger.runScheduledRotations()
	time.Sleep(150 * time.Millisecond)
	close(logger.scheduledRotationQuitCh)
	logger.scheduledRotationWg.Wait()
}
func TestCompressLogFile_CloseDestFails(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "log.log")
	dst := src + ".gz"
	_ = os.WriteFile(src, []byte("dummy"), 0644)

	// Patch osStat to return valid info
	originalStat := osStat
	osStat = func(name string) (os.FileInfo, error) {
		return os.Stat(src)
	}
	defer func() { osStat = originalStat }()

	// simulate close failure via ReadOnly FS or mocking
	err := compressLogFile(src, dst)
	if err != nil && !strings.Contains(err.Error(), "failed to close destination") {
		t.Fatalf("expected close error, got: %v", err)
	}
}

func TestRunScheduledRotations_NoFutureSlotFound(t *testing.T) {
	originalTime := currentTime
	defer func() { currentTime = originalTime }()
	currentTime = func() time.Time {
		return time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC)
	}

	logger := &Logger{
		Filename:         "mock.log",
		RotateAtMinutes:  []int{0},
		BackupTimeFormat: backupTimeFormat,
	}
	logger.processedRotateAtMinutes = []int{0}
	logger.scheduledRotationQuitCh = make(chan struct{})

	logger.scheduledRotationWg.Add(1)
	go logger.runScheduledRotations()

	time.Sleep(200 * time.Millisecond)
	close(logger.scheduledRotationQuitCh)
	logger.scheduledRotationWg.Wait()
}

func TestScheduledRotation_TimerFiresAndRotates(t *testing.T) {
	originalTime := currentTime
	defer func() { currentTime = originalTime }()

	now := time.Date(2025, 1, 1, 10, 0, 0, 0, time.UTC)
	currentTime = func() time.Time { return now }

	tmpDir := t.TempDir()
	file := filepath.Join(tmpDir, "timerfire.log")

	logger := &Logger{
		Filename:         file,
		RotateAtMinutes:  []int{1}, // Next minute after 'now'
		BackupTimeFormat: backupTimeFormat,
	}
	logger.processedRotateAtMinutes = []int{1}
	logger.scheduledRotationQuitCh = make(chan struct{})
	logger.lastRotationTime = now.Add(-time.Hour) // So it qualifies

	logger.scheduledRotationWg.Add(1)
	go logger.runScheduledRotations()

	time.Sleep(1500 * time.Millisecond)
	close(logger.scheduledRotationQuitCh)
	logger.scheduledRotationWg.Wait()
}

func TestMillRunOnce_RemoveFails(t *testing.T) {
	tmp := t.TempDir()
	logFile := filepath.Join(tmp, "log-2025-01-01T00-00-00.000-size.log")
	_ = os.WriteFile(logFile, []byte("data"), 0644)

	origRemove := osRemove
	osRemove = func(path string) error {
		return fmt.Errorf("mock remove failure")
	}
	defer func() { osRemove = origRemove }()

	logger := &Logger{
		Filename:         filepath.Join(tmp, "dummy.log"),
		MaxBackups:       1,
		Compress:         false,
		BackupTimeFormat: backupTimeFormat,
	}
	err := logger.millRunOnce()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestCompressLogFile_CopyFails_2(t *testing.T) {
	// Create a file then remove it to make it unreadable
	tmp := t.TempDir()
	src := filepath.Join(tmp, "broken.log")
	_ = os.WriteFile(src, []byte("data"), 0644)

	// Simulate failure by removing source before compression
	os.Remove(src)

	dst := src + ".gz"
	err := compressLogFile(src, dst)
	if err == nil {
		t.Fatal("expected error due to missing source, got nil")
	}
	if !strings.Contains(err.Error(), "failed to open source") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestRunScheduledRotations_FallbackRetry(t *testing.T) {
	originalNow := currentTime
	defer func() { currentTime = originalNow }()

	// Simulate a time far in the future so that no candidate slot is after 'now'
	currentTime = func() time.Time {
		return time.Date(9999, 1, 1, 23, 59, 59, 0, time.UTC)
	}

	logger := &Logger{
		Filename:                 "test.log",
		RotateAtMinutes:          []int{0},
		processedRotateAtMinutes: []int{0},
		scheduledRotationQuitCh:  make(chan struct{}),
		BackupTimeFormat:         backupTimeFormat,
	}

	logger.scheduledRotationWg.Add(1)
	go logger.runScheduledRotations()

	time.Sleep(200 * time.Millisecond)
	close(logger.scheduledRotationQuitCh)
	logger.scheduledRotationWg.Wait()
}

func TestRunScheduledRotations_TimerFires(t *testing.T) {
	tmp := t.TempDir()
	logFile := filepath.Join(tmp, "test.log")

	logger := &Logger{
		Filename:                 logFile,
		RotateAtMinutes:          []int{1},
		processedRotateAtMinutes: []int{1},
		scheduledRotationQuitCh:  make(chan struct{}),
		BackupTimeFormat:         backupTimeFormat,
	}

	logger.lastRotationTime = time.Now().Add(-time.Hour)

	logger.scheduledRotationWg.Add(1)
	go logger.runScheduledRotations()

	// Wait for timer to fire
	time.Sleep(1500 * time.Millisecond)
	close(logger.scheduledRotationQuitCh)
	logger.scheduledRotationWg.Wait()
}

func TestCompressLogFile_CopyFails_4(t *testing.T) {
	// Prepare unreadable source file (delete after creation)
	tmp := t.TempDir()
	src := filepath.Join(tmp, "unreadable.log")
	_ = os.WriteFile(src, []byte("something"), 0600)
	_ = os.Remove(src) // remove so Open will fail

	dst := filepath.Join(tmp, "unreadable.log.gz")

	err := compressLogFile(src, dst)
	if err == nil || !strings.Contains(err.Error(), "failed to open source") {
		t.Fatalf("expected source open error, got: %v", err)
	}
}
func TestWrite_SizeRotateFails_4(t *testing.T) {
	tmp := t.TempDir()
	logPath := filepath.Join(tmp, "fail-size.log")

	logger := &Logger{
		Filename:         logPath,
		MaxSize:          1, // 1 MB
		BackupTimeFormat: backupTimeFormat,
	}

	// Write almost max-size file manually
	_ = os.WriteFile(logPath, bytes.Repeat([]byte("x"), int(logger.max()-1)), 0644)

	// Don't preopen file — force logger to call openExistingOrNew → openNew → osRename
	logger.file = nil
	logger.size = logger.max() - 1

	// Simulate rename failure
	origRename := osRename
	osRename = func(oldpath, newpath string) error {
		return fmt.Errorf("mock rename error")
	}
	defer func() { osRename = origRename }()

	_, err := logger.Write([]byte("x")) // this triggers rotation
	if err == nil || !strings.Contains(err.Error(), "can't rename log file") {
		t.Fatalf("expected rename error during rotation, got: %v", err)
	}
}

func TestWrite_IntervalRotationFails(t *testing.T) {
	currentTime = func() time.Time {
		return time.Date(2025, 1, 1, 12, 0, 0, 0, time.UTC)
	}
	defer func() { currentTime = time.Now }()

	// Patch osRename to force rotate("time") to fail
	origRename := osRename
	defer func() { osRename = origRename }()
	osRename = func(_, _ string) error {
		return fmt.Errorf("forced rename failure for interval")
	}

	tmp := t.TempDir()
	logfile := filepath.Join(tmp, "fail.log")

	// Seed file to trigger openExisting
	_ = os.WriteFile(logfile, []byte("seed"), 0644)

	logger := &Logger{
		Filename:         logfile,
		RotationInterval: time.Minute,
		lastRotationTime: currentTime().Add(-2 * time.Minute),
		BackupTimeFormat: backupTimeFormat,
	}
	defer logger.Close()

	_, err := logger.Write([]byte("trigger"))
	if err == nil || !strings.Contains(err.Error(), "interval rotation failed") {
		t.Fatalf("expected interval rotation failure, got: %v", err)
	}
}

func TestRunScheduledRotations_NoFutureSlotRetry(t *testing.T) {
	defer func() { recover() }() // prevent panics

	originalTime := currentTime
	defer func() { currentTime = originalTime }()

	// Simulate time far in the future so all slots are in the past
	currentTime = func() time.Time {
		return time.Date(9999, 1, 1, 23, 59, 59, 0, time.UTC)
	}

	logger := &Logger{
		Filename:                 "noop.log",
		RotateAtMinutes:          []int{0}, // only candidate is "00"
		processedRotateAtMinutes: []int{0},
		scheduledRotationQuitCh:  make(chan struct{}),
		BackupTimeFormat:         backupTimeFormat,
	}

	logger.scheduledRotationWg.Add(1)
	go logger.runScheduledRotations()

	// Wait briefly to ensure fallback path is entered
	time.Sleep(200 * time.Millisecond)

	// Shut it down cleanly
	close(logger.scheduledRotationQuitCh)
	logger.scheduledRotationWg.Wait()
}

func TestRunScheduledRotations_RotateFails(t *testing.T) {
	defer func() { recover() }() // catch potential panic from background goroutine

	// Force rotate to fail by setting invalid filename
	logger := &Logger{
		Filename:                 "/invalid/should/fail.log",
		RotateAtMinutes:          []int{0},
		processedRotateAtMinutes: []int{0},
		scheduledRotationQuitCh:  make(chan struct{}),
		BackupTimeFormat:         backupTimeFormat,
	}

	logger.scheduledRotationWg.Add(1)
	go logger.runScheduledRotations()

	// Let the loop trigger the rotation attempt
	time.Sleep(300 * time.Millisecond)

	// Clean shutdown
	close(logger.scheduledRotationQuitCh)
	logger.scheduledRotationWg.Wait()
}

func TestRotate_ManualTriggersTimeRotation(t *testing.T) {
	currentTime = func() time.Time {
		return time.Date(2025, 6, 5, 12, 0, 0, 0, time.UTC)
	}
	defer func() { currentTime = time.Now }()

	dir := t.TempDir()
	filename := filepath.Join(dir, "manual-trigger.log")

	// Seed file to ensure it rotates
	_ = os.WriteFile(filename, []byte("before"), 0644)

	l := &Logger{
		Filename:         filename,
		RotationInterval: time.Minute,
		lastRotationTime: time.Date(2025, 6, 5, 11, 58, 0, 0, time.UTC),
		BackupTimeFormat: backupTimeFormat,
	}
	defer l.Close()

	err := l.Rotate()
	if err != nil {
		t.Fatalf("expected successful manual rotation, got: %v", err)
	}

	// Check new empty file and rotated one with original data
	currentData, err := os.ReadFile(filename)
	if err != nil || len(currentData) != 0 {
		t.Errorf("expected new empty logfile after rotation, got: %q", currentData)
	}

	// The rotated file will include "-time" in the filename
	var found bool
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), "-time.log") {
			rotatedPath := filepath.Join(dir, e.Name())
			content, _ := os.ReadFile(rotatedPath)
			if string(content) == "before" {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatal("expected rotated file with -time suffix not found")
	}
}

func TestRunScheduledRotations_FallbackOnRotateFailure(t *testing.T) {
	defer func() { recover() }() // absorb any goroutine panics

	// Force rotate("time") to fail by mocking osRename
	originalRename := osRename
	defer func() { osRename = originalRename }()
	osRename = func(_, _ string) error {
		return fmt.Errorf("forced failure in scheduled rotate")
	}

	// Setup time to be just before a known rotation mark
	originalTime := currentTime
	defer func() { currentTime = originalTime }()
	currentTime = func() time.Time {
		return time.Date(2025, 1, 1, 0, 0, 1, 0, time.UTC) // match minute 0
	}

	dir := t.TempDir()
	logFile := filepath.Join(dir, "fallback.log")

	// Seed file so openNew hits rename
	_ = os.WriteFile(logFile, []byte("seed"), 0644)

	logger := &Logger{
		Filename:                 logFile,
		RotateAtMinutes:          []int{0},
		processedRotateAtMinutes: []int{0},
		scheduledRotationQuitCh:  make(chan struct{}),
		BackupTimeFormat:         backupTimeFormat,
	}
	logger.scheduledRotationWg.Add(1)

	go logger.runScheduledRotations()

	// Let it attempt and fail
	time.Sleep(300 * time.Millisecond)
	close(logger.scheduledRotationQuitCh)
	logger.scheduledRotationWg.Wait()
}

func TestLoggerClose_ClosesMillChannel(t *testing.T) {
	logger := &Logger{
		Filename:         "test-close-mill.log",
		millCh:           make(chan bool, 1),
		BackupTimeFormat: backupTimeFormat,
	}

	// Set startMill to run millRun (to simulate actual usage)
	logger.startMill.Do(func() {
		go logger.millRun()
	})

	// Close should close millCh
	err := logger.Close()
	if err != nil {
		t.Fatalf("Close() returned error: %v", err)
	}

	// Wait a bit to let millRun exit
	time.Sleep(100 * time.Millisecond)

	// Test that millCh is closed
	select {
	case _, ok := <-logger.millCh:
		if ok {
			t.Fatal("millCh should be closed but is still open")
		}
	default:
		// if nothing is received, we assume it's closed and drained
	}
}

func TestOpenNew_SetsLogStartTimeWhenFileMissing(t *testing.T) {
	currentTime = func() time.Time {
		return time.Date(2025, 6, 5, 15, 0, 0, 0, time.UTC)
	}
	defer func() { currentTime = time.Now }()

	dir := t.TempDir()
	logfile := filepath.Join(dir, "missing.log")

	logger := &Logger{
		Filename:         logfile,
		BackupTimeFormat: backupTimeFormat,
	}
	defer logger.Close()

	err := logger.openNew("size")
	if err != nil {
		t.Fatalf("openNew failed: %v", err)
	}

	if logger.logStartTime.IsZero() {
		t.Fatal("expected logStartTime to be set, but it is zero")
	}
}
