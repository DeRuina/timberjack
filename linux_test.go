//go:build linux
// +build linux

package timberjack

import (
	"os"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

type fakeFile struct {
	uid int
	gid int
}

type fakeFS struct {
	mu    sync.Mutex
	files map[string]fakeFile
}

func newFakeFS() *fakeFS {
	return &fakeFS{files: make(map[string]fakeFile)}
}

func (fs *fakeFS) Chown(name string, uid, gid int) error {
	fs.mu.Lock()
	fs.files[name] = fakeFile{uid: uid, gid: gid}
	fs.mu.Unlock()
	return nil
}

func (fs *fakeFS) Stat(name string) (os.FileInfo, error) {
	info, err := os.Stat(name)
	if err != nil {
		return nil, err
	}
	stat := info.Sys().(*syscall.Stat_t)
	stat.Uid = 555
	stat.Gid = 666
	return info, nil
}

// accessors for tests (so they don’t read the map without lock)
func (fs *fakeFS) Owner(name string) (uid, gid int, ok bool) {
	fs.mu.Lock()
	defer fs.mu.Unlock()
	ff, ok := fs.files[name]
	return ff.uid, ff.gid, ok
}

func TestMaintainMode(t *testing.T) {
	currentTime = fakeTime
	dir := makeTempDir("TestMaintainMode", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)

	mode := os.FileMode(0600)
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, mode)
	isNil(err, t)
	f.Close()

	l := &Logger{
		Filename:   filename,
		MaxBackups: 1,
		MaxSize:    100, // megabytes
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	newFakeTime()

	err = l.Rotate()
	isNil(err, t)

	filename2 := backupFileWithReason(dir, "size")
	info, err := os.Stat(filename)
	isNil(err, t)
	info2, err := os.Stat(filename2)
	isNil(err, t)
	equals(mode, info.Mode(), t)
	equals(mode, info2.Mode(), t)
}

func TestMaintainOwner(t *testing.T) {
	fakeFS := newFakeFS()
	osChown = fakeFS.Chown
	osStat = fakeFS.Stat
	defer func() {
		osChown = os.Chown
		osStat = os.Stat
	}()

	currentTime = fakeTime
	dir := makeTempDir("TestMaintainOwner", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)

	f, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0644)
	isNil(err, t)
	f.Close()

	l := &Logger{
		Filename:   filename,
		MaxBackups: 1,
		MaxSize:    100, // megabytes
	}
	defer l.Close()

	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	newFakeTime()

	err = l.Rotate()
	isNil(err, t)

	uid, gid, ok := fakeFS.Owner(filename)
	if !ok {
		t.Fatalf("owner for %s not recorded", filename)
	}
	equals(555, uid, t)
	equals(666, gid, t)
}

func TestCompressMaintainMode(t *testing.T) {
	currentTime = fakeTime

	dir := makeTempDir("TestCompressMaintainMode", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)

	mode := os.FileMode(0600)
	f, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, mode)
	isNil(err, t)
	f.Close()

	l := &Logger{
		Compress:   true,
		Filename:   filename,
		MaxBackups: 1,
		MaxSize:    100, // megabytes
	}
	defer l.Close()
	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	newFakeTime()

	err = l.Rotate()
	isNil(err, t)

	// we need to wait a little bit since the files get compressed on a different
	// goroutine.
	<-time.After(10 * time.Millisecond)

	// a compressed version of the log file should now exist with the correct
	// mode.
	filename2 := backupFileWithReason(dir, "size")
	info, err := os.Stat(filename)
	isNil(err, t)
	info2, err := os.Stat(filename2 + compressSuffix)
	isNil(err, t)
	equals(mode, info.Mode(), t)
	equals(mode, info2.Mode(), t)
}

func TestCompressMaintainOwner(t *testing.T) {
	fakeFS := newFakeFS()
	osChown = fakeFS.Chown
	osStat = fakeFS.Stat
	defer func() {
		osChown = os.Chown
		osStat = os.Stat
	}()

	currentTime = fakeTime
	dir := makeTempDir("TestCompressMaintainOwner", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)

	f, err := os.OpenFile(filename, os.O_CREATE|os.O_RDWR, 0644)
	isNil(err, t)
	f.Close()

	l := &Logger{
		Compress:   true,
		Filename:   filename,
		MaxBackups: 1,
		MaxSize:    100, // megabytes
	}
	defer l.Close()

	b := []byte("boo!")
	n, err := l.Write(b)
	isNil(err, t)
	equals(len(b), n, t)

	newFakeTime()

	err = l.Rotate()
	isNil(err, t)

	// compression happens in mill goroutine
	<-time.After(10 * time.Millisecond)

	// check owner of compressed backup
	filename2 := backupFileWithReason(dir, "size")
	name := filename2 + compressSuffix

	uid, gid, ok := fakeFS.Owner(name)
	if !ok {
		t.Fatalf("owner for %s not recorded", name)
	}
	equals(555, uid, t)
	equals(666, gid, t)
}

type badFileInfo struct{}

func (badFileInfo) Name() string       { return "bad" }
func (badFileInfo) Size() int64        { return 0 }
func (badFileInfo) Mode() os.FileMode  { return 0644 }
func (badFileInfo) ModTime() time.Time { return time.Now() }
func (badFileInfo) IsDir() bool        { return false }
func (badFileInfo) Sys() interface{}   { return nil }

func TestChownInvalidSys(t *testing.T) {
	err := chown("fake.log", badFileInfo{})
	if err == nil || !strings.Contains(err.Error(), "failed to get syscall.Stat_t") {
		t.Fatalf("expected chown to fail on invalid Sys(), got: %v", err)
	}
}
