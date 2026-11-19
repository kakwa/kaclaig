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
// release history: the chronologically latest tag whose commit date is not
// after the date of before, excluding before itself. When before is empty,
// it returns the latest tag in the repository.
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

	type tagged struct {
		tag  string
		when time.Time
		ok   bool
	}
	var list []tagged
	var beforeTime time.Time
	if before != "" {
		beforeTime, _ = tagTime(ctx, dir, before)
	}

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

	for _, e := range list {
		if before != "" && e.tag == before {
			continue
		}
		if before != "" && beforeTime.IsZero() {
			break
		}
		if before != "" && !e.ok {
			continue
		}
		if before != "" && !e.when.Before(beforeTime) {
			continue
		}
		return e.tag, nil
	}
	return "", nil
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

// HeadCommit returns the subject line of the HEAD commit.
func HeadCommit(ctx context.Context, dir string) (string, error) {
	out, err := RunGit(ctx, dir, "log", "-1", "--format=%s")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// CollectGitData gathers the git log and diff between oldTag and HEAD
// (the whole history when oldTag is empty). newTag is the version being
// generated and is only recorded in the result.
func CollectGitData(ctx context.Context, dir, newTag, oldTag string) (*GitData, error) {
	if _, err := RunGit(ctx, dir, "rev-parse", "--git-dir"); err != nil {
		return nil, errors.New("not a git repository (or not inside one)")
	}

	data := &GitData{NewTag: newTag, OldTag: oldTag}
	data.Subject, _ = HeadCommit(ctx, dir)

	// The new tag normally doesn't exist yet (it's being generated), so the
	// range always ends at HEAD.
	revRange := "HEAD"
	if oldTag != "" {
		revRange = oldTag + "..HEAD"
	}

	logOut, logErr := RunGit(ctx, dir, "log", "--no-merges", "--pretty=format:%h %s", revRange)
	if logErr != nil {
		logOut, logErr = RunGit(ctx, dir, "log", "--no-merges", "-1", "--pretty=format:%h %s", "HEAD")
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
		if diffOut, err := RunGit(ctx, dir, "diff", "--stat", oldTag, "HEAD"); err == nil {
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
