package main

import (
	"context"
	"flag"
	"fmt"
	"os"

	"github.com/kaclaig/kaclaig"
)

func main() {
	var (
		newTag        = flag.String("tag", "", "new version/tag to generate (default: derived from latest tag)")
		oldTag        = flag.String("from", "", "previous tag to diff against (default: latest parent tag)")
		newMajor      = flag.Bool("M", false, "bump the major version segment (ignored with -tag)")
		newMinor      = flag.Bool("m", false, "bump the minor version segment, resetting patch (ignored with -tag)")
		newPatch      = flag.Bool("p", false, "bump the patch version segment (ignored with -tag)")
		unreleased    = flag.Bool("u", false, "update the [Unreleased] section instead of creating a release (default)")
		changelogFile = flag.String("changelog", "", "path to the changelog file (default: auto-detect in the current directory)")
		configFile    = flag.String("c", "", "path to an opencode-style LLM config file (default: $HOME/.kaclaig-llm/kaclaig.json, then $HOME/.config/opencode/opencode.json)")
		configBlob    = flag.String("C", "", "inline LLM config JSON blob, same format as the -c config file; overrides it")
		model         = flag.String("model", "", "LLM model name, overriding the config and environment (e.g. qwen3.8-27b)")
		maxTries      = flag.Int("max-tries", 3, "maximum number of LLM attempts before giving up")
		dir           = flag.String("dir", ".", "directory of the git repository (default: current directory)")
		noWrite       = flag.Bool("no-write", false, "print the generated entry instead of updating the changelog file")
		semverCheck   = flag.Bool("semver-conformance-enabled", false, "enable the semver conformance check for release bumps (off by default)")
		verbose       = flag.Bool("v", false, "verbose output")
	)
	flag.Usage = func() {
		out := flag.CommandLine.Output()
		fmt.Fprintln(out, "kaclaig - generate a Keep a Changelog entry from git history using an LLM")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "usage: kaclaig [options]")
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "options:")
		flag.PrintDefaults()
		fmt.Fprintln(out, "")
		fmt.Fprintln(out, "LLM settings are resolved in priority order: -model flag,")
		fmt.Fprintln(out, "the -C inline config JSON blob, the -c config file (opencode.json format),")
		fmt.Fprintln(out, "then the default config locations, then environment variables")
		fmt.Fprintln(out, "(LLM_MODEL, LLM_BASE_URL, LLM_API_KEY, OPENAI_*).")
		fmt.Fprintln(out, "The generated entry is validated against the Keep a Changelog 1.1.0")
		fmt.Fprintln(out, "format; invalid entries are sent back to the LLM for correction.")
		fmt.Fprintln(out, "The optional semver conformance check (enabled with --semver-conformance-enabled)")
		fmt.Fprintln(out, "verifies that release version bumps match the changes they describe.")
	}
	flag.Parse()

	// Validate the LLM config sources up front so bad input fails fast.
	if *configBlob != "" {
		if _, err := kaclaig.ParseLLMConfigBlob([]byte(*configBlob)); err != nil {
			fmt.Fprintln(os.Stderr, "error: parsing -C config blob:", err)
			os.Exit(1)
		}
	}
	if *configFile != "" {
		if _, err := kaclaig.LoadLLMSettings(*configFile); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	}

	bump := kaclaig.BumpUnreleased
	switch {
	case *newMajor:
		bump = kaclaig.BumpMajor
	case *newMinor:
		bump = kaclaig.BumpMinor
	case *newPatch:
		bump = kaclaig.BumpPatch
	default:
		// -u (unreleased) is the default mode; it needs no extra handling.
		_ = *unreleased
	}

	gen := &kaclaig.Generator{
		Dir:               *dir,
		NewTag:            *newTag,
		OldTag:            *oldTag,
		Bump:              bump,
		Model:             *model,
		MaxTries:          *maxTries,
		KeepFile:          *changelogFile,
		NoWrite:           *noWrite,
		Verbose:           *verbose,
		Config:            *configFile,
		ConfigBlob:        *configBlob,
		SemverConformance: semverCheck,
	}

	entry, err := gen.Run(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	if *noWrite {
		fmt.Println(entry)
	} else {
		fmt.Fprintln(os.Stderr, "Done. Entry:")
		fmt.Println(entry)
	}
}
