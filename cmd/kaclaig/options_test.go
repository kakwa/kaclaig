package main

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

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
	if opts.maxTries != 3 {
		t.Errorf("maxTries = %d, want 3", opts.maxTries)
	}
	if opts.timeout != 5*time.Minute {
		t.Errorf("timeout = %v, want 5m", opts.timeout)
	}
	if opts.semverConformance {
		t.Error("semverConformance should default to false")
	}
	if opts.newTag != "" || opts.oldTag != "" || opts.changelogFile != "" || opts.config != "" || opts.configBlob != "" {
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
		"-l", "model-x",
		"-b", "http://llm.example/v1",
		"-k", "secret",
		"-d", "/repo",
		"-n",
		"-r", "7",
		"-T", "10s",
		"-v",
	}
	opts, err := parseOptions(args, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}

	if opts.newTag != "1.2.0" || opts.oldTag != "v1.1.0" {
		t.Errorf("tag/from = %q/%q", opts.newTag, opts.oldTag)
	}
	if !opts.newMajor || !opts.newMinor || !opts.newPatch || !opts.unreleased {
		t.Errorf("bump flags not set: %+v", opts)
	}
	if opts.changelogFile != "CHANGELOG.md" || opts.config != "cfg.json" || opts.configBlob != `{"model":"x"}` {
		t.Errorf("changelog/config/blob = %q/%q/%q", opts.changelogFile, opts.config, opts.configBlob)
	}
	if opts.model != "model-x" || opts.baseURL != "http://llm.example/v1" || opts.apiKey != "secret" {
		t.Errorf("LLM options = %q/%q/%q", opts.model, opts.baseURL, opts.apiKey)
	}
	if opts.dir != "/repo" {
		t.Errorf("dir = %q", opts.dir)
	}
	if !opts.noWrite || !opts.verbose {
		t.Errorf("boolean options not set: %+v", opts)
	}
	if opts.maxTries != 7 || opts.timeout != 10*time.Second {
		t.Errorf("maxTries/timeout = %d/%v", opts.maxTries, opts.timeout)
	}
}

func TestParseOptionsLongForms(t *testing.T) {
	args := []string{
		"--tag=1.2.0",
		"--from=v1.1.0",
		"--major", "--minor", "--patch", "--unreleased",
		"--changelog=CHANGELOG.md",
		"--config=cfg.json",
		"--config-blob={\"model\":\"x\"}",
		"--model=model-x",
		"--base-url=http://llm.example/v1",
		"--api-key=secret",
		"--dir=/repo",
		"--no-write",
		"--semver-conformance-enabled",
		"--max-tries=7",
		"--timeout=10s",
		"--verbose",
	}
	opts, err := parseOptions(args, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}

	if opts.newTag != "1.2.0" || opts.oldTag != "v1.1.0" {
		t.Errorf("tag/from = %q/%q", opts.newTag, opts.oldTag)
	}
	if !opts.newMajor || !opts.newMinor || !opts.newPatch || !opts.unreleased {
		t.Errorf("bump flags not set: %+v", opts)
	}
	if opts.changelogFile != "CHANGELOG.md" || opts.config != "cfg.json" || opts.configBlob != `{"model":"x"}` {
		t.Errorf("changelog/config/blob = %q/%q/%q", opts.changelogFile, opts.config, opts.configBlob)
	}
	if opts.model != "model-x" || opts.baseURL != "http://llm.example/v1" || opts.apiKey != "secret" {
		t.Errorf("LLM options = %q/%q/%q", opts.model, opts.baseURL, opts.apiKey)
	}
	if opts.dir != "/repo" {
		t.Errorf("dir = %q", opts.dir)
	}
	if !opts.noWrite || !opts.semverConformance || !opts.verbose {
		t.Errorf("boolean options not set: %+v", opts)
	}
	if opts.maxTries != 7 || opts.timeout != 10*time.Second {
		t.Errorf("maxTries/timeout = %d/%v", opts.maxTries, opts.timeout)
	}
}

func TestParseOptionsLegacySingleDashLongForms(t *testing.T) {
	// These spellings were accepted by the old stdlib flag parser.
	args := []string{
		"-tag", "1.2.0",
		"-from", "v1.1.0",
		"-changelog", "CHANGELOG.md",
		"-model", "mock",
		"-base-url", "http://llm.example/v1",
		"-api-key", "test",
		"-dir", "/repo",
		"-no-write",
		"-max-tries", "7",
		"-timeout", "10s",
		"-semver-conformance-enabled",
	}
	opts, err := parseOptions(args, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}

	if opts.newTag != "1.2.0" || opts.oldTag != "v1.1.0" {
		t.Errorf("tag/from = %q/%q", opts.newTag, opts.oldTag)
	}
	if opts.changelogFile != "CHANGELOG.md" || opts.model != "mock" {
		t.Errorf("changelog/model = %q/%q", opts.changelogFile, opts.model)
	}
	if opts.baseURL != "http://llm.example/v1" || opts.apiKey != "test" {
		t.Errorf("baseURL/apiKey = %q/%q", opts.baseURL, opts.apiKey)
	}
	if opts.dir != "/repo" || !opts.noWrite || !opts.semverConformance {
		t.Errorf("dir/noWrite/semver = %q/%t/%t", opts.dir, opts.noWrite, opts.semverConformance)
	}
	if opts.maxTries != 7 || opts.timeout != 10*time.Second {
		t.Errorf("maxTries/timeout = %d/%v", opts.maxTries, opts.timeout)
	}
}

func TestParseOptionsCombinedShorthands(t *testing.T) {
	opts, err := parseOptions([]string{"-pv", "-t", "1.2.3"}, &bytes.Buffer{})
	if err != nil {
		t.Fatalf("parseOptions() error = %v", err)
	}
	if !opts.newPatch || !opts.verbose {
		t.Errorf("expected patch and verbose, got %+v", opts)
	}
	if opts.newTag != "1.2.3" {
		t.Errorf("tag = %q", opts.newTag)
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
		"-t, --tag",
		"-f, --from",
		"-M, --major",
		"-o, --changelog",
		"-c, --config",
		"-C, --config-blob",
		"--semver-conformance-enabled",
		"-v, --verbose",
		"Single-dash long options",
	} {
		if !strings.Contains(usage, want) {
			t.Errorf("help output missing %q:\n%s", want, usage)
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
		{name: "missing value", args: []string{"--tag"}, want: "flag needs an argument: --tag"},
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
