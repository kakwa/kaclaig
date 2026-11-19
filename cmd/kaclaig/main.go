package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

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
		config        = flag.String("c", "", "path to an opencode-style LLM config file (default: $HOME/.kaclaig-llm/kaclaig.json, then $HOME/.config/opencode/opencode.json)")
		model         = flag.String("model", "", "LLM model name, overriding the config file and environment (e.g. qwen3.8-27b)")
		baseURL       = flag.String("base-url", "", "OpenAI-compatible API base URL, overriding the config file and environment")
		apiKey        = flag.String("api-key", "", "API key, overriding the config file and environment")
		dir           = flag.String("dir", ".", "directory of the git repository (default: current directory)")
		noWrite       = flag.Bool("no-write", false, "print the generated entry instead of updating the changelog file")
		noSemver      = flag.Bool("no-semver", false, "disable the semver conformance check")
		maxTries      = flag.Int("max-tries", 3, "maximum number of LLM attempts before giving up")
		timeout       = flag.Duration("timeout", 5*time.Minute, "per-request LLM timeout")
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
		fmt.Fprintln(out, "LLM settings are resolved in priority order: -model/-base-url/-api-key flags,")
		fmt.Fprintln(out, "the -c config file (opencode.json format), then environment variables")
		fmt.Fprintln(out, "(LLM_MODEL, LLM_BASE_URL, LLM_API_KEY, OPENAI_*).")
		fmt.Fprintln(out, "The generated entry is validated against the Keep a Changelog 1.1.0")
		fmt.Fprintln(out, "format; invalid entries are sent back to the LLM for correction.")
	}
	flag.Parse()

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

	// If an explicit config file is given, validate it up front.
	if *config != "" {
		if _, err := kaclaig.LoadLLMSettings(*config); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	}

	warnSemver := !*noSemver
	gen := &kaclaig.Generator{
		Dir:        *dir,
		NewTag:     *newTag,
		OldTag:     *oldTag,
		Bump:       bump,
		Model:      *model,
		MaxTries:   *maxTries,
		KeepFile:   *changelogFile,
		NoWrite:    *noWrite,
		Verbose:    *verbose,
		Timeout:    *timeout,
		BaseURL:    *baseURL,
		APIKey:     *apiKey,
		Config:     *config,
		WarnSemver: &warnSemver,
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
