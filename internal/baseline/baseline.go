// Package baseline persists a snapshot of detected clone clusters and
// diffs a later run against it — the clone-watchlist / drift-alert
// surface (roadmap bet #5). `--update-baseline <file>` writes a
// Snapshot after a normal scan; `--baseline <file>` compares a later
// scan against it and reports drift Events.
//
// # Member identity across runs
//
// Snippet names embed line ranges ("path:12-40 Symbol"), which shift on
// routine edits. A snapshot member is therefore identified by:
//
//   - its line-range-stripped name ("path Symbol"), using exactly the
//     normalization config's ignore_pairs matcher applies
//     (config.ParseSnippetName), and
//   - that path made relative to the scan roots (NewKeyer), so
//     "codetwin --baseline f.json ./src" matches a snapshot taken from
//     a different working directory or sibling tree.
//
// Each member also carries a hash of its normalized token stream
// (identifiers→VAR, literals→STR/NUM, whitespace-invariant), so a body
// edit that leaves the member inside its cluster is still visible and
// surfaces as a member-changed event. Formatting, comment, and
// rename-only edits do not change the hash.
//
// # Determinism
//
// Save writes clusters sorted by first member key, members sorted by
// key, and NO timestamp: two --update-baseline runs over the same tree
// produce byte-identical files. A created-at field was deliberately
// left out of the schema for exactly this reason — the file's mtime and
// the VCS history carry that information.
package baseline

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/ccsrvs/codetwin/internal/config"
)

// SchemaVersion is the snapshot format version. Load refuses files
// written by a different version with an explicit error, so a schema
// change can never be silently misread as drift.
const SchemaVersion = 1

// Params records the scan parameters that affect cluster comparability.
// A --baseline run whose parameters differ from the snapshot's is a
// user error (the cluster sets aren't comparable) and is rejected up
// front via Mismatches.
type Params struct {
	Threshold    float64 `json:"threshold"`
	Eps          float64 `json:"eps"`
	MinPts       int     `json:"min_pts"`
	Granularity  string  `json:"granularity"`
	IncludeTests bool    `json:"include_tests"`
}

// Mismatches describes every field where p (the snapshot's params) and
// current (this run's params) disagree, one human-readable string per
// field. Empty means the runs are comparable.
func (p Params) Mismatches(current Params) []string {
	var out []string
	if p.Threshold != current.Threshold {
		out = append(out, fmt.Sprintf("threshold: baseline %g, current %g", p.Threshold, current.Threshold))
	}
	if p.Eps != current.Eps {
		out = append(out, fmt.Sprintf("eps: baseline %g, current %g", p.Eps, current.Eps))
	}
	if p.MinPts != current.MinPts {
		out = append(out, fmt.Sprintf("min-pts: baseline %d, current %d", p.MinPts, current.MinPts))
	}
	if p.Granularity != current.Granularity {
		out = append(out, fmt.Sprintf("granularity: baseline %q, current %q", p.Granularity, current.Granularity))
	}
	if p.IncludeTests != current.IncludeTests {
		out = append(out, fmt.Sprintf("include-tests: baseline %v, current %v", p.IncludeTests, current.IncludeTests))
	}
	return out
}

// Member is one cluster member in a snapshot: a durable identity key
// plus a normalized-token content hash.
type Member struct {
	Key  string `json:"key"`
	Hash string `json:"hash"`
}

// Cluster is one clone family in a snapshot, members sorted by Key.
type Cluster struct {
	Members []Member `json:"members"`
}

func (c Cluster) has(key string) bool {
	for _, m := range c.Members {
		if m.Key == key {
			return true
		}
	}
	return false
}

func (c Cluster) firstKey() string {
	if len(c.Members) == 0 {
		return ""
	}
	return c.Members[0].Key
}

// keys returns the member keys joined for display, capped so a huge
// cluster doesn't produce an unreadable drift line.
func (c Cluster) keys() string {
	const maxShown = 6
	names := make([]string, 0, len(c.Members))
	for _, m := range c.Members {
		names = append(names, m.Key)
	}
	if len(names) <= maxShown {
		return strings.Join(names, ", ")
	}
	return strings.Join(names[:maxShown], ", ") +
		fmt.Sprintf(" (+%d more)", len(names)-maxShown)
}

// Snapshot is the on-disk baseline: schema version, the tool version
// that wrote it (informational only — never compared), the scan
// parameters that gate comparability, and the cluster list.
type Snapshot struct {
	SchemaVersion int       `json:"schema_version"`
	ToolVersion   string    `json:"tool_version"`
	Params        Params    `json:"params"`
	Clusters      []Cluster `json:"clusters"`
}

// normalize sorts members within each cluster by key and clusters by
// their member-key sequence, making the snapshot canonical for both
// byte-stable serialization and positional diffing.
func (s *Snapshot) normalize() {
	for i := range s.Clusters {
		ms := s.Clusters[i].Members
		sort.Slice(ms, func(a, b int) bool { return ms[a].Key < ms[b].Key })
	}
	joined := func(c Cluster) string {
		var b strings.Builder
		for _, m := range c.Members {
			b.WriteString(m.Key)
			b.WriteByte('\n')
		}
		return b.String()
	}
	sort.Slice(s.Clusters, func(a, b int) bool {
		return joined(s.Clusters[a]) < joined(s.Clusters[b])
	})
}

// Save writes the snapshot to path as canonical, indented JSON. The
// snapshot is normalized first, so member/cluster order never depends
// on the caller's construction order.
func Save(file string, s Snapshot) error {
	s.normalize()
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode baseline: %w", err)
	}
	data = append(data, '\n')
	if err := os.WriteFile(file, data, 0o644); err != nil {
		return fmt.Errorf("write baseline: %w", err)
	}
	return nil
}

// Load reads a snapshot from path. Files with a missing or mismatched
// schema_version fail with an explicit error telling the user to
// regenerate the baseline, never a silent misread.
func Load(file string) (Snapshot, error) {
	data, err := os.ReadFile(file)
	if err != nil {
		return Snapshot{}, fmt.Errorf("read baseline: %w", err)
	}
	var probe struct {
		SchemaVersion *int `json:"schema_version"`
	}
	if err := json.Unmarshal(data, &probe); err != nil {
		return Snapshot{}, fmt.Errorf("parse baseline %s: %w", file, err)
	}
	if probe.SchemaVersion == nil {
		return Snapshot{}, fmt.Errorf("baseline %s has no schema_version — not a codetwin baseline file", file)
	}
	if *probe.SchemaVersion != SchemaVersion {
		return Snapshot{}, fmt.Errorf(
			"baseline %s has schema version %d; this codetwin reads version %d — regenerate it with --update-baseline",
			file, *probe.SchemaVersion, SchemaVersion)
	}
	var s Snapshot
	if err := json.Unmarshal(data, &s); err != nil {
		return Snapshot{}, fmt.Errorf("parse baseline %s: %w", file, err)
	}
	s.normalize()
	return s, nil
}

// ── Member identity ───────────────────────────────────────────────────────────

// NewKeyer returns the snippet-name → member-key function for a scan
// over the given roots (the CLI path arguments). The key is the
// line-range-stripped snippet name (config.ParseSnippetName — the same
// normalization ignore_pairs uses) with the path made relative to the
// longest matching root and slash-normalized, so a snapshot taken with
// `codetwin --update-baseline f.json ./before` compares cleanly against
// `codetwin --baseline f.json ./after` (or the same tree scanned from a
// different working directory). A root that IS the file maps to the
// file's base name. Paths under no root pass through unchanged.
func NewKeyer(roots []string) func(name string) string {
	cleaned := make([]string, 0, len(roots))
	for _, r := range roots {
		c := filepath.ToSlash(filepath.Clean(r))
		if c != "." && c != "" {
			cleaned = append(cleaned, c)
		}
	}
	// Longest root first so nested roots resolve to the most specific one.
	sort.Slice(cleaned, func(i, j int) bool { return len(cleaned[i]) > len(cleaned[j]) })
	return func(name string) string {
		p, sym := config.ParseSnippetName(name)
		p = filepath.ToSlash(filepath.Clean(p))
		for _, r := range cleaned {
			if p == r {
				p = path.Base(p)
				break
			}
			if strings.HasPrefix(p, r+"/") {
				p = p[len(r)+1:]
				break
			}
		}
		if sym == "" {
			return p
		}
		return p + " " + sym
	}
}

// HashTokens returns a 16-hex-char digest of a normalized token stream.
// Tokens are length-prefixed before hashing so stream boundaries are
// unambiguous ("ab","c" never collides with "a","bc").
func HashTokens(tokens []string) string {
	h := sha1.New()
	for _, t := range tokens {
		h.Write([]byte(strconv.Itoa(len(t))))
		h.Write([]byte{':'})
		h.Write([]byte(t))
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// BuildClusters converts report-cluster member-name lists into snapshot
// Clusters. tokensByName supplies each snippet's normalized token
// stream for content hashing; key derives the durable member key
// (NewKeyer). Members whose keys collide inside one cluster — e.g. two
// Elixir clauses of the same def — are merged into a single member
// whose hash combines the colliding hashes (sorted, then re-hashed), so
// any clause changing still fires member-changed. Empty member lists
// are dropped. The result is normalized (sorted) and deterministic.
func BuildClusters(
	memberLists [][]string,
	tokensByName map[string][]string,
	key func(string) string,
) []Cluster {
	out := make([]Cluster, 0, len(memberLists))
	for _, names := range memberLists {
		if len(names) == 0 {
			continue
		}
		byKey := make(map[string][]string, len(names))
		for _, n := range names {
			k := key(n)
			byKey[k] = append(byKey[k], HashTokens(tokensByName[n]))
		}
		members := make([]Member, 0, len(byKey))
		for k, hashes := range byKey {
			members = append(members, Member{Key: k, Hash: combineHashes(hashes)})
		}
		sort.Slice(members, func(a, b int) bool { return members[a].Key < members[b].Key })
		out = append(out, Cluster{Members: members})
	}
	s := Snapshot{Clusters: out}
	s.normalize()
	return s.Clusters
}

func combineHashes(hs []string) string {
	if len(hs) == 1 {
		return hs[0]
	}
	sort.Strings(hs)
	sum := sha1.Sum([]byte(strings.Join(hs, "|")))
	return hex.EncodeToString(sum[:])[:16]
}

// ── Drift detection ───────────────────────────────────────────────────────────

// EventKind names one kind of drift between a baseline and the current
// scan.
type EventKind string

const (
	// MemberAdded: a matched cluster gained a member.
	MemberAdded EventKind = "member-added"
	// MemberRemoved: a matched cluster lost a member.
	MemberRemoved EventKind = "member-removed"
	// MemberChanged: a member is still in its cluster but its
	// normalized-token body hash changed.
	MemberChanged EventKind = "member-changed"
	// ClusterAppeared: a current cluster matches no baseline cluster
	// above the overlap floor.
	ClusterAppeared EventKind = "cluster-appeared"
	// ClusterDissolved: a baseline cluster matches no current cluster
	// above the overlap floor.
	ClusterDissolved EventKind = "cluster-dissolved"
)

// Event is one drift finding. Cluster is the index of the cluster in
// the CURRENT snapshot's (sorted) cluster list — except for
// ClusterDissolved, where no current cluster exists and the index
// refers to the BASELINE snapshot's list. Detail always carries member
// keys, so events are self-describing regardless of the index.
type Event struct {
	Kind    EventKind `json:"kind"`
	Cluster int       `json:"cluster"`
	Detail  string    `json:"detail"`
}

// String renders the stable one-line stderr format:
//
//	drift: <kind> cluster <n>: <detail>
func (e Event) String() string {
	return fmt.Sprintf("drift: %s cluster %d: %s", e.Kind, e.Cluster, e.Detail)
}

// MinOverlap is the cluster-matching floor: a baseline and a current
// cluster are match candidates only when they share at least half the
// members of the smaller cluster (overlap coefficient ≥ 0.5, on member
// keys). Below the floor the pair reads as one cluster dissolving and
// an unrelated one appearing. The overlap coefficient — rather than
// Jaccard — is the floor so a cluster that doubles in size still
// matches its baseline; Jaccard then RANKS the candidates so the most
// specific match wins.
const MinOverlap = 0.5

// Diff compares the current snapshot against the baseline and returns
// drift events. Clusters are matched greedily by highest member-key
// Jaccard among candidates above MinOverlap, ties broken by first
// member key (baseline side first, then current side), so matching is
// fully deterministic. Both snapshots are normalized in place. Event
// order: per current cluster (added, removed, changed — each sorted by
// member key), appeared clusters in position, then dissolved baseline
// clusters.
func Diff(base, cur Snapshot) []Event {
	base.normalize()
	cur.normalize()

	type cand struct {
		bi, ci int
		jac    float64
	}
	var cands []cand
	for bi, b := range base.Clusters {
		for ci, c := range cur.Clusters {
			inter := 0
			for _, m := range b.Members {
				if c.has(m.Key) {
					inter++
				}
			}
			if inter == 0 {
				continue
			}
			minSize := len(b.Members)
			if len(c.Members) < minSize {
				minSize = len(c.Members)
			}
			if float64(inter)/float64(minSize) < MinOverlap {
				continue
			}
			union := len(b.Members) + len(c.Members) - inter
			cands = append(cands, cand{bi: bi, ci: ci, jac: float64(inter) / float64(union)})
		}
	}
	sort.Slice(cands, func(i, j int) bool {
		a, b := cands[i], cands[j]
		if a.jac != b.jac {
			return a.jac > b.jac
		}
		ka, kb := base.Clusters[a.bi].firstKey(), base.Clusters[b.bi].firstKey()
		if ka != kb {
			return ka < kb
		}
		kca, kcb := cur.Clusters[a.ci].firstKey(), cur.Clusters[b.ci].firstKey()
		if kca != kcb {
			return kca < kcb
		}
		if a.bi != b.bi {
			return a.bi < b.bi
		}
		return a.ci < b.ci
	})

	matchedB := make(map[int]int, len(base.Clusters)) // bi → ci
	matchedC := make(map[int]int, len(cur.Clusters))  // ci → bi
	for _, c := range cands {
		if _, taken := matchedB[c.bi]; taken {
			continue
		}
		if _, taken := matchedC[c.ci]; taken {
			continue
		}
		matchedB[c.bi] = c.ci
		matchedC[c.ci] = c.bi
	}

	var events []Event
	for ci, c := range cur.Clusters {
		bi, ok := matchedC[ci]
		if !ok {
			events = append(events, Event{
				Kind: ClusterAppeared, Cluster: ci,
				Detail: fmt.Sprintf("%d members: %s", len(c.Members), c.keys()),
			})
			continue
		}
		b := base.Clusters[bi]
		baseHash := make(map[string]string, len(b.Members))
		for _, m := range b.Members {
			baseHash[m.Key] = m.Hash
		}
		for _, m := range c.Members {
			if _, in := baseHash[m.Key]; !in {
				events = append(events, Event{Kind: MemberAdded, Cluster: ci, Detail: m.Key})
			}
		}
		for _, m := range b.Members {
			if !c.has(m.Key) {
				events = append(events, Event{Kind: MemberRemoved, Cluster: ci, Detail: m.Key})
			}
		}
		for _, m := range c.Members {
			if bh, in := baseHash[m.Key]; in && bh != m.Hash {
				events = append(events, Event{Kind: MemberChanged, Cluster: ci, Detail: m.Key})
			}
		}
	}
	for bi, b := range base.Clusters {
		if _, ok := matchedB[bi]; !ok {
			events = append(events, Event{
				Kind: ClusterDissolved, Cluster: bi,
				Detail: fmt.Sprintf("baseline cluster of %d members no longer detected: %s",
					len(b.Members), b.keys()),
			})
		}
	}
	return events
}
