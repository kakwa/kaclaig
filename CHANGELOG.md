## [0.3.0] - 2026-11-25

### Changed
- improve test generation and git data handling

## [0.2.0] - 2026-11-20

### Fixed
- fix badge URLs in the project README
- 2026-11-20
- fix smoke test and CI pipeline
- fix option and entry merge behavior

### Added
- add long option support and improve CLI argument parsing
- add a man page

### Changed
- rework CLI flags and improve option parsing and usage output
- add codecov coverage reporting and project badges
- add a `-C` flag to pass the LLM config inline as a JSON blob (same format as the `-c` config file); it takes precedence over `-c`
- add a `--semver-check` flag to opt in to the semver conformance check, which is now disabled by default

### Removed
- the `--base-url`, `--api-key`, and `--timeout` flags: connection settings now live in the `-C`/`-c` config blob or environment variables (the per-request LLM timeout now defaults to 5 minutes)
- the `--no-semver` flag: the semver conformance check is disabled by default and is enabled with `--semver-check`

## [0.1.0] - 2026-11-19

### Added
- add support for initial project code
- add changelog and README
