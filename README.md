# timberjack [![Go Reference](https://pkg.go.dev/badge/github.com/DeRuina/timberjack.svg)](https://pkg.go.dev/github.com/DeRuina/timberjack) [![Go Report Card](https://goreportcard.com/badge/github.com/DeRuina/timberjack)](https://goreportcard.com/report/github.com/DeRuina/timberjack) ![Audit](https://github.com/DeRuina/timberjack/actions/workflows/audit.yaml/badge.svg) ![Version](https://img.shields.io/github/v/tag/DeRuina/timberjack?sort=semver)


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

To use timberjack with the standard library's `log` package:

```go
log.SetOutput(&timberjack.Logger{
    Filename:         "/var/log/myapp/foo.log",
    MaxSize:          500,            // megabytes
    MaxBackups:       3,              // backups
    MaxAge:           28,             // days
    Compress:         true,           // default: false
    LocalTime:        true,           // default: false
    RotationInterval: time.Hour * 24, // rotate daily
})
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
}
```


## How Rotation Works

1. **Size-Based**: If a write causes the log file to exceed `MaxSize`, it is rotated.
2. **Time-Based**: If `RotationInterval` has elapsed since the log file was created, it is rotated.
   The rotated filename reflects the **start time** of the log file (when it was first opened), not the time it was renamed.
3. **Manual**: You can call `Logger.Rotate()` directly to force a rotation.

Rotated files are renamed using the pattern:

```
<name>-<timestamp>-<reason>.log
```

For example:

```
/var/log/myapp/foo-2025-04-30T15-00-00.000-size.log
/var/log/myapp/foo-2025-04-30T22-15-42.123-time.log
```

## Log Cleanup

When a new log file is created:
- Older backups beyond `MaxBackups` are deleted.
- Files older than `MaxAge` days are deleted.
- If `Compress` is true, older files are gzip-compressed.


## Contributing

We welcome contributions!  
Please see our [contributing guidelines](CONTRIBUTING.md) before submitting a pull request.


## License

MIT