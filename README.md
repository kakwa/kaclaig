# kaclaig

[![CI](https://github.com/kakwa/kaclaig/actions/workflows/ci.yml/badge.svg)](https://github.com/kaclaig/kaclaig/actions/workflows/ci.yml)
[![codecov](https://codecov.io/gh/kakwa/kaclaig/branch/main/graph/badge.svg)](https://codecov.io/gh/kaclaig/kaclaig)

Keep A Changelog, AI Generated. A tool to automatically generate
[Keep a Changelog](https://keepachangelog.com/en/1.1.0/)-compliant
`CHANGELOG.md` entries from git history using any OpenAI-compatible LLM API.

`kaclaig` is a standalone CLI extracted from the
[git-pkgs/changelog](https://github.com/git-pkgs/changelog) generator. It
reads the git log and diff between the current state and the latest parent tag,
combines that with your existing changelog, and asks an LLM to write a new
entry. The entry is validated against the Keep a Changelog format; if it is
invalid, the reason is sent back to the LLM and it retries (up to 3 times).

## Installation

```bash
go install github.com/kaclaig/kaclaig/cmd/kaclaig@latest
```

Or build from source:

```bash
make build   # -> bin/kaclaig
make install # -> GOBIN
```

A man page is included at [`man/kaclaig.1`](man/kaclaig.1); view it with
`man -l man/kaclaig.1`.

## Usage

By default the tool prints a new entry for the existing `[Unreleased]` section
(it keeps any entries already there and adds the new ones from the commit log).
Pass `-w` to write it into the changelog file. Use `-p`, `-m`, or `-M` to
create a release instead, which bumps the version and writes a dated
`## [x.y.z] - YYYY-MM-DD` entry:

```bash
# Default: print the [Unreleased] entry; nothing is written
kaclaig

# Update the changelog file instead of printing only
kaclaig -w

# Create a release: bump the version, dated entry, Unreleased section is folded in
kaclaig -p    # patch: 0.2.1 -> 0.2.2
kaclaig -m    # minor: 0.2.1 -> 0.3.0
kaclaig -M    # major: 0.2.1 -> 1.0.0

# Choose versions explicitly (overrides the bump flags)
kaclaig --new-version 1.2.0 --from v1.1.0

# Verbose mode shows the validation attempts
kaclaig -v
```

Every option has a short form (e.g. `-p`) and a long form (e.g. `--patch`);
both can be used interchangeably. Short options can be combined (`-pv`), and
values can be passed as a separate argument or with `=`
(`--new-version=1.2.0`). Single-dash long spellings (e.g. `-new-version`) are
also accepted.

Version bumping is handled by
[Masterminds/semver](https://pkg.go.dev/github.com/Masterminds/semver/v3);
prerelease labels (e.g. `1.2.3-rc.1`) are preserved when bumping.

### Options

| Flag                   | Description                                                          |
| ---------------------- | -------------------------------------------------------------------- |
| `-M`, `--major`        | Bump the major version (reset the rest) and create a release         |
| `-m`, `--minor`        | Bump the minor version (reset patch) and create a release            |
| `-p`, `--patch`        | Bump the patch version and create a release                          |
| `-u`, `--unreleased`   | Generate an `[Unreleased]` entry (default)                           |
| `-S`, `--semver-check` | Enable the semver conformance check (default: off)                   |
| `-t`, `--new-version`  | New version to generate (overrides the bump flags)                   |
| `-f`, `--from`         | Previous tag to diff against (default: latest parent tag)            |
| `-o`, `--changelog`    | Path to the changelog file (default: auto-detect)                    |
| `-c`, `--config`       | Path to an LLM config file (opencode.json format)                    |
| `-C`, `--config-blob`  | Inline LLM config JSON blob, same format as `--config`; overrides it |
| `-d`, `--dir`          | Git repository directory (default: current directory)                |
| `-w`, `--write`        | Update the changelog file (default: print only)                      |
| `-v`, `--verbose`      | Verbose output                                                       |
| `-V`, `--version`      | Print the version and exit                                           |

`--version` prints the version recorded in the Go build info, or `dev` for
local builds. Set it explicitly at build time with
`-ldflags "-X main.version=vX.Y.Z"` when needed.

The generated entries follow the [Keep a Changelog 1.1.0](https://keepachangelog.com/en/1.1.0/)
specification: the standard categories (Added, Changed, Deprecated, Removed,
Fixed, Security), only the sections that apply, and concise user-facing
bullet points in the imperative mood.

### Semver conformance check

The semver conformance check is **off by default**; enable it with
`-S` / `--semver-check`. When enabled, `kaclaig` checks that the
version bump is consistent with the change, per
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

It never runs for `[Unreleased]` updates, since no bump is being made.

### LLM configuration

Connection settings are resolved in priority order:

1. the `-C` / `--config-blob` inline JSON (same format as the config file)
2. the `-c` / `--config` config file
3. `$HOME/.kaclaig-llm/kaclaig.json`
4. `$HOME/.config/opencode/opencode.json` / `opencode.jsonc`
5. environment variables (`LLM_MODEL`, `LLM_BASE_URL`, `LLM_API_KEY`, `OPENAI_*`)

The config file (and the `-C` blob) uses the opencode.json provider format:

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

To pass the config inline instead of in a file, quote the same JSON for `-C`:

```bash
kaclaig -C '{"providers":{"local":{"settings":{"baseURL":"https://ai.example.com/v1","apiKey":"secret"}}},"model":"local/qwen3.8-27b"}'
```

`-C` overrides `-c` when both are given.

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
		// NewTag / Bump, Model / Config / ConfigBlob, NoWrite, ...
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

### Releasing

Releases are handled by four make targets. All use `kaclaig` to generate or
validate the changelog entry and run its semver conformance check by default:

```bash
make version-bump-minor  # writes CHANGELOG.md, never commits
make version-bump-major  # idem, major bump
make version-bump-patch  # idem, patch bump
# review and commit the change yourself
make tag                 # requires a clean tree; validates the release, then tags HEAD
```

The `version-bump-*` targets write the new release entry but deliberately
leave it uncommitted. `tag` refuses to run if `git status --porcelain` reports
anything (tracked modifications or untracked files), takes the version from
the newest released `## [x.y.z]` heading in `CHANGELOG.md` (ignoring
`[Unreleased]`), validates that version with `kaclaig` in print-only mode, and
creates an annotated tag `x.y.z` with message `release x.y.z`. It fails if the
tag already exists or the semver check rejects the bump.

Opt-outs:

- `KACLIAIG=0` skips `kaclaig` entirely. The `version-bump-*` targets then only
  prepend a bare version header (no changelog generation, no semver check);
  `tag` only checks the tree and creates the tag.
- `SEMVER_CHECK=0` keeps changelog generation but omits `-S` / `--semver-check`.
- `KACLIAIG_FLAGS='...'` passes extra flags to `kaclaig`, e.g.
  `KACLIAIG_FLAGS="-C '{\"model\":...}'"` for an inline LLM config.
- `CHANGELOG=path/to/CHANGELOG.md` overrides the changelog file used by both
  the make logic and `kaclaig`.

The tests require `git` on `PATH` and run unattended: they create temporary
repositories with backdated tags and use local `httptest` servers as mock
LLMs, so no API key or network access is needed. Continuous integration runs
the same suite in GitHub Actions
([`.github/workflows/ci.yml`](.github/workflows/ci.yml)), including an
end-to-end smoke test of the built binary against a local mock LLM.

## License

MIT — see [LICENSE](LICENSE).
