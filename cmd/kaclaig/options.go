package main

import (
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/spf13/pflag"
)

// options holds every CLI setting before it is mapped onto kaclaig.Generator.
type options struct {
	newVersion        string
	oldTag            string
	newMajor          bool
	newMinor          bool
	newPatch          bool
	unreleased        bool
	changelogFile     string
	config            string
	configBlob        string
	dir               string
	write             bool
	semverConformance bool
	verbose           bool
	showVersion       bool
}

// legacyLongNames are the long option names that are also accepted with a
// single dash. They are normalised to two dashes before parsing so
// invocations such as `-new-version X` keep working.
var legacyLongNames = map[string]bool{
	"new-version":  true,
	"from":         true,
	"major":        true,
	"minor":        true,
	"patch":        true,
	"unreleased":   true,
	"changelog":    true,
	"config":       true,
	"dir":          true,
	"write":        true,
	"verbose":      true,
	"semver-check": true,
	"version":      true,
}

// ErrVersion signals that --version was requested. Like ErrHelp, it tells the
// caller to exit successfully without running the generator.
var ErrVersion = errors.New("version requested")

// normalizeLegacyArgs rewrites single-dash long options (`-new-version`) to
// their canonical double-dash form (`--new-version`). Short options and
// shorthand clusters (`-p`, `-pv`) are left untouched.
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
	// Keep registration order in help output so related flags stay grouped.
	fs.SortFlags = false

	fs.BoolVarP(&opts.newMajor, "major", "M", false, "bump the major version segment (ignored with --new-version)")
	fs.BoolVarP(&opts.newMinor, "minor", "m", false, "bump the minor version segment, resetting patch (ignored with --new-version)")
	fs.BoolVarP(&opts.newPatch, "patch", "p", false, "bump the patch version segment (ignored with --new-version)")
	fs.BoolVarP(&opts.unreleased, "unreleased", "u", false, "update the [Unreleased] section instead of creating a release (default)")
	fs.BoolVarP(&opts.semverConformance, "semver-check", "S", false, "enable the semver conformance check for release bumps (off by default)")
	fs.StringVarP(&opts.newVersion, "new-version", "t", "", "new version `TAG` to generate (default: derived from latest tag)")
	fs.StringVarP(&opts.oldTag, "from", "f", "", "previous `TAG` to diff against (default: latest parent tag)")
	fs.StringVarP(&opts.changelogFile, "changelog", "o", "", "path to the changelog `FILE` (default: auto-detect in the current directory)")
	fs.StringVarP(&opts.config, "config", "c", "", "path to an opencode-style LLM config `FILE` (default: $HOME/.kaclaig-llm/kaclaig.json, then $HOME/.config/opencode/opencode.json)")
	fs.StringVarP(&opts.configBlob, "config-blob", "C", "", "inline LLM config `JSON` blob, same format as --config; overrides it")
	fs.StringVarP(&opts.dir, "dir", "d", ".", "git repository `DIR` (default: current directory)")
	fs.BoolVarP(&opts.write, "write", "w", false, "update the changelog file (default: print the generated entry)")
	fs.BoolVarP(&opts.verbose, "verbose", "v", false, "verbose output")
	fs.BoolVarP(&opts.showVersion, "version", "V", false, "print the version and exit")

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
}

// parseOptions parses args using GNU-style short and long options. Parse
// errors and help output are written to out; ErrHelp and ErrVersion are
// returned unchanged so callers can exit successfully.
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

	if opts.showVersion {
		return nil, ErrVersion
	}

	if extra := fs.Args(); len(extra) > 0 {
		err := fmt.Errorf("unexpected arguments: %s", strings.Join(extra, " "))
		fmt.Fprintf(out, "error: %v\n", err)
		fs.Usage()
		return nil, err
	}

	return opts, nil
}
