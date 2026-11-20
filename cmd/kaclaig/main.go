package main

import (
	"context"
	"errors"
	"fmt"
	"os"

	"github.com/spf13/pflag"

	"github.com/kaclaig/kaclaig"
)

func main() {
	opts, err := parseOptions(os.Args[1:], os.Stderr)
	if err != nil {
		if errors.Is(err, pflag.ErrHelp) {
			return
		}
		os.Exit(2)
	}

	// Validate the LLM config sources up front so bad input fails fast.
	if opts.configBlob != "" {
		if _, err := kaclaig.ParseLLMConfigBlob([]byte(opts.configBlob)); err != nil {
			fmt.Fprintln(os.Stderr, "error: parsing -C config blob:", err)
			os.Exit(1)
		}
	}
	if opts.config != "" {
		if _, err := kaclaig.LoadLLMSettings(opts.config); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
	}

	bump := kaclaig.BumpUnreleased
	switch {
	case opts.newMajor:
		bump = kaclaig.BumpMajor
	case opts.newMinor:
		bump = kaclaig.BumpMinor
	case opts.newPatch:
		bump = kaclaig.BumpPatch
	}

	semverConformance := opts.semverConformance
	gen := &kaclaig.Generator{
		Dir:               opts.dir,
		NewTag:            opts.newTag,
		OldTag:            opts.oldTag,
		Bump:              bump,
		Model:             opts.model,
		MaxTries:          opts.maxTries,
		KeepFile:          opts.changelogFile,
		NoWrite:           opts.noWrite,
		Verbose:           opts.verbose,
		Timeout:           opts.timeout,
		BaseURL:           opts.baseURL,
		APIKey:            opts.apiKey,
		Config:            opts.config,
		ConfigBlob:        opts.configBlob,
		SemverConformance: &semverConformance,
	}

	entry, err := gen.Run(context.Background())
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}

	if opts.noWrite {
		fmt.Println(entry)
	} else {
		fmt.Fprintln(os.Stderr, "Done. Entry:")
		fmt.Println(entry)
	}
}
