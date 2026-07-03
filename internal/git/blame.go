package git

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// ErrFileNotTracked is returned by Blame when the target file is not
// tracked by git (e.g. it's brand new and never been added). Callers
// should treat this as "no provenance available" rather than fatal.
var ErrFileNotTracked = errors.New("file is not tracked by git")

// BlameRange aggregates the blame metadata for a [Start, End] line
// range: the oldest (First*) and newest (Last*) author info. For a
// range whose lines all originate in the same commit, First* and Last*
// hold the same values.
type BlameRange struct {
	FirstCommit string
	FirstAuthor string
	FirstTime   time.Time
	LastCommit  string
	LastAuthor  string
	LastTime    time.Time
}

// Blame runs `git blame --line-porcelain` on absPath restricted to
// [start, end] and aggregates the per-line metadata into a single
// BlameRange. start and end are 1-based and inclusive.
//
// Returns ErrFileNotTracked when the file isn't tracked by git so the
// caller can downgrade to "no provenance" without aborting the run.
// Other failure modes (bad path, broken git, invalid range) propagate
// as plain errors.
func (r *Repo) Blame(absPath string, start, end int) (BlameRange, error) {
	if start < 1 || end < start {
		return BlameRange{}, fmt.Errorf("invalid blame range [%d, %d]", start, end)
	}
	rel, ok := relWithinRoot(r.Root, absPath)
	if !ok {
		return BlameRange{}, fmt.Errorf("path outside repo: %s", absPath)
	}

	out, err := r.run("blame", "--line-porcelain",
		"-L", fmt.Sprintf("%d,%d", start, end), "--", rel)
	if err != nil {
		// `git blame` complains with a fatal error containing
		// "no such path" when the file isn't tracked. We translate
		// that into ErrFileNotTracked so callers don't have to grep
		// the error string themselves.
		msg := err.Error()
		if strings.Contains(msg, "no such path") || strings.Contains(msg, "no matches found") {
			return BlameRange{}, ErrFileNotTracked
		}
		return BlameRange{}, err
	}
	return parseBlamePorcelain(out)
}

// parseBlamePorcelain walks the porcelain stream and returns the
// aggregated BlameRange. The porcelain format emits one record per line
// of the source range, each starting with `<sha> <orig> <final> [count]`
// followed by header lines (author, author-time, author-tz, committer,
// committer-time, committer-tz, summary, optional boundary, filename)
// then the source line prefixed with a single tab. Subsequent records
// for the same commit may abbreviate, omitting the headers — we cache
// the headers we've already seen by SHA to handle that.
func parseBlamePorcelain(out []byte) (BlameRange, error) {
	type commitMeta struct {
		author string
		t      time.Time
	}
	commits := map[string]commitMeta{}

	var br BlameRange
	have := false

	sc := bufio.NewScanner(bytes.NewReader(out))
	sc.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	var curSHA string
	var curAuthor string
	var curTime time.Time
	for sc.Scan() {
		line := sc.Text()
		if line == "" {
			continue
		}
		// Header records start with a 40-char SHA. We use length and
		// hex-ish first byte as a cheap discriminator before doing
		// more careful parsing.
		if len(line) >= 40 && isHexSHA(line[:40]) && (len(line) == 40 || line[40] == ' ') {
			// Flush the previous record into the aggregate.
			if curSHA != "" {
				updateRange(&br, &have, curSHA, curAuthor, curTime)
			}
			fields := strings.Fields(line)
			curSHA = fields[0]
			if cached, ok := commits[curSHA]; ok {
				curAuthor = cached.author
				curTime = cached.t
			} else {
				curAuthor = ""
				curTime = time.Time{}
			}
			continue
		}
		switch {
		case strings.HasPrefix(line, "author "):
			curAuthor = strings.TrimPrefix(line, "author ")
		case strings.HasPrefix(line, "author-time "):
			ts, err := strconv.ParseInt(strings.TrimPrefix(line, "author-time "), 10, 64)
			if err == nil {
				curTime = time.Unix(ts, 0).UTC()
			}
		case curSHA != "" && line[0] == '\t':
			// Source line marker — record what we've gathered so far
			// for this commit (covers SHAs whose headers were elided
			// on subsequent appearances).
			if _, ok := commits[curSHA]; !ok && curAuthor != "" && !curTime.IsZero() {
				commits[curSHA] = commitMeta{author: curAuthor, t: curTime}
			}
		}
	}
	if curSHA != "" {
		updateRange(&br, &have, curSHA, curAuthor, curTime)
	}
	if !have {
		return BlameRange{}, fmt.Errorf("blame produced no records")
	}
	return br, nil
}

func updateRange(br *BlameRange, have *bool, sha, author string, t time.Time) {
	if t.IsZero() {
		return
	}
	if !*have {
		br.FirstCommit, br.FirstAuthor, br.FirstTime = sha, author, t
		br.LastCommit, br.LastAuthor, br.LastTime = sha, author, t
		*have = true
		return
	}
	if t.Before(br.FirstTime) {
		br.FirstCommit, br.FirstAuthor, br.FirstTime = sha, author, t
	}
	if t.After(br.LastTime) {
		br.LastCommit, br.LastAuthor, br.LastTime = sha, author, t
	}
}

func isHexSHA(s string) bool {
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= '0' && c <= '9':
		case c >= 'a' && c <= 'f':
		case c >= 'A' && c <= 'F':
		default:
			return false
		}
	}
	return true
}
