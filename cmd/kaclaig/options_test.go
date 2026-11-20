package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/pflag"
)

func TestParseOptionsDefaults(t *testing.T) {
	var out bytes.Buffer
	opts, err := parseOptions(nil, &out)
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}
	if out.Len() != 0 {
		t.Fatalf("unexpected output: %q", out.String())
	}
	if opts.dir != "." {
		t.Errorf("dir = %q, want %q", opts.dir, ".")
	}
	if opts.semverConformance {
		t.Error("semverConformance should default to false")
	}
	if opts.write {
		t.Error("write should default to false")
	}
	if opts.newVersion != "" || opts.oldTag != "" || opts.changelogFile != "" || opts.config != "" || opts.configBlob != "" {
		t.Errorf("expected empty string options, got %+v", opts)
	}
}

func TestParseOptionsShortForms(t *testing.T) {
	args := []string{
		"-t", "1.2.0",
		"-f", "v1.1.0",
		"-M", "-m", "-p", "-u",
		"-o", "CHANGELOG.md",
		"-c", "cfg.json",
		"-C", `{"model":"x"}`,
		"-d", "/repo",
		"-w",
		"-S",
		"-v",
	}
	opts, err := parseOptions(args, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}

	if opts.newVersion != "1.2.0" || opts.oldTag != "v1.1.0" {
		t.Errorf("tag/from = %q/%q", opts.newVersion, opts.oldTag)
	}
	if !opts.newMajor || !opts.newMinor || !opts.newPatch || !opts.unreleased {
		t.Errorf("bump flags not set: %+v", opts)
	}
	if opts.changelogFile != "CHANGELOG.md" || opts.config != "cfg.json" || opts.configBlob != `{"model":"x"}` {
		t.Errorf("changelog/config/blob = %q/%q/%q", opts.changelogFile, opts.config, opts.configBlob)
	}
	if opts.dir != "/repo" {
		t.Errorf("dir = %q", opts.dir)
	}
	if !opts.write || !opts.semverConformance || !opts.verbose {
		t.Errorf("boolean options not set: %+v", opts)
	}
}

func TestParseOptionsLongForms(t *testing.T) {
	args := []string{
		"--new-version=1.2.0",
		"--from=v1.1.0",
		"--major", "--minor", "--patch", "--unreleased",
		"--changelog=CHANGELOG.md",
		"--config=cfg.json",
		"--config-blob={\"model\":\"x\"}",
		"--dir=/repo",
		"--write",
		"--semver-check",
		"--verbose",
	}
	opts, err := parseOptions(args, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}

	if opts.newVersion != "1.2.0" || opts.oldTag != "v1.1.0" {
		t.Errorf("tag/from = %q/%q", opts.newVersion, opts.oldTag)
	}
	if !opts.newMajor || !opts.newMinor || !opts.newPatch || !opts.unreleased {
		t.Errorf("bump flags not set: %+v", opts)
	}
	if opts.changelogFile != "CHANGELOG.md" || opts.config != "cfg.json" || opts.configBlob != `{"model":"x"}` {
		t.Errorf("changelog/config/blob = %q/%q/%q", opts.changelogFile, opts.config, opts.configBlob)
	}
	if opts.dir != "/repo" {
		t.Errorf("dir = %q", opts.dir)
	}
	if !opts.write || !opts.semverConformance || !opts.verbose {
		t.Errorf("boolean options not set: %+v", opts)
	}
}

func TestParseOptionsLegacySingleDashLongForms(t *testing.T) {
	// These spellings were accepted by the old stdlib flag parser.
	args := []string{
		"-new-version", "1.2.0",
		"-from", "v1.1.0",
		"-changelog", "CHANGELOG.md",
		"-dir", "/repo",
		"-write",
		"-semver-check",
	}
	opts, err := parseOptions(args, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}

	if opts.newVersion != "1.2.0" || opts.oldTag != "v1.1.0" {
		t.Errorf("tag/from = %q/%q", opts.newVersion, opts.oldTag)
	}
	if opts.changelogFile != "CHANGELOG.md" {
		t.Errorf("changelog = %q", opts.changelogFile)
	}
	if opts.dir != "/repo" || !opts.write || !opts.semverConformance {
		t.Errorf("dir/write/semver = %q/%t/%t", opts.dir, opts.write, opts.semverConformance)
	}
}

func TestParseOptionsCombinedShorthands(t *testing.T) {
	opts, err := parseOptions([]string{"-pSv", "-t", "1.2.3"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}
	if !opts.newPatch || !opts.semverConformance || !opts.verbose {
		t.Errorf("expected patch, semver conformance and verbose, got %+v", opts)
	}
	if opts.newVersion != "1.2.3" {
		t.Errorf("tag = %q", opts.newVersion)
	}
}

func TestParseOptionsHelp(t *testing.T) {
	var out bytes.Buffer
	_, err := parseOptions([]string{"--help"}, &out)
	if !errors.Is(err, pflag.ErrHelp) {
		t.Fatalf("error = %v, want pflag.ErrHelp", err)
	}
	usage := out.String()
	for _, want := range []string{
		"-o, --changelog FILE",
		"-c, --config FILE",
		"-C, --config-blob JSON",
		"-d, --dir DIR",
		"-f, --from TAG",
		"-t, --new-version TAG",
		"-S, --semver-check",
		"-w, --write",
		"-v, --verbose",
	} {
		if !strings.Contains(usage, want) {
			t.Errorf("help output missing %q:\n%s", want, usage)
		}
	}
	for _, gone := range []string{
		"--model", "--base-url", "--api-key", "--timeout", "--max-tries", "--tag",
		"--no-write", "--semver-conformance-enabled",
		"LLM settings are resolved", "Single-dash long options",
		"Keep a Changelog 1.1.0", "semver conformance check verifies",
	} {
		if strings.Contains(usage, gone) {
			t.Errorf("help output still contains %q:\n%s", gone, usage)
		}
	}
	// Bump flags and the semver check are listed together, in this order.
	prev := -1
	for _, want := range []string{
		"-M, --major", "-m, --minor", "-p, --patch", "-u, --unreleased", "-S, --semver-check",
	} {
		i := strings.Index(usage, want)
		if i < 0 {
			t.Errorf("help output missing %q:\n%s", want, usage)
			continue
		}
		if i <= prev {
			t.Errorf("help output flags out of order at %q:\n%s", want, usage)
		}
		prev = i
	}
}

func TestParseOptionsRemovedFlags(t *testing.T) {
	for _, args := range [][]string{
		{"-l", "model-x"},
		{"--model", "model-x"},
		{"-b", "http://llm.example/v1"},
		{"--base-url", "http://llm.example/v1"},
		{"-k", "secret"},
		{"--api-key", "secret"},
		{"-T", "10s"},
		{"--timeout", "10s"},
		{"-model", "model-x"},
		{"-base-url", "http://llm.example/v1"},
		{"-api-key", "secret"},
		{"-timeout", "10s"},
		{"-r", "7"},
		{"--max-tries", "7"},
		{"-max-tries", "7"},
		{"-n"},
		{"--no-write"},
		{"-no-write"},
		{"--tag", "1.2.0"},
		{"-tag", "1.2.0"},
	} {
		if _, err := parseOptions(args, &bytes.Buffer{}); err == nil {
			t.Errorf("parseOptions(%q) succeeded, want error", args)
		}
	}
}

func TestParseOptionsErrors(t *testing.T) {
	tests := []struct {
		name string
		args []string
		want string
	}{
		{name: "unknown flag", args: []string{"--nope"}, want: "unknown flag: --nope"},
		{name: "missing value", args: []string{"--new-version"}, want: "flag needs an argument: --new-version"},
		{name: "positional arg", args: []string{"extra"}, want: "unexpected arguments: extra"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var out bytes.Buffer
			_, err := parseOptions(tt.args, &out)
			if err == nil {
				t.Fatal("expected an error")
			}
			if !strings.Contains(out.String(), "error: "+tt.want) {
				t.Errorf("output = %q, want it to contain %q", out.String(), tt.want)
			}
			if !strings.Contains(out.String(), "usage: kaclaig [options]") {
				t.Errorf("output did not include usage:\n%s", out.String())
			}
		})
	}
}
