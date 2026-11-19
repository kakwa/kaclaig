# kaclaig

[![CI](https://github.com/kaclaig/kaclaig/actions/workflows/ci.yml/badge.svg)](https://github.com/kaclaig/kaclaig/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/kaclaig/kaclaig/branch/main/graph/badge.svg)](https://codecov.io/gh/kaclaig/kaclaig)

Keep A Changelog, AI Generated. A tool to automatically generate
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/)-compliant
`CHANGELOG.md` entries from git history using any OpenAI-compatible LLM API.

`kaclaig` is a standalone CLI extracted from the
[git-pkgs/changelog](https://github.com/git-pkgs/changelog) generator. It
reads the git log and diff between the current state and the latest parent tag,
combines that with your existing changelog, and asks an LLM to write a new
entry. The entry is validated against the Keep a Changelog format; if it is
invalid, the reason is sent back to the LLM and it retries (up to
`--max-tries`).

## Installation

```bash
go install github.com/kaclaig/kaclaig/cmd/kaclaig@latest
```

Or build from source:

```bash
make build   # -> bin/kaclaig
make install # -> GOBIN
```

## Usage

By default the tool writes into the existing `[Unreleased]` section (it keeps
any entries already there and adds the new ones from the commit log). Use
`-p`, `-m`, or `-M` to create a release instead, which bumps the version and
writes a dated `## [x.y.z] - YYYY-MM-DD` entry:

```bash
# Default: update the [Unreleased] section
kaclaig

# Create a release: bump the version, dated entry, Unreleased section is folded in
kaclaig -p    # patch: 0.2.1 -> 0.2.2
kaclaig -m    # minor: 0.2.1 -> 0.3.0
kaclaig -M    # major: 0.2.1 -> 1.0.0

# Choose versions explicitly (overrides the bump flags)
kaclaig --tag 1.2.0 --from v1.1.0

# Just print the entry, don't touch the file
kaclaig --no-write

# Verbose mode shows the validation attempts
kaclaig -v
```

Version bumping is handled by
[Masterminds/semver](https://pkg.go.dev/github.com/Masterminds/semver/v3);
prerelease labels (e.g. `1.2.3-rc.1`) are preserved when bumping.

### Options

| Flag | Description |
| --- | --- |
| `-u` | Update the `[Unreleased]` section (default) |
| `-p` | Bump the patch version and create a release |
| `-m` | Bump the minor version (reset patch) and create a release |
| `-M` | Bump the major version (reset the rest) and create a release |
| `--tag` | New version/tag to generate (overrides the bump flags) |
| `--from` | Previous tag to diff against (default: latest parent tag) |
| `--changelog` | Path to the changelog file (default: auto-detect) |
| `-c` | Path to an LLM config file (opencode.json format) |
| `--model` / `--base-url` / `--api-key` | Override LLM connection settings |
| `--dir` | Git repository directory (default: current directory) |
| `--no-write` | Print the entry instead of updating the file |
| `--no-semver` | Disable the semver conformance check (default: on) |
| `--max-tries` | Maximum LLM attempts (default: 3) |
| `--timeout` | Per-request LLM timeout (default: 5m) |
| `-v` | Verbose output |

The generated entries follow the [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/)
specification: the standard categories (Added, Changed, Deprecated, Removed,
Fixed, Security), only the sections that apply, and concise user-facing
bullet points in the imperative mood.

### Semver conformance check

After validation, `kaclaig` checks that the version bump is consistent with
the change, per
[Semantic Versioning](https://semver.org/):

- **Over-zealous** (warning): a major bump with no breaking change. The
  entry is still written; a warning is printed on stderr.
- **Under-zealous** (error): breaking changes shipped on a minor or patch
  bump. `kaclaig` exits with an error and nothing is written.

Breaking changes are detected structurally first — a `### Removed` section
or an inline `BREAKING` / `**BREAKING**` / `BREAKING CHANGE:` marker in any
bullet — and the LLM is only consulted for entries with no structural
signal. When a check fails, a second LLM call produces a short explanation
pointing at the offending bullet(s) and, when the diff stat names files, the
files involved:

```
error: semver: breaking changes are present but the version was only bumped
from v1.0.0 to 1.0.1 (minor/patch); a major version bump is required per
semver
    The entry removes the --legacy flag (see "Removed" section), which is a
    breaking change. Implicated file: cmd/tool/main.go.
```

Disable the check with `--no-semver`. It never runs for `[Unreleased]`
updates, since no bump is being made.

### LLM configuration

Connection settings are resolved in priority order:

1. `--model`, `--base-url`, `--api-key` flags
2. the `-c <config file>` flag
3. `$HOME/.kaclaig-llm/kaclaig.json`
4. `$HOME/.config/opencode/opencode.json`
5. environment variables (`LLM_MODEL`, `LLM_BASE_URL`, `LLM_API_KEY`, `OPENAI_*`)

The config file uses the opencode.json provider format:

```json
{
  "providers": {
    "local": {
      "name": "Local AI",
      "package": "@opencode/ai/providers/openai-compatible",
      "settings": {
        "baseURL": "https://ai.example.com/v1",
        "apiKey": "secret"
      },
      "models": {
        "qwen3.8-27b": {
          "name": "Qwen 3.8 27B"
        }
      }
    }
  },
  "model": "local/qwen3.8-27b"
}
```

The `model` field selects a provider and model as `<provider>/<model>`.

## Library usage

`kaclaig` is also importable as a library:

```go
package main

import (
	"context"
	"fmt"
	"time"

	"github.com/kaclaig/kaclaig"
)

func main() {
	gen := &kaclaig.Generator{
		Dir:     ".",
		MaxTries: 3,
		Timeout:  5 * time.Minute,
		// NewTag / Bump, BaseURL / APIKey / Model / Config, NoWrite, ...
	}
	entry, err := gen.Run(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		return
	}
	fmt.Println(entry)
}
```

Key types and functions:

- `Generator` — orchestrates git collection, the LLM call, validation, and
  writing. See `Generator.Run`.
- `CollectGitData(ctx, dir, newTag, oldTag)` — gathers log and diff stat for
  a range.
- `LatestTag` / `PreviousTag` — tag discovery.
- `LoadLLMSettings(explicit)` — resolve LLM settings from a config file.
- `ValidateKeepAChangelog(entry, version)` — validate an entry against the
  Keep a Changelog format.

## Development

```bash
make all        # fmt-check + vet + build
make test       # run all tests
make test-race  # tests with the race detector
make coverage   # tests with a coverage summary (coverage.out)
make vet        # go vet
make fmt        # gofmt
make build      # build the CLI
```

The tests require `git` on `PATH` and run unattended: they create temporary
repositories with backdated tags and use local `httptest` servers as mock
LLMs, so no API key or network access is needed. Continuous integration runs
the same suite in GitHub Actions
([`.github/workflows/ci.yml`](.github/workflows/ci.yml)), including an
end-to-end smoke test of the built binary against a local mock LLM.

## License

MIT — see [LICENSE](LICENSE).
