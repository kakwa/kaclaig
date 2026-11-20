package main

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/spf13/pflag"
)

// options holds every CLI setting before it is mapped onto kaclaig.Generator.
type options struct {
	newTag            string
	oldTag            string
	newMajor          bool
	newMinor          bool
	newPatch          bool
	unreleased        bool
	changelogFile     string
	config            string
	configBlob        string
	model             string
	baseURL           string
	apiKey            string
	dir               string
	noWrite           bool
	semverConformance bool
	maxTries          int
	timeout           time.Duration
	verbose           bool
}

// legacyLongNames are the long option names that were already accepted with a
// single dash by the old flag-based parser. They are normalised to two dashes
// before parsing so existing invocations such as `-tag X` keep working.
var legacyLongNames = map[string]bool{
	"tag":                        true,
	"from":                       true,
	"major":                      true,
	"minor":                      true,
	"patch":                      true,
	"unreleased":                 true,
	"changelog":                  true,
	"config":                     true,
	"model":                      true,
	"base-url":                   true,
	"api-key":                    true,
	"dir":                        true,
	"no-write":                   true,
	"max-tries":                  true,
	"timeout":                    true,
	"verbose":                    true,
	"semver-conformance-enabled": true,
}

// normalizeLegacyArgs rewrites single-dash long options (`-tag`) to their
// canonical double-dash form (`--tag`). Short options and shorthand clusters
// (`-p`, `-pv`) are left untouched.
func normalizeLegacyArgs(args []string) []string {
	normalized := make([]string, len(args))
	for i, arg := range args {
		if len(arg) > 2 && arg[0] == '-' && arg[1] != '-' {
			name := arg[1:]
			if eq := strings.IndexByte(name, '='); eq >= 0 {
				name = name[:eq]
			}
			if legacyLongNames[name] {
				normalized[i] = "-" + arg
				continue
			}
		}
		normalized[i] = arg
	}
	return normalized
}

func newFlagSet(opts *options, out io.Writer) *pflag.FlagSet {
	fs := pflag.NewFlagSet("kaclaig", pflag.ContinueOnError)
	fs.SetOutput(out)

	fs.StringVarP(&opts.newTag, "tag", "t", "", "new version/tag to generate (default: derived from latest tag)")
	fs.StringVarP(&opts.oldTag, "from", "f", "", "previous tag to diff against (default: latest parent tag)")
	fs.BoolVarP(&opts.newMajor, "major", "M", false, "bump the major version segment (ignored with --tag)")
	fs.BoolVarP(&opts.newMinor, "minor", "m", false, "bump the minor version segment, resetting patch (ignored with --tag)")
	fs.BoolVarP(&opts.newPatch, "patch", "p", false, "bump the patch version segment (ignored with --tag)")
	fs.BoolVarP(&opts.unreleased, "unreleased", "u", false, "update the [Unreleased] section instead of creating a release (default)")
	fs.StringVarP(&opts.changelogFile, "changelog", "o", "", "path to the changelog file (default: auto-detect in the current directory)")
	fs.StringVarP(&opts.config, "config", "c", "", "path to an opencode-style LLM config file (default: $HOME/.kaclaig-llm/kaclaig.json, then $HOME/.config/opencode/opencode.json)")
	fs.StringVarP(&opts.configBlob, "config-blob", "C", "", "inline LLM config JSON blob, same format as --config; overrides it")
	fs.StringVarP(&opts.model, "model", "l", "", "LLM model name, overriding the config and environment (e.g. qwen3.8-27b)")
	fs.StringVarP(&opts.baseURL, "base-url", "b", "", "OpenAI-compatible API base URL, overriding the config and environment")
	fs.StringVarP(&opts.apiKey, "api-key", "k", "", "API key, overriding the config and environment")
	fs.StringVarP(&opts.dir, "dir", "d", ".", "directory of the git repository (default: current directory)")
	fs.BoolVarP(&opts.noWrite, "no-write", "n", false, "print the generated entry instead of updating the changelog file")
	fs.BoolVar(&opts.semverConformance, "semver-conformance-enabled", false, "enable the semver conformance check for release bumps (off by default)")
	fs.IntVarP(&opts.maxTries, "max-tries", "r", 3, "maximum number of LLM attempts before giving up")
	fs.DurationVarP(&opts.timeout, "timeout", "T", 5*time.Minute, "per-request LLM timeout")
	fs.BoolVarP(&opts.verbose, "verbose", "v", false, "verbose output")

	fs.Usage = func() { printUsage(out, fs) }
	return fs
}

func printUsage(out io.Writer, fs *pflag.FlagSet) {
	fmt.Fprintln(out, "kaclaig - generate a Keep a Changelog entry from git history using an LLM")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "usage: kaclaig [options]")
	fmt.Fprintln(out)
	fmt.Fprintln(out, "options:")
	fmt.Fprint(out, fs.FlagUsages())
	fmt.Fprintln(out)
	fmt.Fprintln(out, "LLM settings are resolved in priority order: --model/--base-url/--api-key flags,")
	fmt.Fprintln(out, "the -C/--config-blob inline JSON, the -c/--config file (opencode.json format),")
	fmt.Fprintln(out, "then the default config locations, then environment variables")
	fmt.Fprintln(out, "(LLM_MODEL, LLM_BASE_URL, LLM_API_KEY, OPENAI_*).")
	fmt.Fprintln(out, "The generated entry is validated against the Keep a Changelog 1.1.0")
	fmt.Fprintln(out, "format; invalid entries are sent back to the LLM for correction.")
	fmt.Fprintln(out, "The optional semver conformance check (enabled with --semver-conformance-enabled)")
	fmt.Fprintln(out, "verifies that release version bumps match the changes they describe.")
	fmt.Fprintln(out, "Single-dash long options (e.g. -tag) are also accepted for compatibility.")
}

// parseOptions parses args using GNU-style short and long options. Parse
// errors and help output are written to out; ErrHelp is returned unchanged so
// callers can exit successfully without printing anything else.
func parseOptions(args []string, out io.Writer) (*options, error) {
	opts := &options{}
	fs := newFlagSet(opts, out)

	if err := fs.Parse(normalizeLegacyArgs(args)); err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return nil, err
		}
		fmt.Fprintf(out, "error: %v\n", err)
		fs.Usage()
		return nil, err
	}

	if extra := fs.Args(); len(extra) > 0 {
		err := fmt.Errorf("unexpected arguments: %s", strings.Join(extra, " "))
		fmt.Fprintf(out, "error: %v\n", err)
		fs.Usage()
		return nil, err
	}

	return opts, nil
}
