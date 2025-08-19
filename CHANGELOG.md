# Changelog

## [3.0.0](https://github.com/DeRuina/timberjack/compare/v2.0.1...v3.0.0) (2025-08-19)


### ⚠ BREAKING CHANGES

* `BackupTimeFormat` field is now required for Logger instance to work. Returns error if invalid value is passed.

### Features

* read group permission on newly created files ([#30](https://github.com/DeRuina/timberjack/issues/30)) ([ee44715](https://github.com/DeRuina/timberjack/commit/ee447152a04d62ae12811a2212815f8960ca0d9d))
* release please script ([42d3575](https://github.com/DeRuina/timberjack/commit/42d35750d4f0f5cfac7c339ba9dcdee77527ab72))
* release please script ([7514015](https://github.com/DeRuina/timberjack/commit/751401565635ff4eecbaffdf82e2333973cfe18a))
* Rotation Suffix Time Format ([e2c2211](https://github.com/DeRuina/timberjack/commit/e2c22115ae301c034e07c703ab9729d25b170a49))
* **rotation:** add RotateAtMinutes support ([e4c22b6](https://github.com/DeRuina/timberjack/commit/e4c22b6858ea7ca2493a1c6af4a6032f5e2ea95c))
* **rotation:** add RotateAtMinutes support ([2e93add](https://github.com/DeRuina/timberjack/commit/2e93adddf122269e2043506a5b7a46b4106eea86))
* use log file creation time in rotated filenames ([3d87292](https://github.com/DeRuina/timberjack/commit/3d87292ec8b528e24dc325c51a137170e49ca0f7))
* use log file creation time in rotated filenames ([fa3bfd0](https://github.com/DeRuina/timberjack/commit/fa3bfd082947e261c003cbe1134af52428dea4dc))


### Bug Fixes

* bump go version to 1.21 ([9bdd903](https://github.com/DeRuina/timberjack/commit/9bdd9038638e72a7fb330fe97f8c730864b9cbd5))
* millRun goroutine leak fix ([28bf784](https://github.com/DeRuina/timberjack/commit/28bf784b830e5f839054f7d82950087e323b958f))
* Prevent panic on write after close and improve shutdown robustness ([#25](https://github.com/DeRuina/timberjack/issues/25)) ([332b9c2](https://github.com/DeRuina/timberjack/commit/332b9c2553d63f5eafdce47237d29b510609f823))
* test release flow from v1.3.1 ([bef4fe1](https://github.com/DeRuina/timberjack/commit/bef4fe1e31bc8a3099e8fc94605ebf345b5c8b52))
* truncateFractional bug fix ([9a6f908](https://github.com/DeRuina/timberjack/commit/9a6f908d270ddfa45df66621b0b12b1ff44ab28f))

## [1.3.4](https://github.com/DeRuina/timberjack/compare/v1.3.3...v1.3.4) (2025-08-05)

### Features

* read group permission on newly created files ([#30](https://github.com/DeRuina/timberjack/issues/30)) ([ee44715](https://github.com/DeRuina/timberjack/commit/ee447152a04d62ae12811a2212815f8960ca0d9d))

## [1.3.3](https://github.com/DeRuina/timberjack/compare/v1.3.2...v1.3.3) (2025-07-24)

### Bug Fixes

*  Prevent panic on write after close and improve shutdown robustness ([#25](https://github.com/DeRuina/timberjack/issues/25)) ([332b9c2](https://github.com/DeRuina/timberjack/commit/332b9c2553d63f5eafdce47237d29b510609f823))


## [1.3.2](https://github.com/DeRuina/timberjack/compare/v1.3.1...v1.3.2) (2025-07-21)

### Bug Fixes

* millRun goroutine leak fix ([28bf784](https://github.com/DeRuina/timberjack/commit/28bf784b830e5f839054f7d82950087e323b958f))


## [1.3.1](https://github.com/DeRuina/timberjack/compare/v1.3.0...v1.3.1) (2025-07-17)


### Features

* `BackupTimeFormat` field is now required for Logger instance to work. Returns error if invalid value is passed.
* Rotation Suffix Time Format ([e2c2211](https://github.com/DeRuina/timberjack/commit/e2c22115ae301c034e07c703ab9729d25b170a49))

### Bug Fixes

* truncateFractional bug fix ([9a6f908](https://github.com/DeRuina/timberjack/commit/9a6f908d270ddfa45df66621b0b12b1ff44ab28f))


## [1.3.0](https://github.com/DeRuina/timberjack/compare/v1.2.0...v1.3.0) (2025-06-04)


### Features

* **rotation:** add RotateAtMinutes support ([e4c22b6](https://github.com/DeRuina/timberjack/commit/e4c22b6858ea7ca2493a1c6af4a6032f5e2ea95c))
* **rotation:** add RotateAtMinutes support ([2e93add](https://github.com/DeRuina/timberjack/commit/2e93adddf122269e2043506a5b7a46b4106eea86))

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
