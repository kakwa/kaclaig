package kaclaig

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strings"
	"time"
)

const maxLogSize = 400 << 10 // 400 KiB
const maxDiffSize = 1 << 20  // 1 MiB

// GitData holds the git context used to generate a changelog entry.
type GitData struct {
	NewTag  string // tag being generated (or empty when deriving from HEAD)
	OldTag  string // previous tag, the diff base ("" if none)
	Subject string // subject of the HEAD commit, or ""
	Count   int    // number of commits in the range
	Log     string // git log output for the range
	Diff    string // git diff --stat output for the range (may be truncated)
}

// RunGit runs git with the given arguments in dir and returns stdout.
func RunGit(ctx context.Context, dir string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	// Keep git non-interactive: never open an editor, prompt, or pager.
	cmd.Env = append(os.Environ(),
		"GIT_EDITOR=true",
		"GIT_TERMINAL_PROMPT=0",
		"GIT_PAGER=cat",
	)
	var out, errb strings.Builder
	cmd.Stdout = &out
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errb.String())
		if msg != "" {
			return "", fmt.Errorf("git %s: %s: %w", strings.Join(args, " "), msg, err)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return out.String(), nil
}

// LatestTag returns the most recent tag reachable from HEAD, or an empty
// string if there are no tags.
func LatestTag(ctx context.Context, dir string) (string, error) {
	out, err := RunGit(ctx, dir, "describe", "--tags", "--abbrev=0")
	if err != nil {
		return "", nil
	}
	return strings.TrimSpace(out), nil
}

// PreviousTag returns the tag that immediately precedes before in the
// release history: the latest tag reachable from the commit just before the
// one before points at. Using the commit ancestry (rather than dates) keeps
// the result correct even when two tags share the same date. When before is
// empty, it returns the latest tag in the repository.
func PreviousTag(ctx context.Context, dir, before string) (string, error) {
	out, err := RunGit(ctx, dir, "tag", "-l")
	if err != nil {
		return "", err
	}
	var tags []string
	for _, line := range strings.Split(out, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			tags = append(tags, line)
		}
	}
	if len(tags) == 0 {
		return "", errors.New("no tags found in repository")
	}

	if before != "" {
		// The parent is the newest tag reachable from the commit that
		// precedes the one before points at. --tags includes lightweight
		// tags; a missing parent (e.g. the very first release) yields an
		// empty result.
		if parent, err := RunGit(ctx, dir, "describe", "--tags", "--abbrev=0", before+"~1"); err == nil {
			return strings.TrimSpace(parent), nil
		}
		return "", nil
	}

	// No explicit "before": return the newest tag by author date, with the
	// tag name as a deterministic tiebreaker for tags on the same date.
	type tagged struct {
		tag  string
		when time.Time
		ok   bool
	}
	list := make([]tagged, 0, len(tags))
	for _, tag := range tags {
		t, err := tagTime(ctx, dir, tag)
		list = append(list, tagged{tag: tag, when: t, ok: err == nil})
	}
	sort.Slice(list, func(i, j int) bool {
		if !list[i].ok && !list[j].ok {
			return list[i].tag > list[j].tag
		}
		if !list[i].ok {
			return false
		}
		if !list[j].ok {
			return true
		}
		if list[i].when.Equal(list[j].when) {
			return list[i].tag > list[j].tag
		}
		return list[i].when.After(list[j].when)
	})
	return list[0].tag, nil
}

// tagTime returns the author date of the commit a tag points at. The author
// date is used (instead of the committer date) so amended commits keep a
// stable, comparable date.
func tagTime(ctx context.Context, dir, tag string) (time.Time, error) {
	out, err := RunGit(ctx, dir, "log", "-1", "--format=%aI", tag)
	if err != nil {
		return time.Time{}, err
	}
	return time.Parse(time.RFC3339, strings.TrimSpace(out))
}

// tagTarget returns the commit-ish a version resolves to, or "" when no tag
// matches. Tags are commonly written with a leading "v" while the version
// string is not (or vice versa), so each spelling is tried. The peeled commit
// (what the tag ultimately points at) is preferred so annotated tags resolve
// to the same commit as the raw tag reference.
func tagTarget(ctx context.Context, dir, version string) string {
	seen := map[string]bool{}
	for _, candidate := range tagCandidates(version) {
		if seen[candidate] {
			continue
		}
		seen[candidate] = true
		if out, err := RunGit(ctx, dir, "rev-parse", candidate+"^{commit}"); err == nil {
			return strings.TrimSpace(out)
		}
	}
	return ""
}

// tagCandidates returns the tag spellings to try for a version, in order of
// preference: the version itself, then with a "v" or "V" prefix.
func tagCandidates(version string) []string {
	v := strings.TrimSpace(version)
	if v == "" {
		return nil
	}
	candidates := []string{v}
	if v[0] != 'v' && v[0] != 'V' {
		candidates = append(candidates, "v"+v, "V"+v)
	}
	return candidates
}

// HeadCommit returns the subject line of the HEAD commit.
func HeadCommit(ctx context.Context, dir string) (string, error) {
	out, err := RunGit(ctx, dir, "log", "-1", "--format=%s")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// CollectGitData gathers the git log and diff between oldTag and the range
// end (the whole history when oldTag is empty). newTag is the version being
// generated and is recorded in the result; when it already exists as a tag it
// is used as the range end, so regenerating an existing release only covers
// the commits up to that release rather than everything up to HEAD.
func CollectGitData(ctx context.Context, dir, newTag, oldTag string) (*GitData, error) {
	if _, err := RunGit(ctx, dir, "rev-parse", "--git-dir"); err != nil {
		return nil, errors.New("not a git repository (or not inside one)")
	}

	data := &GitData{NewTag: newTag, OldTag: oldTag}

	// The range normally ends at HEAD because the new tag doesn't exist yet.
	// When the tag already exists (a regeneration), end the range at the tag
	// so commits made after the release aren't folded into it.
	end := "HEAD"
	data.Subject, _ = HeadCommit(ctx, dir)
	if t := tagTarget(ctx, dir, newTag); t != "" {
		end = t
		subj, err := RunGit(ctx, dir, "log", "-1", "--format=%s", t)
		if err == nil {
			data.Subject = strings.TrimSpace(subj)
		}
	}

	revRange := end
	if oldTag != "" {
		revRange = oldTag + ".." + end
	}

	logOut, logErr := RunGit(ctx, dir, "log", "--no-merges", "--pretty=format:%h %s", revRange)
	if logErr != nil {
		logOut, logErr = RunGit(ctx, dir, "log", "--no-merges", "-1", "--pretty=format:%h %s", end)
	}
	if logErr != nil {
		return nil, logErr
	}
	data.Log = truncate(logOut, maxLogSize)
	if strings.TrimSpace(data.Log) == "" {
		data.Count = 0
	} else {
		data.Count = len(strings.Split(data.Log, "\n"))
	}

	if oldTag != "" {
		if diffOut, err := RunGit(ctx, dir, "diff", "--stat", oldTag, end); err == nil {
			data.Diff = truncate(diffOut, maxDiffSize)
		}
	}
	return data, nil
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max] + "\n... (truncated)"
}
