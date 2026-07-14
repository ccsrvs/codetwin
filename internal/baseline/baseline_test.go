package baseline

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// ── helpers ───────────────────────────────────────────────────────────────────

// mk builds a Cluster from "key=hash" member specs.
func mk(members ...string) Cluster {
	c := Cluster{}
	for _, m := range members {
		key, hash := m, "h0"
		if i := strings.LastIndex(m, "="); i >= 0 {
			key, hash = m[:i], m[i+1:]
		}
		c.Members = append(c.Members, Member{Key: key, Hash: hash})
	}
	return c
}

func snap(clusters ...Cluster) Snapshot {
	return Snapshot{
		SchemaVersion: SchemaVersion,
		ToolVersion:   "test",
		Params: Params{
			Threshold: 0.5, Eps: 0.35, MinPts: 2,
			Granularity: "function",
		},
		Clusters: clusters,
	}
}

func kinds(events []Event) []EventKind {
	out := make([]EventKind, 0, len(events))
	for _, e := range events {
		out = append(out, e.Kind)
	}
	return out
}

// ── Save / Load round-trip ────────────────────────────────────────────────────

func TestSaveLoad_RoundTripsSnapshotUnchanged(t *testing.T) {
	file := filepath.Join(t.TempDir(), "b.json")
	s := snap(mk("a.go F=11", "b.go F=22"), mk("c.go G=33", "d.go G=44"))

	if err := Save(file, s); err != nil {
		t.Fatalf("Save: %v", err)
	}
	got, err := Load(file)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	want := s
	want.normalize()
	if !reflect.DeepEqual(got, want) {
		t.Errorf("round-trip mismatch:\ngot  %+v\nwant %+v", got, want)
	}
}

// TestSave_IsByteDeterministic: the determinism contract — same logical
// snapshot, twice, regardless of construction order, must produce
// byte-identical files (the schema deliberately has no timestamp).
func TestSave_IsByteDeterministic(t *testing.T) {
	dir := t.TempDir()
	f1 := filepath.Join(dir, "1.json")
	f2 := filepath.Join(dir, "2.json")

	// Same clusters/members, scrambled construction order.
	s1 := snap(mk("a.go F=11", "b.go F=22"), mk("c.go G=33", "d.go G=44"))
	s2 := snap(mk("d.go G=44", "c.go G=33"), mk("b.go F=22", "a.go F=11"))

	if err := Save(f1, s1); err != nil {
		t.Fatalf("Save 1: %v", err)
	}
	if err := Save(f2, s2); err != nil {
		t.Fatalf("Save 2: %v", err)
	}
	b1, _ := os.ReadFile(f1)
	b2, _ := os.ReadFile(f2)
	if string(b1) != string(b2) {
		t.Errorf("snapshots differ byte-wise:\n%s\n---\n%s", b1, b2)
	}
	if len(b1) == 0 || b1[len(b1)-1] != '\n' {
		t.Errorf("snapshot should end with a newline")
	}
}

func TestLoad_SchemaVersionMismatch_ReturnsClearError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "b.json")
	doc := map[string]any{"schema_version": SchemaVersion + 99, "clusters": []any{}}
	data, _ := json.Marshal(doc)
	if err := os.WriteFile(file, data, 0o644); err != nil {
		t.Fatal(err)
	}

	_, err := Load(file)
	if err == nil {
		t.Fatal("Load should fail on a schema version mismatch")
	}
	msg := err.Error()
	if !strings.Contains(msg, "schema version") || !strings.Contains(msg, "--update-baseline") {
		t.Errorf("error should name the schema version and the fix, got: %v", err)
	}
}

func TestLoad_MissingSchemaVersion_ReturnsClearError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "b.json")
	if err := os.WriteFile(file, []byte(`{"clusters": []}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(file); err == nil || !strings.Contains(err.Error(), "schema_version") {
		t.Errorf("Load should reject a file without schema_version, got: %v", err)
	}
}

func TestLoad_InvalidJSON_ReturnsError(t *testing.T) {
	file := filepath.Join(t.TempDir(), "b.json")
	if err := os.WriteFile(file, []byte("not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(file); err == nil {
		t.Error("Load should fail on invalid JSON")
	}
}

func TestLoad_MissingFile_ReturnsError(t *testing.T) {
	if _, err := Load(filepath.Join(t.TempDir(), "nope.json")); err == nil {
		t.Error("Load should fail on a missing file")
	}
}

// ── Params comparability ──────────────────────────────────────────────────────

func TestParams_Mismatches_EmptyWhenEqual(t *testing.T) {
	p := Params{Threshold: 0.5, Eps: 0.35, MinPts: 2, Granularity: "function"}
	if got := p.Mismatches(p); len(got) != 0 {
		t.Errorf("equal params should have no mismatches, got %v", got)
	}
}

func TestParams_Mismatches_ReportsEveryDifferingField(t *testing.T) {
	base := Params{Threshold: 0.5, Eps: 0.35, MinPts: 2, Granularity: "function", IncludeTests: false}
	cur := Params{Threshold: 0.8, Eps: 0.25, MinPts: 3, Granularity: "file", IncludeTests: true}
	got := base.Mismatches(cur)
	if len(got) != 5 {
		t.Fatalf("want 5 mismatches, got %d: %v", len(got), got)
	}
	joined := strings.Join(got, "; ")
	for _, want := range []string{"threshold", "eps", "min-pts", "granularity", "include-tests"} {
		if !strings.Contains(joined, want) {
			t.Errorf("mismatches should mention %q: %s", want, joined)
		}
	}
}

// ── Member identity ───────────────────────────────────────────────────────────

func TestNewKeyer_StripsLineRangeAndRoot(t *testing.T) {
	key := NewKeyer([]string{"testdata/baseline/before"})
	cases := map[string]string{
		"testdata/baseline/before/alpha.go:3-17 SumEvenA": "alpha.go SumEvenA",
		"testdata/baseline/before/sub/x.go:1-5":           "sub/x.go",
		"testdata/baseline/before/alpha.go":               "alpha.go", // whole-file snippet
		"elsewhere/y.go:2-9 F":                            "elsewhere/y.go F",
	}
	for name, want := range cases {
		if got := key(name); got != want {
			t.Errorf("key(%q) = %q, want %q", name, got, want)
		}
	}
}

func TestNewKeyer_LongestRootWins(t *testing.T) {
	key := NewKeyer([]string{"repo", "repo/internal"})
	if got := key("repo/internal/a.go:1-4 F"); got != "a.go F" {
		t.Errorf("nested root should win: got %q, want %q", got, "a.go F")
	}
}

func TestNewKeyer_FileRootUsesBaseName(t *testing.T) {
	key := NewKeyer([]string{"testdata/sum.go", "testdata/sum.py"})
	if got := key("testdata/sum.go:1-10 Sum"); got != "sum.go Sum" {
		t.Errorf("file root: got %q, want %q", got, "sum.go Sum")
	}
}

func TestNewKeyer_DotRootLeavesPathAlone(t *testing.T) {
	key := NewKeyer([]string{"."})
	if got := key("internal/a.go:1-4 F"); got != "internal/a.go F" {
		t.Errorf("dot root: got %q, want %q", got, "internal/a.go F")
	}
}

func TestHashTokens_DiffersOnContentAndBoundaries(t *testing.T) {
	a := HashTokens([]string{"if", "VAR", "==", "STR"})
	b := HashTokens([]string{"if", "VAR", "!=", "STR"})
	if a == b {
		t.Error("different token streams must hash differently")
	}
	// Boundary ambiguity guard: {"ab","c"} vs {"a","bc"}.
	if HashTokens([]string{"ab", "c"}) == HashTokens([]string{"a", "bc"}) {
		t.Error("length-prefixing should disambiguate token boundaries")
	}
	if a != HashTokens([]string{"if", "VAR", "==", "STR"}) {
		t.Error("hash must be stable for identical streams")
	}
	if len(a) != 16 {
		t.Errorf("hash length = %d, want 16", len(a))
	}
}

func TestBuildClusters_SortsAndHashes(t *testing.T) {
	tokens := map[string][]string{
		"d/b.go:5-9 F":  {"x", "y"},
		"d/a.go:1-4 F":  {"x", "y"},
		"d/c.go:2-8 G":  {"z"},
		"d/e.go:3-11 G": {"z", "w"},
	}
	key := NewKeyer([]string{"d"})
	got := BuildClusters([][]string{
		{"d/c.go:2-8 G", "d/e.go:3-11 G"},
		{"d/b.go:5-9 F", "d/a.go:1-4 F"},
	}, tokens, key)

	if len(got) != 2 {
		t.Fatalf("want 2 clusters, got %d", len(got))
	}
	// Clusters sorted by first member key: "a.go F" < "c.go G".
	if got[0].Members[0].Key != "a.go F" || got[0].Members[1].Key != "b.go F" {
		t.Errorf("cluster 0 members = %+v, want sorted a.go F, b.go F", got[0].Members)
	}
	if got[0].Members[0].Hash != got[0].Members[1].Hash {
		t.Errorf("identical token streams must produce identical hashes")
	}
	if got[1].Members[0].Hash == got[1].Members[1].Hash {
		t.Errorf("different token streams must produce different hashes")
	}
}

// TestBuildClusters_DuplicateKeysMerge: two chunks with the same durable
// key (e.g. Elixir multi-clause defs) merge into one member whose hash
// still reacts when any clause's body changes.
func TestBuildClusters_DuplicateKeysMerge(t *testing.T) {
	key := NewKeyer(nil)
	before := BuildClusters([][]string{{"m.ex:1-4 parse", "m.ex:5-9 parse", "n.ex:1-9 parse"}},
		map[string][]string{
			"m.ex:1-4 parse": {"a"},
			"m.ex:5-9 parse": {"b"},
			"n.ex:1-9 parse": {"c"},
		}, key)
	after := BuildClusters([][]string{{"m.ex:1-4 parse", "m.ex:5-9 parse", "n.ex:1-9 parse"}},
		map[string][]string{
			"m.ex:1-4 parse": {"a"},
			"m.ex:5-9 parse": {"b", "b"}, // second clause's body changed
			"n.ex:1-9 parse": {"c"},
		}, key)

	if len(before) != 1 || len(before[0].Members) != 2 {
		t.Fatalf("duplicate keys should merge: got %+v", before)
	}
	var bHash, aHash string
	for _, m := range before[0].Members {
		if m.Key == "m.ex parse" {
			bHash = m.Hash
		}
	}
	for _, m := range after[0].Members {
		if m.Key == "m.ex parse" {
			aHash = m.Hash
		}
	}
	if bHash == "" || aHash == "" {
		t.Fatal("merged member 'm.ex parse' not found")
	}
	if bHash == aHash {
		t.Error("a clause body change must alter the merged member hash")
	}
}

// ── Drift detection ───────────────────────────────────────────────────────────

func TestDiff_NoDrift_ReturnsNoEvents(t *testing.T) {
	base := snap(mk("a F=1", "b F=2"), mk("c G=3", "d G=4"))
	cur := snap(mk("c G=3", "d G=4"), mk("a F=1", "b F=2")) // reordered
	if got := Diff(base, cur); len(got) != 0 {
		t.Errorf("identical snapshots should produce no drift, got %v", got)
	}
}

// TestDiff_AllFiveKinds: one synthetic before/after covering every
// event kind exactly once.
func TestDiff_AllFiveKinds(t *testing.T) {
	base := snap(
		mk("a1=h", "a2=h"),         // gains a3 → member-added
		mk("b1=h", "b2=h", "b3=h"), // loses b3 → member-removed
		mk("c1=h", "c2=old"),       // c2's body changes → member-changed
		mk("d1=h", "d2=h"),         // vanishes → cluster-dissolved
	)
	cur := snap(
		mk("a1=h", "a2=h", "a3=h"),
		mk("b1=h", "b2=h"),
		mk("c1=h", "c2=new"),
		mk("e1=h", "e2=h"), // brand new → cluster-appeared
	)

	events := Diff(base, cur)
	if len(events) != 5 {
		t.Fatalf("want exactly 5 events, got %d: %v", len(events), events)
	}
	byKind := map[EventKind][]Event{}
	for _, e := range events {
		byKind[e.Kind] = append(byKind[e.Kind], e)
	}
	for kind, wantDetail := range map[EventKind]string{
		MemberAdded:   "a3",
		MemberRemoved: "b3",
		MemberChanged: "c2",
	} {
		es := byKind[kind]
		if len(es) != 1 {
			t.Fatalf("%s: want exactly 1 event, got %v", kind, es)
		}
		if es[0].Detail != wantDetail {
			t.Errorf("%s detail = %q, want %q", kind, es[0].Detail, wantDetail)
		}
	}
	if es := byKind[ClusterAppeared]; len(es) != 1 || !strings.Contains(es[0].Detail, "e1") {
		t.Errorf("cluster-appeared: want 1 event naming e1, got %v", es)
	}
	if es := byKind[ClusterDissolved]; len(es) != 1 || !strings.Contains(es[0].Detail, "d1") {
		t.Errorf("cluster-dissolved: want 1 event naming d1, got %v", es)
	}
}

// TestDiff_OverlapFloor: clusters sharing at least half the smaller
// side's members match; below the floor they read as dissolve+appear.
func TestDiff_OverlapFloor(t *testing.T) {
	// Overlap coefficient 1/2 = 0.5 → matched (one removed, one added).
	base := snap(mk("a=1", "b=1"))
	cur := snap(mk("a=1", "c=1"))
	got := kinds(Diff(base, cur))
	want := []EventKind{MemberAdded, MemberRemoved}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("at the floor: got %v, want %v", got, want)
	}

	// Overlap coefficient 1/3 < 0.5 → dissolved + appeared.
	base = snap(mk("a=1", "b=1", "c=1"))
	cur = snap(mk("a=1", "x=1", "y=1"))
	got = kinds(Diff(base, cur))
	want = []EventKind{ClusterAppeared, ClusterDissolved}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("below the floor: got %v, want %v", got, want)
	}
}

// TestDiff_GrownClusterStillMatches: overlap coefficient (not Jaccard)
// is the floor, so a baseline cluster that doubled in size still
// matches and reports member-added events rather than dissolve+appear.
func TestDiff_GrownClusterStillMatches(t *testing.T) {
	base := snap(mk("a=1", "b=1"))
	cur := snap(mk("a=1", "b=1", "c=1", "d=1", "e=1", "f=1"))
	got := kinds(Diff(base, cur))
	want := []EventKind{MemberAdded, MemberAdded, MemberAdded, MemberAdded}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("grown cluster: got %v, want %v", got, want)
	}
}

// TestDiff_GreedyMatchPrefersHighestJaccard: when a baseline cluster
// could match two current clusters, the higher-Jaccard one wins and the
// other reads as appeared.
func TestDiff_GreedyMatchPrefersHighestJaccard(t *testing.T) {
	base := snap(mk("a=1", "b=1", "c=1"))
	cur := snap(
		mk("a=1", "b=1", "c=1"), // Jaccard 1.0
		mk("a=1", "b=1", "z=1"), // Jaccard 0.5
	)
	events := Diff(base, cur)
	if len(events) != 1 || events[0].Kind != ClusterAppeared {
		t.Fatalf("want exactly one cluster-appeared for the split-off, got %v", events)
	}
	if !strings.Contains(events[0].Detail, "z") {
		t.Errorf("the lower-overlap cluster should be the appeared one: %v", events[0])
	}
}

// TestDiff_TieBreakIsDeterministic: two candidates with identical
// Jaccard resolve by first member key, every time.
func TestDiff_TieBreakIsDeterministic(t *testing.T) {
	base := snap(mk("a=1", "b=1"))
	cur := snap(
		mk("a=1", "x=1"), // Jaccard 1/3
		mk("b=1", "y=1"), // Jaccard 1/3
	)
	var first []Event
	for i := 0; i < 20; i++ {
		events := Diff(base, cur)
		if i == 0 {
			first = events
			continue
		}
		if !reflect.DeepEqual(events, first) {
			t.Fatalf("Diff is nondeterministic: run 0 %v, run %d %v", first, i, events)
		}
	}
	// The base cluster {a,b} ties between {a,x} and {b,y}; the current
	// cluster with the smaller first key ("a...") must win.
	byKind := map[EventKind][]Event{}
	for _, e := range first {
		byKind[e.Kind] = append(byKind[e.Kind], e)
	}
	if es := byKind[ClusterAppeared]; len(es) != 1 || !strings.Contains(es[0].Detail, "b, y") {
		t.Errorf("tie-break: {b,y} should be the appeared cluster, got %v", first)
	}
}

func TestEvent_String_StableFormat(t *testing.T) {
	e := Event{Kind: MemberAdded, Cluster: 2, Detail: "alpha.go SumEvenC"}
	want := "drift: member-added cluster 2: alpha.go SumEvenC"
	if got := e.String(); got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

// TestDiff_LargeClusterDetailCapped: appeared/dissolved details cap the
// member list so drift lines stay readable.
func TestDiff_LargeClusterDetailCapped(t *testing.T) {
	members := make([]string, 10)
	for i := range members {
		members[i] = strings.Repeat("m", 1) + string(rune('a'+i)) + "=1"
	}
	events := Diff(snap(), snap(mk(members...)))
	if len(events) != 1 || events[0].Kind != ClusterAppeared {
		t.Fatalf("want one cluster-appeared, got %v", events)
	}
	if !strings.Contains(events[0].Detail, "(+4 more)") {
		t.Errorf("detail should cap at 6 keys: %q", events[0].Detail)
	}
}
