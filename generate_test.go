package kaclaig

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// testRepo sets up a temporary git repository with two tags and commits in
// between, and returns its path.
func testRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	// Backdating is done by setting GIT_AUTHOR_DATE / GIT_COMMITTER_DATE at
	// commit time. Committing and amending with a date can invoke the user's
	// editor in some environments, which would hang an unattended test.
	var commitDate string
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		// Never open an editor or prompt: tests must run unattended.
		env := append(os.Environ(),
			"GIT_EDITOR=true",
			"EDITOR=true",
			"GIT_TERMINAL_PROMPT=0",
			"GIT_PAGER=cat",
		)
		if commitDate != "" {
			env = append(env,
				"GIT_AUTHOR_DATE="+commitDate,
				"GIT_COMMITTER_DATE="+commitDate,
			)
		}
		cmd.Env = env
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("git %v: %v: %s", args, err, out)
		}
		return string(out)
	}

	run("init", "-q", "-b", "main")
	run("config", "user.email", "test@example.com")
	run("config", "user.name", "Test")

	commit := func(message, file, content, date string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, file), []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
		commitDate = date
		run("add", "-A")
		run("commit", "-q", "-m", message)
		commitDate = ""
	}

	commit("initial", "README.md", "# Test\n", "2025-01-10T00:00:00+00:00")
	run("tag", "v1.0.0")
	commit("add feature", "feat.go", "package test\n\n// Feature added.\nfunc Feature() {}\n", "2025-02-01T00:00:00+00:00")
	commit("fix bug", "fix.go", "package test\n\n// Fixed.\nfunc Fix() {}\n", "2025-02-02T00:00:00+00:00")
	run("tag", "v1.1.0")
	commit("add another feature", "more.go", "package test\n\n// More.\nfunc More() {}\n", "2025-03-01T00:00:00+00:00")
	commit("update docs", "README.md", "# Test\n\nUpdated docs.\n", "2025-03-02T00:00:00+00:00")
	return dir
}

func TestLatestTag(t *testing.T) {
	dir := testRepo(t)
	got, err := LatestTag(context.Background(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != "v1.1.0" {
		t.Errorf("LatestTag = %q, want v1.1.0", got)
	}
}

func TestPreviousTag(t *testing.T) {
	dir := testRepo(t)

	t.Run("parent of v1.1.0 is v1.0.0", func(t *testing.T) {
		got, err := PreviousTag(context.Background(), dir, "v1.1.0")
		if err != nil {
			t.Fatal(err)
		}
		if got != "v1.0.0" {
			t.Errorf("PreviousTag = %q, want v1.0.0", got)
		}
	})

	t.Run("parent of v1.0.0 is empty", func(t *testing.T) {
		got, err := PreviousTag(context.Background(), dir, "v1.0.0")
		if err != nil {
			t.Fatal(err)
		}
		if got != "" {
			t.Errorf("PreviousTag = %q, want empty", got)
		}
	})

	t.Run("no tags errors", func(t *testing.T) {
		bare := t.TempDir()
		cmd := exec.Command("git", "init", "-q")
		cmd.Dir = bare
		if err := cmd.Run(); err != nil {
			t.Skipf("git not available: %v", err)
		}
		if _, err := PreviousTag(context.Background(), bare, ""); err == nil {
			t.Error("expected error when no tags exist")
		}
	})
}

func TestCollectGitData(t *testing.T) {
	dir := testRepo(t)
	data, err := CollectGitData(context.Background(), dir, "v1.1.0", "v1.0.0")
	if err != nil {
		t.Fatal(err)
	}
	// The range is v1.0.0..HEAD, which includes the two commits up to v1.1.0
	// plus the two commits after it.
	if data.Count != 4 {
		t.Errorf("Count = %d, want 4", data.Count)
	}
	for _, want := range []string{"add feature", "fix bug", "add another feature", "update docs"} {
		if !strings.Contains(data.Log, want) {
			t.Errorf("Log missing expected commit %q:\n%s", want, data.Log)
		}
	}
	if !strings.Contains(data.Diff, "feat.go") {
		t.Errorf("Diff missing expected file: %q", data.Diff)
	}
}

func TestNextVersion(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "0.1.0"},
		{"v1.2.3", "1.2.4"},
		{"1.2.3", "1.2.4"},
		{"1.2.3-rc.1", "1.2.4-rc.1"},
		{"1.2", "1.3"},
		{"v2.0.0-alpha", "2.0.1-alpha"},
		{"not-a-version", "not-a-version-next"},
	}
	for _, c := range cases {
		if got := nextVersion(c.in); got != c.want {
			t.Errorf("nextVersion(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestBumpVersion(t *testing.T) {
	cases := []struct {
		latest, mode, want string
	}{
		{"", "patch", "0.1.0"},
		{"", "minor", "0.1.0"},
		{"", "major", "0.1.0"},
		{"v1.2.3", "patch", "1.2.4"},
		{"v1.2.3", "minor", "1.3.0"},
		{"v1.2.3", "major", "2.0.0"},
		{"1.2.3-rc.1", "patch", "1.2.4-rc.1"},
		{"1.2.3-rc.1", "minor", "1.3.0-rc.1"},
		{"1.2.3-rc.1", "major", "2.0.0-rc.1"},
		{"v1.2", "minor", "1.3.0"},
		{"v1.2", "major", "2.0.0"},
	}
	for _, c := range cases {
		if got := bumpVersion(c.latest, c.mode); got != c.want {
			t.Errorf("bumpVersion(%q, %q) = %q, want %q", c.latest, c.mode, got, c.want)
		}
	}
}

func TestValidateUnreleased(t *testing.T) {
	ok, reason := ValidateKeepAChangelog("## [Unreleased]\n\n### Added\n\n- New feature\n", "Unreleased")
	if !ok {
		t.Errorf("expected valid Unreleased section to pass, got: %s", reason)
	}

	invalid := []string{
		"",
		"## [1.2.0] - 2026-01-15\n\n- feature",
		"## [Unreleased]\n\nNo bullets here.\n",
		"## [Unreleased]\n\n- a\n\n## [Unreleased]\n\n- b",
		"## [Unreleased]\n\n- a\n\n## [1.2.0] - 2026-01-15\n\n- b",
	}
	for _, entry := range invalid {
		if ok, _ := ValidateKeepAChangelog(entry, "Unreleased"); ok {
			t.Errorf("expected invalid Unreleased section to fail: %q", entry)
		}
	}
}

func TestValidateKeepAChangelog(t *testing.T) {
	ok, reason := ValidateKeepAChangelog("## [1.2.0] - 2026-01-15\n\n### Added\n\n- New feature\n", "1.2.0")
	if !ok {
		t.Errorf("expected valid entry to pass, got: %s", reason)
	}

	invalid := []string{
		"",
		"# 1.2.0\n\n- feature",
		"## 1.2.0 - 2026-01-15\n\n- feature",
		"## [1.2.0]\n\n- feature",
		"## [1.2.0] - 2026-1-15\n\n- feature",
		"## [9.9.9] - 2026-01-15\n\n- feature",
		"## [1.2.0] - 2026-01-15\n\n## [1.3.0] - 2026-01-16\n\n- feature",
		"## [1.2.0] - 2026-01-15\n\nNo bullets here.\n",
	}
	for _, entry := range invalid {
		if ok, _ := ValidateKeepAChangelog(entry, "1.2.0"); ok {
			t.Errorf("expected invalid entry to fail: %q", entry)
		}
	}
}

func TestExtractEntry(t *testing.T) {
	good := "Here is the entry:\n\n## [1.2.0] - 2026-01-15\n\n### Added\n\n- feature\n\nThanks!"
	got := extractEntry(good, "1.2.0")
	if !strings.HasPrefix(got, "## [1.2.0]") {
		t.Errorf("extractEntry = %q, want header first", got)
	}
	if strings.Contains(got, "Here is") {
		t.Error("extractEntry kept preamble")
	}

	fenced := "```\n## [1.2.0] - 2026-01-15\n\n### Added\n\n- feature\n```"
	got = extractEntry(fenced, "1.2.0")
	if !strings.HasPrefix(got, "## [1.2.0]") {
		t.Errorf("extractEntry = %q, want header first", got)
	}
}

func jsonEscape(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '"':
			b.WriteString(`\"`)
		case '\\':
			b.WriteString(`\\`)
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func TestGeneratorWithFakeLLM(t *testing.T) {
	dir := testRepo(t)

	// Write an existing changelog with an Unreleased section.
	cl := filepath.Join(dir, "CHANGELOG.md")
	existing := "# Changelog\n\n## [Unreleased]\n\n### Added\n\n- Unreleased feature\n\n## [1.1.0] - 2025-01-10\n\n### Added\n\n- Released 1.1.0\n"
	if err := os.WriteFile(cl, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	entry := "## [1.1.1] - 2026-01-15\n\n### Added\n\n- Another feature\n\n### Fixed\n\n- A bug\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"` + jsonEscape(entry) + `"}}]}`))
	}))
	t.Cleanup(srv.Close)

	gen := &Generator{
		Dir:      dir,
		OldTag:   "v1.0.0",
		NewTag:   "1.1.1",
		KeepFile: cl,
		MaxTries: 2,
		Timeout:  time.Second * 5,
		BaseURL:  srv.URL + "/v1",
		APIKey:   "test",
	}
	out, err := gen.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != strings.TrimSpace(entry) {
		t.Errorf("Run output = %q, want %q", out, entry)
	}

	updated, err := os.ReadFile(cl)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), entry) {
		t.Error("changelog file was not updated with the new entry")
	}
	if strings.Contains(string(updated), "Unreleased") {
		t.Error("Unreleased section should have been replaced by the new entry")
	}
	if !strings.Contains(string(updated), "Released 1.1.0") {
		t.Error("existing entries must be preserved")
	}
}

func TestGeneratorRetriesOnInvalid(t *testing.T) {
	dir := testRepo(t)
	cl := filepath.Join(dir, "CHANGELOG.md")
	if err := os.WriteFile(cl, []byte("# Changelog\n\n## [1.1.0] - 2025-01-10\n\n- Old release\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	good := "## [1.1.1] - 2026-01-15\n\n### Added\n\n- Another feature\n"
	bad := "# 1.1.1\n\n- not keep a changelog"

	var calls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		reply := bad
		if calls >= 2 {
			reply = good
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"` + jsonEscape(reply) + `"}}]}`))
	}))
	t.Cleanup(srv.Close)

	// The semver check is disabled so the call count only reflects the
	// generation retries this test is about.
	disabled := false
	gen := &Generator{
		Dir:        dir,
		OldTag:     "v1.0.0",
		NewTag:     "1.1.1",
		KeepFile:   cl,
		MaxTries:   3,
		Timeout:    time.Second * 5,
		BaseURL:    srv.URL + "/v1",
		APIKey:     "test",
		WarnSemver: &disabled,
	}
	out, err := gen.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != strings.TrimSpace(good) {
		t.Errorf("Run output = %q, want %q", out, good)
	}
	if calls != 2 {
		t.Errorf("expected 2 LLM calls, got %d", calls)
	}
}

func TestGeneratorFailsAfterMaxTries(t *testing.T) {
	dir := testRepo(t)
	cl := filepath.Join(dir, "CHANGELOG.md")
	if err := os.WriteFile(cl, []byte("# Changelog\n\n## [1.1.0] - 2025-01-10\n\n- Old release\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	bad := "## [WRONG] - 2026-01-15\n\n- wrong version"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"` + jsonEscape(bad) + `"}}]}`))
	}))
	t.Cleanup(srv.Close)

	gen := &Generator{
		Dir:      dir,
		OldTag:   "v1.0.0",
		NewTag:   "1.1.1",
		KeepFile: cl,
		MaxTries: 2,
		Timeout:  time.Second * 5,
		BaseURL:  srv.URL + "/v1",
		APIKey:   "test",
	}
	if _, err := gen.Run(context.Background()); err == nil {
		t.Fatal("expected error when LLM keeps producing invalid entries")
	}
}

func TestGeneratorUnreleased(t *testing.T) {
	dir := testRepo(t)
	cl := filepath.Join(dir, "CHANGELOG.md")
	existing := "# Changelog\n\n## [Unreleased]\n\n### Added\n\n- Unreleased feature\n\n## [1.1.0] - 2025-01-10\n\n### Added\n\n- Released 1.1.0\n"
	if err := os.WriteFile(cl, []byte(existing), 0o644); err != nil {
		t.Fatal(err)
	}

	entry := "## [Unreleased]\n\n### Added\n\n- Unreleased feature\n- Another feature\n\n### Fixed\n\n- A bug\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"` + jsonEscape(entry) + `"}}]}`))
	}))
	t.Cleanup(srv.Close)

	gen := &Generator{
		Dir:      dir,
		OldTag:   "v1.0.0",
		NewTag:   "",
		Bump:     BumpUnreleased,
		KeepFile: cl,
		MaxTries: 2,
		Timeout:  time.Second * 5,
		BaseURL:  srv.URL + "/v1",
		APIKey:   "test",
	}
	out, err := gen.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != strings.TrimSpace(entry) {
		t.Errorf("Run output = %q, want %q", out, entry)
	}
	if gen.NewTag != "Unreleased" {
		t.Errorf("NewTag = %q, want Unreleased", gen.NewTag)
	}

	updated, err := os.ReadFile(cl)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(updated), "Another feature") {
		t.Error("changelog file was not updated with the new Unreleased entries")
	}
	if !strings.Contains(string(updated), "Released 1.1.0") {
		t.Error("existing entries must be preserved")
	}
	if strings.Count(string(updated), "## [Unreleased]") != 1 {
		t.Errorf("expected exactly one Unreleased section, got %d", strings.Count(string(updated), "## [Unreleased]"))
	}
}

func TestGeneratorNoWrite(t *testing.T) {
	dir := testRepo(t)
	cl := filepath.Join(dir, "CHANGELOG.md")
	original := "# Changelog\n\n## [1.1.0] - 2025-01-10\n\n- Old release\n"
	if err := os.WriteFile(cl, []byte(original), 0o644); err != nil {
		t.Fatal(err)
	}

	entry := "## [1.1.1] - 2026-01-15\n\n### Added\n\n- Another feature\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"` + jsonEscape(entry) + `"}}]}`))
	}))
	t.Cleanup(srv.Close)

	gen := &Generator{
		Dir:      dir,
		OldTag:   "v1.0.0",
		NewTag:   "1.1.1",
		KeepFile: cl,
		NoWrite:  true,
		MaxTries: 1,
		Timeout:  time.Second * 5,
		BaseURL:  srv.URL + "/v1",
		APIKey:   "test",
	}
	out, err := gen.Run(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out) != strings.TrimSpace(entry) {
		t.Errorf("Run output = %q, want %q", out, entry)
	}
	after, err := os.ReadFile(cl)
	if err != nil {
		t.Fatal(err)
	}
	if string(after) != original {
		t.Error("no-write mode must not modify the changelog file")
	}
}
