# Changelog

## [1.2.0](https://github.com/DeRuina/timberjack/compare/v1.1.0...v1.2.0) (2025-05-27)


### Features

* release please script ([42d3575](https://github.com/DeRuina/timberjack/commit/42d35750d4f0f5cfac7c339ba9dcdee77527ab72))
* release please script ([7514015](https://github.com/DeRuina/timberjack/commit/751401565635ff4eecbaffdf82e2333973cfe18a))

## [1.1.0] - 2025-05-27

### Added
- Support for time-based log rotation via `RotationInterval` configuration
- Rotation reason (`-time`, `-size`) included in backup filenames
- Platform-specific file ownership preservation (`chown_linux.go`)
- Enhanced filename parsing to recognize timestamp and rotation reason
- Extensive unit tests for time-based rotation, compression, and ownership
- Default filename uses `-timberjack.log` if none is provided

### Changed
- Refactored rotation logic to support time-based, size-based, and manual triggers uniformly
- Replaced deprecated `ioutil.ReadDir` with modern `os.ReadDir`
- Improved compression logic to handle chown and cleanup safely

### Fixed
- Preserved original file mode and ownership during rotation and compression
- Resolved edge cases in backup name parsing with improved robustness

### Removed
- Legacy logic relying solely on size-based rotation
