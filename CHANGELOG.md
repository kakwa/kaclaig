## [Unreleased]

### Added
- add a `-C` flag to pass the LLM config inline as a JSON blob (same
  format as the `-c` config file); it takes precedence over `-c`
- add a `--semver-check` flag to opt in to the semver
  conformance check, which is now disabled by default
- add codecov coverage reporting and project badges

### Changed
- rework CLI flags and improve option parsing and usage output

### Removed
- the `--base-url`, `--api-key`, and `--timeout` flags: connection
  settings now live in the `-C`/`-c` config blob or environment
  variables (the per-request LLM timeout now defaults to 5 minutes)
- the `--no-semver` flag: the semver conformance check is disabled by
  default and is enabled with `--semver-check`

### Fixed
- fix smoke test and CI pipeline


## [0.1.0] - 2026-11-19

### Added
- add support for initial project code
- add changelog and README

