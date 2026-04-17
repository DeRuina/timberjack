//go:build !windows

package timberjack

import (
	"os"
	"syscall"
	"testing"
)

func TestOpenNewDefaultPerm(t *testing.T) {
	// if runtime.GOOS == "windows" {
	// 	t.Skip("Skipping default perm test on Windows")
	// }

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
	// if runtime.GOOS == "windows" {
	// 	t.Skip("Skipping custom perm test on Windows")
	// }

	// Ensure no bits get masked out.
	syscall.Umask(0o000)

	dir := makeTempDir("TestOpenNewCustomPerm", t)
	defer os.RemoveAll(dir)

	filename := logFile(dir)
	l := &Logger{
		Filename: filename,
		FileMode: 0o747,
	}
	_, err := l.Write([]byte("foo"))
	isNil(err, t)
	hasPerm(filename, 0o747, t)
	l.Close()

	filename += ".1"
	l = &Logger{
		Filename: filename,
		FileMode: 0o200,
	}
	_, err = l.Write([]byte("foo"))
	isNil(err, t)
	hasPerm(filename, 0o200, t)
	l.Close()

	filename += ".2"
	l = &Logger{
		Filename: filename,
		FileMode: 0o666,
	}
	_, err = l.Write([]byte("foo"))
	isNil(err, t)
	hasPerm(filename, 0o666, t)
	l.Close()
}
