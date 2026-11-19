package kaclaig

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Masterminds/semver/v3"
)

// Version bump modes, used when NewTag is not given explicitly.
const (
	BumpUnreleased = "unreleased" // default: write into the [Unreleased] section
	BumpPatch      = "patch"      // -p: bump the patch segment
	BumpMinor      = "minor"      // -m: bump the minor segment, reset patch
	BumpMajor      = "major"      // -M: bump the major segment, reset the rest
)

// Generator assembles git history and the existing changelog and asks an LLM
// to produce a new changelog entry, validating the result before returning it.
type Generator struct {
	Dir      string
	NewTag   string
	OldTag   string
	Bump     string // version mode when NewTag is empty; BumpUnreleased by default
	Model    string
	MaxTries int
	KeepFile string // path to the changelog file; "" to auto-detect
	NoWrite  bool
	Verbose  bool
	// WarnSemver enables the semver conformance check (on by default).
	// Under-zealous bumps are errors; over-zealous bumps are warnings.
	WarnSemver *bool
	Timeout    time.Duration
	BaseURL    string // optional LLM base URL override
	APIKey     string // optional LLM API key override
	Config     string // optional explicit path to an LLM config file
	Existing   string // existing changelog content
	Git        *GitData
	llm        *LLM

	prevTag string // latest tag at collect time (the base of a derived bump)
}

// Run executes the full generation workflow. When g.NoWrite is false and a
// changelog file exists, the new entry is prepended into it.
func (g *Generator) Run(ctx context.Context) (string, error) {
	if g.Dir == "" {
		g.Dir = "."
	}
	if g.MaxTries <= 0 {
		g.MaxTries = 3
	}
	if g.WarnSemver == nil {
		b := true
		g.WarnSemver = &b
	}
	if g.Bump == "" {
		g.Bump = BumpUnreleased
	}
	switch g.Bump {
	case BumpUnreleased, BumpPatch, BumpMinor, BumpMajor:
	default:
		return "", fmt.Errorf("invalid bump mode %q (want %q, %q, %q or %q)", g.Bump, BumpUnreleased, BumpPatch, BumpMinor, BumpMajor)
	}

	if err := g.collect(ctx); err != nil {
		return "", err
	}

	cfg := LLMConfig{
		Model:   g.Model,
		Timeout: g.Timeout,
		BaseURL: g.BaseURL,
		APIKey:  g.APIKey,
	}
	if cfg.BaseURL == "" || cfg.APIKey == "" || cfg.Model == "" {
		fileSettings, err := LoadLLMSettings(g.Config)
		if err != nil {
			return "", err
		}
		if cfg.BaseURL == "" {
			cfg.BaseURL = fileSettings.BaseURL
		}
		if cfg.APIKey == "" {
			cfg.APIKey = fileSettings.APIKey
		}
		if cfg.Model == "" {
			cfg.Model = fileSettings.Model
		}
		if g.Verbose && (cfg.BaseURL != "" || cfg.Model != "") {
			g.log("using LLM settings from config file")
		}
	}
	llm, err := NewLLM(cfg)
	if err != nil {
		return "", err
	}
	g.llm = llm
	if g.Verbose {
		g.log("using model %s", llm.model)
	}

	system := g.systemPrompt()
	user := g.userPrompt()
	messages := []chatMessage{
		{Role: "system", Content: system},
		{Role: "user", Content: user},
	}

	var lastErr error
	for attempt := 1; attempt <= g.MaxTries; attempt++ {
		if g.Verbose {
			g.log("attempt %d/%d", attempt, g.MaxTries)
		}
		reply, err := llm.Chat(ctx, messages)
		if err != nil {
			return "", fmt.Errorf("attempt %d: %w", attempt, err)
		}
		entry := extractEntry(reply, g.NewTag)

		if ok, reason := ValidateKeepAChangelog(entry, g.NewTag); ok {
			if *g.WarnSemver {
				if res, err := g.checkSemver(ctx, entry); err != nil {
					g.log("semver check: %v", err)
				} else if res.Severity != SemverOK {
					g.explainSemver(ctx, res, entry)
					switch res.Severity {
					case SemverError:
						return "", fmt.Errorf("semver: %s%s", res.Message, detailSuffix(res))
					case SemverWarning:
						fmt.Fprintf(os.Stderr, "warning: semver: %s%s\n", res.Message, detailSuffix(res))
					}
				}
			}
			if !g.NoWrite && g.KeepFile != "" {
				if err := g.writeEntry(entry); err != nil {
					return "", err
				}
				if g.Verbose {
					g.log("updated %s", g.KeepFile)
				}
			}
			return entry, nil
		} else if lastErr = errors.New(reason); lastErr != nil {
			messages = append(messages,
				chatMessage{Role: "assistant", Content: entry},
				chatMessage{Role: "user", Content: g.correctionPrompt(reason)},
			)
			if g.Verbose {
				g.log("attempt %d failed: %s", attempt, reason)
			}
		}
	}
	return "", fmt.Errorf("LLM produced no valid changelog entry after %d attempts: %w", g.MaxTries, lastErr)
}

// collect gathers the existing changelog and git data.
func (g *Generator) collect(ctx context.Context) error {
	// Existing changelog.
	if g.KeepFile == "" {
		path, err := FindChangelog(g.Dir)
		if err != nil {
			return err
		}
		g.KeepFile = path
	}
	if g.KeepFile != "" {
		if data, err := os.ReadFile(g.KeepFile); err == nil {
			g.Existing = string(data)
		} else if !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}

	// Tags.
	latest, _ := LatestTag(ctx, g.Dir)
	if g.OldTag == "" {
		if latest != "" {
			prev, err := PreviousTag(ctx, g.Dir, latest)
			if err != nil {
				g.OldTag = ""
			} else {
				g.OldTag = prev
			}
		}
	}
	// Remember the latest tag before it may be shadowed, so the semver
	// check can compare against the version actually being released from.
	g.prevTag = latest
	if g.NewTag == "" {
		if g.Bump == BumpUnreleased {
			// No release: write into the existing [Unreleased] section.
			g.NewTag = "Unreleased"
		} else {
			g.NewTag = bumpVersion(latest, g.Bump)
		}
	}

	gitData, err := CollectGitData(ctx, g.Dir, g.NewTag, g.OldTag)
	if err != nil {
		return err
	}
	g.Git = gitData
	if g.Git.Count == 0 && strings.TrimSpace(g.Git.Log) == "" {
		return errors.New("no commits found in the range; nothing to generate")
	}
	return nil
}

// systemPrompt returns the system instructions for the LLM. The rules follow
// the Keep a Changelog 1.1.0 specification (https://keepachangelog.com).
func (g *Generator) systemPrompt() string {
	if g.NewTag == "Unreleased" {
		return unreleasedSystemPrompt
	}
	return releaseSystemPrompt
}

const unreleasedSystemPrompt = `You are a changelog writer. Given git history and the project's existing changelog, write the updated [Unreleased] section in Keep a Changelog 1.1.0 format (https://keepachangelog.com/en/1.1.0/).

Output requirements (strict):
- Output ONLY the [Unreleased] section, no surrounding text, no code fences, no explanations.
- The section must start with exactly one header line: ## [Unreleased]
- Below the header, use ### section headers with the standard Keep a Changelog
  categories: Added, Changed, Deprecated, Removed, Fixed, Security.
  Only include sections that apply, in that order.
- Under each section, list bullet points starting with "- ".
- Write concise, user-facing descriptions (one per bullet). Do not include
  commit hashes or raw commit subjects verbatim; summarize them in the
  imperative mood (e.g. "add support for X", "fix crash when Y").
- If the existing changelog already has an [Unreleased] section with entries,
  keep those entries and add the new ones from the commit log, merging
  duplicates and keeping the existing wording where it still applies.
- Do not invent changes that are not supported by the commit log or diff.
- Never output any other version header, and never repeat the existing changelog.`

const releaseSystemPrompt = `You are a changelog writer. Given git history and the project's existing changelog, write one new release entry in Keep a Changelog 1.1.0 format (https://keepachangelog.com/en/1.1.0/).

Output requirements (strict):
- Output ONLY the new entry, no surrounding text, no code fences, no explanations.
- The entry must start with exactly one header line: ## [VERSION] - YYYY-MM-DD
  where VERSION is the version given in the request and the date is the given release date.
- Below the header, use ### section headers with the standard Keep a Changelog
  categories: Added, Changed, Deprecated, Removed, Fixed, Security.
  Only include sections that apply, in that order.
- Under each section, list bullet points starting with "- ".
- Write concise, user-facing descriptions (one per bullet). Do not include
  commit hashes or raw commit subjects verbatim; summarize them in the
  imperative mood (e.g. "add support for X", "fix crash when Y").
- Do not invent features that are not supported by the commit log or diff.
- If the existing changelog has an [Unreleased] section with entries, those
  entries describe work since the last release and must be merged into the
  new entry (they are not a separate section of the new entry).
- Never output any other version header, and never repeat the existing changelog.`

// userPrompt assembles the per-request data for the LLM.
func (g *Generator) userPrompt() string {
	var b strings.Builder
	fmt.Fprintf(&b, "New version: %s\n", g.NewTag)
	fmt.Fprintf(&b, "Release date: %s\n", time.Now().Format("2006-01-02"))
	if g.Git.OldTag != "" {
		fmt.Fprintf(&b, "Previous tag: %s\n", g.Git.OldTag)
	}
	fmt.Fprintf(&b, "Commits in range (%d):\n%s\n", g.Git.Count, g.Git.Log)
	if strings.TrimSpace(g.Git.Diff) != "" {
		fmt.Fprintf(&b, "\nDiff stat (old tag to new):\n%s\n", g.Git.Diff)
	}
	if strings.TrimSpace(g.Existing) != "" {
		b.WriteString("\nExisting changelog (for style reference):\n")
		b.WriteString(truncate(strings.TrimSpace(g.Existing), 12000))
	}
	b.WriteString("\nWrite the new changelog entry now.")
	return b.String()
}

// correctionPrompt builds the follow-up message sent when an entry is invalid.
func (g *Generator) correctionPrompt(reason string) string {
	header := "## [VERSION] - YYYY-MM-DD"
	if g.NewTag == "Unreleased" {
		header = "## [Unreleased]"
	}
	return "Your previous output was invalid: " + reason + "\n\n" +
		"Rewrite it. Remember:\n" +
		"- Output ONLY the entry (no code fences, no commentary).\n" +
		"- Header line exactly: " + header + ".\n" +
		"- Use ### section headers (Added, Changed, Deprecated, Removed, Fixed, Security), only those that apply.\n" +
		"- Bullet points start with \"- \" and are concise, user-facing, in the imperative mood.\n"
}

// extractEntry pulls the entry out of the LLM reply, stripping code fences and
// keeping only the lines from the version header onward.
func extractEntry(reply, version string) string {
	reply = strings.TrimSpace(reply)
	reply = stripCodeFences(reply)

	lines := strings.Split(reply, "\n")
	start := -1
	for i, line := range lines {
		if isVersionHeader(line, version) || (version == "Unreleased" && isUnreleasedHeader(line)) {
			start = i
			break
		}
	}
	if start < 0 {
		// No matching header; keep the whole reply so validation reports it.
		return strings.TrimSpace(reply)
	}
	return strings.TrimSpace(strings.Join(lines[start:], "\n"))
}

func stripCodeFences(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if idx := strings.Index(s, "\n"); idx >= 0 {
			s = s[idx+1:]
		}
	}
	if strings.HasSuffix(s, "```") {
		s = strings.TrimSuffix(s, "```")
		s = strings.TrimSpace(s)
	}
	return s
}

var headerVersionRe = regexp.MustCompile(`^\s*#+\s*\[?([^\]\s]+)\]?(?:\s*-\s*\d{4}-\d{2}-\d{2})?\s*$`)

func isVersionHeader(line, version string) bool {
	m := headerVersionRe.FindStringSubmatch(line)
	if m == nil {
		return false
	}
	v := strings.TrimPrefix(strings.TrimPrefix(m[1], "v"), "V")
	want := strings.TrimPrefix(strings.TrimPrefix(version, "v"), "V")
	return v == want
}

// isUnreleasedHeader reports whether line is a "## [Unreleased]" header.
func isUnreleasedHeader(line string) bool {
	name := strings.TrimSuffix(strings.TrimPrefix(strings.TrimSpace(line), "## ["), "]")
	return strings.TrimSpace(name) == "Unreleased"
}

// ValidateKeepAChangelog checks that entry is a valid Keep a Changelog
// document containing the given version. It returns ok and a human-readable
// reason describing any failure (empty when ok is true).
//
// When version is "Unreleased", the entry must instead be an [Unreleased]
// section: it starts with exactly one '## [Unreleased]' header (no date) and
// contains at least one bullet point.
func ValidateKeepAChangelog(entry, version string) (bool, string) {
	entry = strings.TrimSpace(entry)
	if entry == "" {
		return false, "the entry is empty"
	}
	if version == "Unreleased" {
		return validateUnreleased(entry)
	}
	if !strings.HasPrefix(entry, "## [") {
		return false, "the entry must start with a '## [version] - date' header line"
	}
	if !keepAChangelogHeaderRe.MatchString(entry) {
		return false, "no version header could be parsed; expected '## [version] - YYYY-MM-DD'"
	}
	if n := len(keepAChangelogHeaderRe.FindAllString(entry, -1)); n > 1 {
		return false, "the entry contains multiple version headers; it must contain exactly one"
	}
	v := keepAChangelogVersion(entry)
	if v == "" {
		return false, "no version header could be parsed; expected '## [version] - YYYY-MM-DD'"
	}
	v = strings.TrimPrefix(strings.TrimPrefix(v, "v"), "V")
	want := strings.TrimPrefix(strings.TrimPrefix(version, "v"), "V")
	if v != want {
		return false, fmt.Sprintf("the header version %q does not match the requested version %q", v, version)
	}
	if keepAChangelogDate(entry) == "" {
		return false, "the header must include a date in YYYY-MM-DD format, e.g. ## [" + version + "] - 2026-01-15"
	}
	if !bulletRe.MatchString(entry) {
		return false, "the entry contains no bullet points; add at least one under a section header"
	}
	return true, ""
}

var (
	unreleasedHeaderRe     = regexp.MustCompile(`(?m)^\s*##\s*\[\s*Unreleased\s*\]\s*$`)
	versionHeaderRe        = regexp.MustCompile(`(?m)^\s*##\s*\[([^\]\s]+)\]`)
	keepAChangelogHeaderRe = regexp.MustCompile(`(?m)^\s*##\s+\[([^\]]+)\](?:\s+-\s+(\d{4}-\d{2}-\d{2}))?`)
	bulletRe               = regexp.MustCompile(`(?m)^\s*[-*+]\s+\S`)
)

// keepAChangelogVersion returns the version of the (first) version header in
// entry, or "" when there is none.
func keepAChangelogVersion(entry string) string {
	m := keepAChangelogHeaderRe.FindStringSubmatch(entry)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[1])
}

// keepAChangelogDate returns the date of the (first) version header in entry,
// or "" when there is none.
func keepAChangelogDate(entry string) string {
	m := keepAChangelogHeaderRe.FindStringSubmatch(entry)
	if m == nil {
		return ""
	}
	return strings.TrimSpace(m[2])
}

// validateUnreleased checks that entry is a valid [Unreleased] section: it
// starts with exactly one '## [Unreleased]' header and contains at least one
// bullet point.
func validateUnreleased(entry string) (bool, string) {
	if !unreleasedHeaderRe.MatchString(entry) {
		return false, "the section must start with a '## [Unreleased]' header line"
	}
	if n := len(unreleasedHeaderRe.FindAllString(entry, -1)); n > 1 {
		return false, fmt.Sprintf("the section contains %d '## [Unreleased]' headers; it must contain exactly one", n)
	}
	for _, m := range versionHeaderRe.FindAllStringSubmatch(entry, -1) {
		v := strings.TrimPrefix(strings.TrimPrefix(m[1], "v"), "V")
		if v != "Unreleased" {
			return false, fmt.Sprintf("the section must not contain other version headers; found %q", m[1])
		}
	}
	if !bulletRe.MatchString(entry) {
		return false, "the section contains no bullet points; add at least one under a section header"
	}
	return true, ""
}

// writeEntry prepends the new entry into the existing changelog file.
func (g *Generator) writeEntry(entry string) error {
	if g.KeepFile == "" {
		return nil
	}

	existing := g.Existing
	var out string

	if g.NewTag == "Unreleased" {
		// The entry IS the [Unreleased] section: replace any existing one,
		// or insert it after the top-level heading / at the top.
		if start, end := unreleasedSection(existing); start >= 0 {
			out = existing[:start] + strings.TrimRight(entry, "\n") + "\n\n" + existing[end:]
		} else {
			idx := indexAfterFirstHeaderLine(existing)
			if idx < 0 {
				out = entry + "\n\n" + existing
			} else {
				out = existing[:idx] + "\n" + entry + "\n" + existing[idx:]
			}
		}
		return os.WriteFile(g.KeepFile, []byte(out), 0o644)
	}

	header := fmt.Sprintf("## [%s]\n", g.NewTag)

	// Replace the Unreleased section with the new entry, or insert the new
	// entry right after the top-level header/intro. The LLM was instructed
	// to fold any Unreleased content into the new entry, so the stale
	// section (header and content) is dropped.
	if start, end := unreleasedSection(existing); start >= 0 {
		out = existing[:start] + strings.TrimRight(entry, "\n") + "\n\n" + existing[end:]
	} else {
		// Insert after the first top-level heading line, or at the top.
		idx := indexAfterFirstHeaderLine(existing)
		if idx < 0 {
			out = entry + "\n\n" + existing
		} else {
			out = existing[:idx] + "\n" + entry + "\n" + existing[idx:]
		}
	}

	// Ensure the version header appears exactly once in the file.
	if countOccurrences(out, header) > 1 {
		// Deduplicate: drop the older occurrence (keep the first).
		first := strings.Index(out, header)
		second := strings.Index(out[first+len(header):], header)
		if first >= 0 && second >= 0 {
			secondAbs := first + len(header) + second
			// Remove the second header and its content up to the next header.
			next := strings.Index(out[secondAbs:], "\n## ")
			end := secondAbs
			if next >= 0 {
				end = secondAbs + next
			}
			out = out[:secondAbs] + out[end:]
		}
	}

	return os.WriteFile(g.KeepFile, []byte(out), 0o644)
}

// unreleasedSection returns the byte offsets [start, end) of the
// "## [Unreleased]" section, including its header and trailing blank line,
// or (-1, -1) when not present.
func unreleasedSection(s string) (int, int) {
	lines := strings.Split(s, "\n")
	startIdx := -1
	for i, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "## [") {
			name := strings.TrimSuffix(strings.TrimPrefix(trimmed, "## ["), "]")
			if strings.TrimSpace(name) == "Unreleased" {
				startIdx = i
				break
			}
		}
	}
	if startIdx < 0 {
		return -1, -1
	}
	start := offsetOfLine(lines, startIdx)
	for i := startIdx + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			end := offsetOfLine(lines, i)
			// Include the preceding blank line so the result stays clean.
			if end > start && s[end-1] == '\n' && end > 0 && s[end-2] == '\n' {
				end--
			}
			return start, end
		}
	}
	return start, len(s)
}

func offsetOfLine(lines []string, i int) int {
	offset := 0
	for j := 0; j < i; j++ {
		offset += len(lines[j]) + 1
	}
	return offset
}

func indexAfterFirstHeaderLine(s string) int {
	for i, line := range strings.Split(s, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") && !strings.HasPrefix(trimmed, "## [") {
			return i + len(line) // offset of end of this line
		}
	}
	return -1
}

func countOccurrences(s, sub string) int {
	return strings.Count(s, sub)
}

// FindChangelog locates a changelog file in the given directory.
// Returns the path to the changelog file, or empty string if not found.
func FindChangelog(directory string) (string, error) {
	dirEntries, err := os.ReadDir(directory)
	if err != nil {
		return "", err
	}

	var files []string
	for _, e := range dirEntries {
		if !e.IsDir() {
			files = append(files, e.Name())
		}
	}

	for _, name := range changelogFilenames {
		var candidates []string
		for _, f := range files {
			if strings.HasSuffix(strings.ToLower(f), ".sh") {
				continue
			}
			lower := strings.ToLower(f)
			base := lower
			ext := filepath.Ext(lower)
			if ext != "" {
				base = lower[:len(lower)-len(ext)]
			}
			if base != name {
				continue
			}
			if slices.Contains(changelogExtensions, ext) {
				candidates = append(candidates, f)
			}
		}

		if len(candidates) == 1 {
			return filepath.Join(directory, candidates[0]), nil
		}

		for _, candidate := range candidates {
			path := filepath.Join(directory, candidate)
			info, err := os.Stat(path)
			if err != nil {
				continue
			}
			size := info.Size()
			if size > 1_000_000 || size < 100 {
				continue
			}
			return path, nil
		}
	}

	return "", nil
}

// Common changelog filenames in priority order.
var changelogFilenames = []string{
	"changelog",
	"news",
	"changes",
	"history",
	"release",
	"whatsnew",
	"releases",
}

// Allowed changelog file extensions.
var changelogExtensions = []string{".md", ".txt", ".rst", ".rdoc", ".markdown", ""}

// parseVersion parses a tag like "v1.2.3-rc.1" into a semver version,
// tolerating a leading "v" or "V". It returns nil when the tag is not a
// parseable semantic version.
func parseVersion(tag string) *semver.Version {
	s := strings.TrimPrefix(strings.TrimPrefix(tag, "v"), "V")
	v, err := semver.NewVersion(s)
	if err != nil {
		return nil
	}
	return v
}

// bumpSegment returns the version with the given segment bumped and the
// lower segments reset, preserving any prerelease label. The returned
// version is in plain dotted form (no leading "v").
func bumpSegment(v *semver.Version, mode string) semver.Version {
	var major, minor, patch uint64
	switch mode {
	case BumpMinor:
		major, minor, patch = v.Major(), v.Minor()+1, 0
	case BumpMajor:
		major, minor, patch = v.Major()+1, 0, 0
	default:
		major, minor, patch = v.Major(), v.Minor(), v.Patch()+1
	}
	next := semver.New(major, minor, patch, v.Prerelease(), v.Metadata())
	return *next
}

// bumpVersion returns the new version derived from the latest tag for the
// given bump mode, or "0.1.0" when there is no latest tag. For two-segment
// versions (e.g. "1.2") the patch bump is interpreted as bumping the minor
// segment, matching how such tags are commonly used.
func bumpVersion(latest, mode string) string {
	if latest == "" {
		return "0.1.0"
	}
	v := parseVersion(latest)
	if v == nil {
		return nextVersion(latest)
	}
	if mode == BumpPatch && !hasThirdSegment(latest) {
		// "1.2" style: there is no patch segment, so bump the last one,
		// giving 1.2 -> 1.3.0.
		return bumpSegment(v, BumpMinor).String()
	}
	return bumpSegment(v, mode).String()
}

// hasThirdSegment reports whether the tag has an explicit third (patch)
// segment, i.e. is a full major.minor.patch version.
func hasThirdSegment(tag string) bool {
	s := strings.TrimPrefix(strings.TrimPrefix(tag, "v"), "V")
	if i := strings.IndexByte(s, '-'); i >= 0 {
		s = s[:i]
	}
	return len(strings.Split(s, ".")) == 3
}

// bumpLastSegment bumps the final numeric segment of a two-segment tag in
// place, preserving the prefix and any prerelease suffix, e.g. "v1.2" ->
// "v1.3" or "1.2-rc.1" -> "1.3-rc.1".
func bumpLastSegment(tag string) string {
	prefix := ""
	if tag[0] == 'v' || tag[0] == 'V' {
		prefix = tag[:1]
		tag = tag[1:]
	}
	num, suffix := tag, ""
	if i := strings.IndexByte(tag, '-'); i >= 0 {
		num, suffix = tag[:i], tag[i:]
	}
	segments := strings.Split(num, ".")
	if len(segments) == 0 {
		return tag + "-next"
	}
	if n, err := strconv.Atoi(segments[len(segments)-1]); err == nil {
		segments[len(segments)-1] = strconv.Itoa(n + 1)
		return prefix + strings.Join(segments, ".") + suffix
	}
	return tag + "-next"
}

// nextVersion derives a candidate new tag from the latest tag by bumping the
// patch component, or returns "0.1.0" when there is no latest tag. Non-semver
// tags fall back to appending "-next".
func nextVersion(latest string) string {
	if latest == "" {
		return "0.1.0"
	}
	if v := parseVersion(latest); v != nil {
		if !hasThirdSegment(latest) {
			// "1.2" style: bump the last segment in place, giving 1.2 -> 1.3.
			// Reconstruct from the original tag so the tag form is preserved.
			return bumpLastSegment(latest)
		}
		return bumpSegment(v, BumpPatch).String()
	}
	return latest + "-next"
}

// checkSemver runs the semver conformance check for the entry about to be
// written. The base version is the explicit -from tag when given, otherwise
// the latest tag that a derived bump was made from.
func (g *Generator) checkSemver(ctx context.Context, entry string) (*SemverResult, error) {
	old := g.OldTag
	if old == "" {
		old = g.prevTag
	}
	return g.CheckSemverConformance(ctx, entry, old, g.NewTag)
}

// explainSemver asks the LLM for a concise pointer/explanation for a
// non-OK semver result and stores it in res.Detail. Failures are silent so a
// secondary LLM call never blocks the main workflow.
func (g *Generator) explainSemver(ctx context.Context, res *SemverResult, entry string) {
	if res.Severity == SemverOK || g.llm == nil {
		return
	}
	old := g.OldTag
	if old == "" {
		old = g.prevTag
	}
	if err := g.ExplainSemver(ctx, res, entry, old, g.NewTag); err != nil && g.Verbose {
		g.log("semver explanation unavailable: %v", err)
	}
}

// detailSuffix formats res.Detail as an indented block appended to a
// message, or "" when there is no detail.
func detailSuffix(res *SemverResult) string {
	if res.Detail == "" {
		return ""
	}
	return "\n" + indentLines(res.Detail, "    ")
}

// indentLines prefixes every line of s with the given indent.
func indentLines(s, indent string) string {
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = indent + l
	}
	return strings.Join(lines, "\n")
}

func (g *Generator) log(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "[kaclaig] "+format+"\n", args...)
}
