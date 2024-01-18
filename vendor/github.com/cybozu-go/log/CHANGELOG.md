# Change Log

All notable changes to this project will be documented in this file.
This project adheres to [Semantic Versioning](https://semver.org/).

## [Unreleased]

## [1.7.0] - 2023-02-01
### Changed
- Improve performance of map iteration using reflect.MapIter in [#34](https://github.com/cybozu-go/log/pull/34)
- Use buffered channel for signal.Notify in [#37](https://github.com/cybozu-go/log/pull/37)
- Fix for deprecated "io/ioutil" in [#39](https://github.com/cybozu-go/log/pull/39)

### Removed
- Remove "Requirements" section from README in [#41](https://github.com/cybozu-go/log/pull/41)
    - Please use the latest Go.

### Fixed
- Fix JSONFormat to ensure outputting JSON on NaN and Infinities in [#29](https://github.com/cybozu-go/log/pull/29)
- Fix JSONFormat to output valid JSON strings in [#33](https://github.com/cybozu-go/log/pull/33)

## [1.6.1] - 2021-12-14
### Changed
- Remove dependency on [github.com/pkg/errors](https://github.com/pkg/errors) ([#28](https://github.com/cybozu-go/log/pull/28)).

## [1.6.0] - 2020-03-19
### Changed
- Validate string with strings.ToValidUTF8 ([#26](https://github.com/cybozu-go/log/pull/26)).

## [1.5.0] - 2018-09-14
### Added
- Opting in to [Go modules](https://github.com/golang/go/wiki/Modules).

## [1.4.2] - 2017-09-07
### Changed
- Fix a bug in `Logger.Writer` ([#16](https://github.com/cybozu-go/log/pull/16)).

## [1.4.1] - 2017-05-08
### Changed
- Call error handler in `Logger.WriteThrough` ([#13](https://github.com/cybozu-go/log/pull/13)).

## [1.4.0] - 2017-04-28
### Added
- `Logger.SetErrorHandler` to change error handler function.

### Changed
- Logger has a default error handler that exits with status 5 on EPIPE ([#11](https://github.com/cybozu-go/log/pull/11)).

## [1.3.0] - 2016-09-10
### Changed
- Fix Windows support by [@mattn](https://github.com/mattn).
- Fix data races in tests.
- Formatters now have an optional `Utsname` field.

## [1.2.0] - 2016-08-26
### Added
- `Logger.WriteThrough` to write arbitrary bytes to the underlying writer.

## [1.1.2] - 2016-08-25
### Changed
- These interfaces are formatted nicer in logs.
    - [`encoding.TextMarshaler`](https://golang.org/pkg/encoding/#TextMarshaler)
    - [`json.Marshaler`](https://golang.org/pkg/encoding/json/#Marshaler)
    - [`error`](https://golang.org/pkg/builtin/#error)

## [1.1.1] - 2016-08-24
### Added
- `FnError` field name constant for error strings.
- [SPEC] add "exec" and "http" log types.

### Changed
- Invalid UTF-8 string no longer results in an error.

## [1.1.0] - 2016-08-22
### Changed
- `Logger.Writer`: fixed a minor bug.
- "id" log field is renamed to "request_id" (API is not changed).

## [1.0.1] - 2016-08-20
### Changed
- [Logger.Writer](https://godoc.org/github.com/cybozu-go/log#Logger.Writer) is rewritten for better performance.

[Unreleased]: https://github.com/cybozu-go/log/compare/v1.7.0...HEAD
[1.7.0]: https://github.com/cybozu-go/log/compare/v1.6.1...v1.7.0
[1.6.1]: https://github.com/cybozu-go/log/compare/v1.6.0...v1.6.1
[1.6.0]: https://github.com/cybozu-go/log/compare/v1.5.0...v1.6.0
[1.5.0]: https://github.com/cybozu-go/log/compare/v1.4.2...v1.5.0
[1.4.2]: https://github.com/cybozu-go/log/compare/v1.4.0...v1.4.2
[1.4.1]: https://github.com/cybozu-go/log/compare/v1.4.0...v1.4.1
[1.4.0]: https://github.com/cybozu-go/log/compare/v1.3.0...v1.4.0
[1.3.0]: https://github.com/cybozu-go/log/compare/v1.2.0...v1.3.0
[1.2.0]: https://github.com/cybozu-go/log/compare/v1.1.2...v1.2.0
[1.1.2]: https://github.com/cybozu-go/log/compare/v1.1.1...v1.1.2
[1.1.1]: https://github.com/cybozu-go/log/compare/v1.1.0...v1.1.1
[1.1.0]: https://github.com/cybozu-go/log/compare/v1.0.1...v1.1.0
[1.0.1]: https://github.com/cybozu-go/log/compare/v1.0.0...v1.0.1
