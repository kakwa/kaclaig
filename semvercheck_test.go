package kaclaig

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mockLLM returns an *LLM backed by a fake OpenAI-compatible server whose
// handler is called for every chat completion request.
func mockLLM(t *testing.T, handler http.HandlerFunc) *LLM {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	llm, err := NewLLM(LLMConfig{BaseURL: srv.URL + "/v1", APIKey: "test"})
	if err != nil {
		t.Fatal(err)
	}
	return llm
}

// replyJSON wraps a string as an OpenAI chat completion response body.
func replyJSON(t *testing.T, w http.ResponseWriter, content string) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"` + jsonEscape(content) + `"}}]}`))
}

func TestEntryCategories(t *testing.T) {
	entry := "## [2.0.0] - 2026-01-15\n\n### Added\n\n- a\n\n### Removed\n\n- b\n\n### Changed\n\n- c\n"
	got := entryCategories(entry)
	want := []string{"Added", "Removed", "Changed"}
	if len(got) != len(want) {
		t.Fatalf("entryCategories = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("entryCategories[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBreakingInEntry(t *testing.T) {
	if !breakingInEntry("### Changed\n\n- **BREAKING** old flag removed\n") {
		t.Error("expected inline BREAKING marker to be detected")
	}
	if !breakingInEntry("### Changed\n\n- BREAKING CHANGE: renamed config key\n") {
		t.Error("expected BREAKING CHANGE: marker to be detected")
	}
	if breakingInEntry("### Added\n\n- new feature\n") {
		t.Error("did not expect a breaking marker in a plain entry")
	}
}

func TestCheckSemverConformance_Table(t *testing.T) {
	cases := []struct {
		name      string
		entry     string
		old, new  string
		wantSev   SemverSeverity
		wantBreak bool
	}{
		{"patch with added", "### Added\n\n- a\n", "1.0.0", "1.0.1", SemverOK, false},
		{"minor with added", "### Added\n\n- a\n", "1.0.0", "1.1.0", SemverOK, false},
		{"major with breaking removed", "### Removed\n\n- b\n", "1.0.0", "2.0.0", SemverOK, true},
		{"over-zealous major no breaking", "### Added\n\n- a\n", "1.0.0", "2.0.0", SemverWarning, false},
		{"under-zealous minor with removed", "### Removed\n\n- b\n", "1.0.0", "1.1.0", SemverError, true},
		{"under-zealous patch with breaking marker", "### Changed\n\n- **BREAKING** c\n", "1.0.0", "1.0.1", SemverError, true},
		{"unreleased is ok", "### Added\n\n- a\n", "1.0.0", "Unreleased", SemverOK, false},
		{"no old version is ok", "### Removed\n\n- b\n", "", "1.1.0", SemverOK, false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// A no-op LLM that is never consulted for a structural hit:
			// when the entry already has a Removed section or a breaking
			// marker, detection returns before calling the LLM, so an
			// empty responder is enough. For the "Added only" cases the
			// LLM is consulted and answers NOT-BREAKING.
			llm := mockLLM(t, func(w http.ResponseWriter, r *http.Request) {
				replyJSON(t, w, "NOT-BREAKING")
			})
			g := &Generator{llm: llm, Git: &GitData{}}
			res, err := g.CheckSemverConformance(context.Background(), c.entry, c.old, c.new)
			if err != nil {
				t.Fatal(err)
			}
			if res.Severity != c.wantSev {
				t.Errorf("Severity = %v, want %v (msg: %s)", res.Severity, c.wantSev, res.Message)
			}
			if res.Breaking != c.wantBreak {
				t.Errorf("Breaking = %v, want %v", res.Breaking, c.wantBreak)
			}
		})
	}
}

func TestCheckSemverConformance_LLMJudgesBreaking(t *testing.T) {
	// No Removed section and no inline marker, but the LLM says the change
	// is breaking (e.g. a behavior change described in prose).
	entry := "### Changed\n\n- the default of --flag now disables the feature\n"
	llm := mockLLM(t, func(w http.ResponseWriter, r *http.Request) {
		replyJSON(t, w, "BREAKING")
	})
	g := &Generator{llm: llm, Git: &GitData{}}
	res, err := g.CheckSemverConformance(context.Background(), entry, "1.0.0", "1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if !res.Breaking {
		t.Error("expected Breaking=true when the LLM judges the change breaking")
	}
	if res.Severity != SemverError {
		t.Errorf("Severity = %v, want SemverError (msg: %s)", res.Severity, res.Message)
	}
}

func TestCheckSemverConformance_NoLLM(t *testing.T) {
	// Without an LLM the structural heuristic must still catch a Removed
	// section, and a minor bump with it must be an error.
	g := &Generator{Git: &GitData{}}
	res, err := g.CheckSemverConformance(context.Background(), "### Removed\n\n- b\n", "1.0.0", "1.1.0")
	if err != nil {
		t.Fatal(err)
	}
	if res.Severity != SemverError || !res.Breaking {
		t.Errorf("got %+v, want SemverError with Breaking", res)
	}
}

func TestExplainSemver(t *testing.T) {
	detail := "The bump is under-zealous because 'Removed' lists a breaking change.\nSee cmd/kaclaig/main.go in the diff stat."
	llm := mockLLM(t, func(w http.ResponseWriter, r *http.Request) {
		replyJSON(t, w, detail)
	})
	g := &Generator{
		llm: llm,
		Git: &GitData{Diff: " cmd/kaclaig/main.go | 5 +++++"},
	}
	res := &SemverResult{Severity: SemverError, Message: "breaking changes require a major bump"}
	if err := g.ExplainSemver(context.Background(), res, "### Removed\n\n- b\n", "1.0.0", "1.1.0"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(res.Detail, "under-zealous") {
		t.Errorf("Detail = %q, want it to contain the explanation", res.Detail)
	}
}

func TestExplainSemver_NoLLM(t *testing.T) {
	g := &Generator{Git: &GitData{}}
	res := &SemverResult{Severity: SemverError, Message: "msg"}
	if err := g.ExplainSemver(context.Background(), res, "x", "1.0.0", "1.1.0"); err != nil {
		t.Fatal(err)
	}
	if res.Detail != "" {
		t.Errorf("expected empty Detail without an LLM, got %q", res.Detail)
	}
}

func TestDetailSuffix(t *testing.T) {
	if detailSuffix(&SemverResult{}) != "" {
		t.Error("expected empty suffix for empty detail")
	}
	s := detailSuffix(&SemverResult{Detail: "line1\nline2"})
	if !strings.Contains(s, "\n    line1\n    line2") {
		t.Errorf("detailSuffix = %q, want indented detail", s)
	}
}

func TestRun_SemverError_Fails(t *testing.T) {
	dir := testRepo(t)
	cl := filepath.Join(dir, "CHANGELOG.md")
	if err := os.WriteFile(cl, []byte("# Changelog\n\n## [1.1.0] - 2025-01-10\n\n- Old release\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// The entry contains a Removed section (breaking) but the requested
	// version is a patch bump -> semver error.
	entry := "## [1.1.1] - 2026-01-15\n\n### Removed\n\n- legacy flag --old\n\n### Fixed\n\n- A bug\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		replyJSON(t, w, entry)
	}))
	t.Cleanup(srv.Close)

	enabled := true
	gen := &Generator{
		Dir:               dir,
		OldTag:            "v1.0.0",
		NewTag:            "1.1.1",
		KeepFile:          cl,
		MaxTries:          2,
		Timeout:           5e9,
		BaseURL:           srv.URL + "/v1",
		APIKey:            "test",
		SemverConformance: &enabled,
	}
	if _, err := gen.Run(context.Background()); err == nil {
		t.Fatal("expected a semver error for a breaking change on a patch bump")
	}
}

func TestRun_SemverWarning_Succeeds(t *testing.T) {
	dir := testRepo(t)
	cl := filepath.Join(dir, "CHANGELOG.md")
	if err := os.WriteFile(cl, []byte("# Changelog\n\n## [1.1.0] - 2025-01-10\n\n- Old release\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Added-only entry but a major bump -> over-zealous warning, not an error.
	entry := "## [2.0.0] - 2026-01-15\n\n### Added\n\n- New feature\n"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		replyJSON(t, w, entry)
	}))
	t.Cleanup(srv.Close)

	enabled := true
	gen := &Generator{
		Dir:               dir,
		OldTag:            "v1.1.0",
		NewTag:            "2.0.0",
		KeepFile:          cl,
		MaxTries:          2,
		Timeout:           5e9,
		BaseURL:           srv.URL + "/v1",
		APIKey:            "test",
		SemverConformance: &enabled,
	}
	out, err := gen.Run(context.Background())
	if err != nil {
		t.Fatalf("expected success for an over-zealous bump (warning only), got: %v", err)
	}
	if strings.TrimSpace(out) != strings.TrimSpace(entry) {
		t.Errorf("Run output = %q, want %q", out, entry)
	}
}

func TestRun_SemverDisabled(t *testing.T) {
	dir := testRepo(t)
	cl := filepath.Join(dir, "CHANGELOG.md")
	if err := os.WriteFile(cl, []byte("# Changelog\n\n## [1.1.0] - 2025-01-10\n\n- Old release\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Breaking entry on a patch bump, but the check is off by default ->
	// success.
	entry := "## [1.1.1] - 2026-01-15\n\n### Removed\n\n- legacy flag --old\n"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		replyJSON(t, w, entry)
	}))
	t.Cleanup(srv.Close)

	gen := &Generator{
		Dir:      dir,
		OldTag:   "v1.0.0",
		NewTag:   "1.1.1",
		KeepFile: cl,
		MaxTries: 2,
		Timeout:  5e9,
		BaseURL:  srv.URL + "/v1",
		APIKey:   "test",
	}
	if _, err := gen.Run(context.Background()); err != nil {
		t.Fatalf("expected success when the semver check is disabled, got: %v", err)
	}
}
