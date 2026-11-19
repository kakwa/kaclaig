package kaclaig

import (
	"context"
	"fmt"
	"regexp"
	"strings"

	"github.com/Masterminds/semver/v3"
)

// SemverSeverity ranks a semver conformance problem.
type SemverSeverity uint8

const (
	// SemverOK means the version bump is consistent with the change.
	SemverOK SemverSeverity = iota
	// SemverWarning means the bump is over-zealous: it is larger than the
	// change warrants (e.g. a major bump with no breaking changes).
	SemverWarning
	// SemverError means the bump is under-zealous: it is smaller than the
	// change requires (e.g. breaking changes shipped as a minor or patch).
	SemverError
)

func (s SemverSeverity) String() string {
	switch s {
	case SemverWarning:
		return "warning"
	case SemverError:
		return "error"
	default:
		return "ok"
	}
}

// SemverResult is the outcome of a semver conformance check.
type SemverResult struct {
	Severity SemverSeverity
	Breaking bool            // the LLM judged the change to contain breaking changes
	Message  string          // human-readable explanation (empty when Severity is OK)
	Detail   string          // optional LLM-written pointer/explanation
	NewVer   *semver.Version // the parsed new version
	OldVer   *semver.Version // the parsed old version
}

// categoryBreaks reports whether the given Keep a Changelog category
// (normalized, e.g. "Removed") indicates a breaking change.
func categoryBreaks(cat string) bool {
	switch strings.ToLower(strings.TrimSpace(cat)) {
	case "removed":
		return true
	default:
		return false
	}
}

// categoryBreaksRe matches an inline breaking-change marker on a bullet
// line: a standalone "BREAKING" word (e.g. "**BREAKING**") or the phrase
// "BREAKING CHANGE" / "BREAKING CHANGES" (with an optional separator).
var categoryBreaksRe = regexp.MustCompile(`\bBREAKING\b(?:[ _-]+CHANGES?)?`)

// breakingInEntry reports whether any bullet in entry carries an explicit
// breaking-change marker such as "**BREAKING**" or "BREAKING CHANGE:".
func breakingInEntry(entry string) bool {
	return categoryBreaksRe.MatchString(entry)
}

// categoryNames are the Keep a Changelog section headers that can appear in
// an entry.
var categoryNames = []string{"Added", "Changed", "Deprecated", "Removed", "Fixed", "Security"}

// entryCategories returns the section header names (e.g. "Added", "Removed")
// present in an entry, in order of first appearance. Only the six canonical
// Keep a Changelog headers count; the version header line (e.g. "## [2.0.0] -
// 2026-01-15") is ignored.
func entryCategories(entry string) []string {
	var out []string
	seen := map[string]bool{}
	for _, line := range strings.Split(entry, "\n") {
		line = strings.TrimRight(line, "\r")
		trimmed := strings.TrimSpace(line)
		if !strings.HasPrefix(trimmed, "###") {
			continue
		}
		name := strings.TrimSpace(strings.TrimLeft(trimmed, "#"))
		if name == "" {
			continue
		}
		key := strings.ToLower(name)
		if seen[key] {
			continue
		}
		// Only canonical Keep a Changelog headers count, so the version
		// header ("## [2.0.0] - 2026-01-15") is skipped even if it were
		// ever emitted at ### level.
		isCat := false
		for _, cn := range categoryNames {
			if key == strings.ToLower(cn) {
				isCat = true
				break
			}
		}
		if !isCat {
			continue
		}
		seen[key] = true
		out = append(out, name)
	}
	return out
}

// detectBreaking asks the LLM whether the changes described in entry
// (supported by gitData) contain breaking changes. It falls back to a
// structural heuristic (Removed sections, inline markers) when the LLM
// cannot be consulted or its answer is unparseable.
func (g *Generator) detectBreaking(ctx context.Context, entry string) bool {
	// A cheap, deterministic signal that never needs the LLM.
	if breakingInEntry(entry) {
		return true
	}
	for _, cat := range entryCategories(entry) {
		if categoryBreaks(cat) {
			return true
		}
	}

	if g.llm == nil {
		return false
	}

	prompt := "You are judging a changelog for semver purposes.\n" +
		"Decide whether the described changes are BREAKING for existing users of the library or CLI.\n" +
		"A change is breaking if it removes, renames, or changes the meaning or default of an API, " +
		"flag, configuration key, or documented behavior in a way that would break existing code or " +
		"scripts. Pure additions, bug fixes, and performance work are NOT breaking.\n\n" +
		"Reply with ONLY the single word: BREAKING or NOT-BREAKING.\n\n" +
		"Changelog entry:\n" +
		entry + "\n"
	if strings.TrimSpace(g.Git.Log) != "" {
		prompt += "\nSupporting git log:\n" + truncate(g.Git.Log, 4000) + "\n"
	}

	reply, err := g.llm.Chat(ctx, []chatMessage{
		{Role: "system", Content: "Answer with exactly one word."},
		{Role: "user", Content: prompt},
	})
	if err != nil {
		return false
	}
	reply = strings.ToUpper(strings.TrimSpace(reply))
	if strings.Contains(reply, "BREAKING") && !strings.Contains(reply, "NOT-BREAKING") && !strings.Contains(reply, "NOT BREAKING") {
		return true
	}
	return false
}

// CheckSemverConformance checks whether the version bump from oldVersion to
// newVersion is consistent with the change described by entry, following the
// semantic versioning rules:
//
//   - A major bump requires at least one breaking change. A major bump with
//     none is over-zealous (SemverWarning).
//   - A minor or patch bump must NOT contain breaking changes. One is
//     under-zealous (SemverError).
//
// When newVersion is "Unreleased", no bump is being made, so the result is
// always SemverOK. When oldVersion is empty or unparseable, there is nothing
// to compare against and the result is SemverOK. When the LLM cannot be
// consulted, breaking changes are detected structurally (Removed sections and
// inline markers).
func (g *Generator) CheckSemverConformance(ctx context.Context, entry, oldVersion, newVersion string) (*SemverResult, error) {
	res := &SemverResult{}
	if newVersion == "" || newVersion == "Unreleased" {
		res.Severity = SemverOK
		return res, nil
	}

	newV, err := semver.NewVersion(strings.TrimPrefix(strings.TrimPrefix(newVersion, "v"), "V"))
	if err != nil {
		// Not a parseable semver: nothing to enforce.
		res.Severity = SemverOK
		return res, nil
	}
	res.NewVer = newV

	if oldVersion == "" {
		res.Severity = SemverOK
		return res, nil
	}
	oldV, err := semver.NewVersion(strings.TrimPrefix(strings.TrimPrefix(oldVersion, "v"), "V"))
	if err != nil {
		res.Severity = SemverOK
		return res, nil
	}
	res.OldVer = oldV

	breaking := g.detectBreaking(ctx, entry)
	res.Breaking = breaking

	// Sanity: the new version must be higher than the old one.
	if !newV.GreaterThan(oldV) {
		res.Severity = SemverWarning
		res.Message = fmt.Sprintf("the new version %s is not greater than the previous version %s", newVersion, oldVersion)
		return res, nil
	}

	switch {
	case breaking && newV.Major() > oldV.Major():
		// Breaking changes correctly bumped to a new major version.
		res.Severity = SemverOK
	case breaking:
		// Breaking changes but the major version was not bumped: under-zealous.
		res.Severity = SemverError
		res.Message = fmt.Sprintf("breaking changes are present but the version was only bumped from %s to %s (minor/patch); a major version bump is required per semver", oldVersion, newVersion)
	case newV.Major() > oldV.Major():
		// No breaking changes but a major bump was made: over-zealous.
		res.Severity = SemverWarning
		res.Message = fmt.Sprintf("no breaking changes were detected, but the major version was bumped from %s to %s; consider a minor or patch bump instead", oldVersion, newVersion)
	default:
		// No breaking changes and no major bump: correct.
		res.Severity = SemverOK
	}
	return res, nil
}

// ExplainSemver asks the LLM for a short, human-readable explanation of why
// the bump from oldVersion to newVersion is inconsistent with the change,
// pointing at the specific bullet(s) in the entry that cause it and, when the
// diff stat identifies a file, at that file. The result is stored in
// res.Detail. When no LLM is available or the call fails, res.Detail is
// left empty and the caller falls back to res.Message alone.
func (g *Generator) ExplainSemver(ctx context.Context, res *SemverResult, entry, oldVersion, newVersion string) error {
	if g.llm == nil || res.Severity == SemverOK {
		return nil
	}

	kind := "over-zealous (a larger bump than the change warrants)"
	if res.Severity == SemverError {
		kind = "under-zealous (a smaller bump than the change requires)"
	}

	var b strings.Builder
	fmt.Fprintf(&b, "A changelog entry was generated for a release bump from %s to %s.\n", oldVersion, newVersion)
	fmt.Fprintf(&b, "A semver conformance check judged the bump to be %s.\n", kind)
	fmt.Fprintf(&b, "Reason: %s\n\n", res.Message)
	b.WriteString("Changelog entry:\n" + entry + "\n")
	if strings.TrimSpace(g.Git.Diff) != "" {
		b.WriteString("\nDiff stat (files changed):\n" + truncate(g.Git.Diff, 3000) + "\n")
	}
	b.WriteString("\nReply with 1-3 short lines:\n")
	b.WriteString("- a one-sentence explanation of WHY the bump is inconsistent with the change;\n")
	b.WriteString("- the specific bullet point(s) in the entry that cause it (quote them);\n")
	b.WriteString("- if the diff stat names a file, mention which file(s) are implicated (e.g. \"see cmd/.../main.go\").\n")
	b.WriteString("Do not use code fences. Be concise and point at the concrete evidence.")

	reply, err := g.llm.Chat(ctx, []chatMessage{
		{Role: "system", Content: "You are a helpful code reviewer. Be concise and specific. Never invent file names that are not in the provided diff."},
		{Role: "user", Content: b.String()},
	})
	if err != nil {
		return err
	}
	res.Detail = strings.TrimSpace(reply)
	return nil
}
