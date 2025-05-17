# timberjack [![Go Reference](https://pkg.go.dev/badge/github.com/DeRuina/timberjack.svg)](https://pkg.go.dev/github.com/DeRuina/timberjack) [![Go Report Card](https://goreportcard.com/badge/github.com/DeRuina/timberjack)](https://goreportcard.com/report/github.com/DeRuina/timberjack) ![Build Status](https://github.com/DeRuina/timberjack/actions/workflows/ci.yml/badge.svg) ![Version](https://img.shields.io/github/v/tag/DeRuina/timberjack?sort=semver)


### Timberjack is a Go package for writing logs to rolling files.

Timberjack is a forked and enhanced version of [`lumberjack`](https://github.com/natefinch/lumberjack), adding features such as time-based rotation and better testability.
Package `timberjack` provides a rolling logger with support for size-based and time-based log rotation.


## Installation

```bash
go get github.com/DeRuina/timberjack
```


## Import

```go
import "github.com/DeRuina/timberjack"
```

Timberjack is intended to be one part of a logging infrastructure. It is a pluggable
component that manages log file writing and rotation. It plays well with any logging package that can write to an
`io.Writer`, including the standard library's `log` package.

> ⚠️ Timberjack assumes that only one process writes to the log file. Using the same configuration from multiple
> processes on the same machine may result in unexpected behavior.


## Example

To use timberjack with the standard library's `log` package, including interval-based and scheduled minute-based rotation:

```go
import (
	"log"
	"time"
	"github.com/DeRuina/timberjack"
)

func main() {
	logger := &timberjack.Logger{
		Filename:   "/var/log/myapp/foo.log", // Choose an appropriate path
		MaxSize:    500,            // megabytes
		MaxBackups: 3,              // backups
		MaxAge:     28,             // days
		Compress:   true,           // default: false
		LocalTime:  true,           // default: false (use UTC)
		RotationInterval: time.Hour * 24, // Rotate daily if no other rotation met
		RotateAtMinutes: []int{0, 15, 30, 45}, // Also rotate at HH:00, HH:15, HH:30, HH:45
	}
	log.SetOutput(logger)
	defer logger.Close() // Ensure logger is closed on application exit to stop goroutines

	// Example log writes
	log.Println("Application started")
	// ... your application logic ...
	log.Println("Application shutting down")
}
```

To trigger rotation on demand (e.g. in response to `SIGHUP`):

```go
l := &timberjack.Logger{}
log.SetOutput(l)
c := make(chan os.Signal, 1)
signal.Notify(c, syscall.SIGHUP)

go func() {
    for range c {
        l.Rotate()
    }
}()
```


## Logger Configuration

```go
type Logger struct {
    Filename         string        // File to write logs to
    MaxSize          int           // Max size (MB) before rotation (default: 100)
    MaxAge           int           // Max age (days) to retain old logs
    MaxBackups       int           // Max number of backups to keep
    LocalTime        bool          // Use local time in rotated filenames
    Compress         bool          // Compress rotated logs (gzip)
    RotationInterval time.Duration // Rotate after this duration (if > 0)
    RotateAtMinutes []int          // Specific minutes within an hour (0-59) to trigger a rotation.
}
```


## How Rotation Works

1. **Size-Based**: If a write operation causes the current log file to exceed `MaxSize`, the file is rotated before the write. The backup filename will include `-size` as the reason.
2. **Time-Based**: If `RotationInterval` is set (e.g., `time.Hour * 24` for daily rotation) and this duration has passed since the last rotation (of any type that updates the interval timer), the file is rotated upon the next write. The backup filename will include `-time` as the reason.
3. **Scheduled Minute-Based**: If `RotateAtMinutes` is configured (e.g., `[]int{0, 30}` to rotate at `HH:00:00` and `HH:30:00`), a dedicated goroutine will trigger a rotation when the current time matches one of these minute marks. This rotation also uses `-time` as the reason in the backup filename.
4. **Manual**: You can call `Logger.Rotate()` directly to force a rotation at any time. The reason in the backup filename will be `"-time"` if an interval rotation was also due, otherwise it defaults to `"-size"`.

Rotated files are renamed using the pattern:

```
<name>-<timestamp>-<reason>.log
```

For example:

```
/var/log/myapp/foo-2025-04-30T15-00-00.000-size.log
/var/log/myapp/foo-2025-04-30T22-15-42.123-time.log
/var/log/myapp/foo-2025-05-01T10:30:00.000-time.log.gz (if scheduled at HH:30 and compressed)
```

## Log Cleanup

When a new log file is created:
- Older backups beyond `MaxBackups` are deleted.
- Files older than `MaxAge` days are deleted.
- If `Compress` is true, older files are gzip-compressed.


## License

MIT