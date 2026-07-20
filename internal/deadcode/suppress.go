package deadcode

import (
	"regexp"

	"github.com/ccsrvs/codetwin/internal/tokenizer"
)

// rustPubRe: `pub`, `pub(crate)`, `pub(super)` etc. on the definition line.
var rustPubRe = regexp.MustCompile(`\bpub\b`)

func nameSet(names ...string) map[string]bool {
	m := make(map[string]bool, len(names))
	for _, n := range names {
		m[n] = true
	}
	return m
}

// suppressedNames lists definitions dispatched without their name ever
// appearing at a call site in user code — runtime entry points,
// interface/trait methods the standard library invokes, operator
// overloads, and framework lifecycle hooks. Reporting these as dead
// would be wrong far more often than right, so they are never findings.
//
// The cost is symmetric: a genuinely dead function that happens to use
// one of these names is never reported either. That trade is accepted —
// this analysis promises a quiet, high-precision report, not recall.
var suppressedNames = map[tokenizer.Language]map[string]bool{
	tokenizer.Go: nameSet(
		"main", "init", "TestMain",
		// fmt / encoding interface methods, dispatched inside the stdlib.
		"String", "Error", "Format", "GoString",
		"MarshalJSON", "UnmarshalJSON", "MarshalText", "UnmarshalText",
		"MarshalBinary", "UnmarshalBinary", "GobEncode", "GobDecode",
		// net/http, io, sort — called through interfaces the caller
		// hands to stdlib machinery (io.Copy, sort.Sort, http.Serve).
		"ServeHTTP", "Read", "Write", "Close", "Len", "Less", "Swap",
	),
	tokenizer.Python: nameSet(
		"main",
		// unittest lifecycle.
		"setUp", "tearDown", "setUpClass", "tearDownClass",
		"setUpModule", "tearDownModule",
	),
	tokenizer.JavaScript: nameSet(
		"constructor", "toString", "toJSON", "valueOf",
		// React lifecycle.
		"render", "componentDidMount", "componentDidUpdate",
		"componentWillUnmount", "shouldComponentUpdate",
		"getDerivedStateFromProps", "componentDidCatch",
		"getSnapshotBeforeUpdate",
	),
	tokenizer.Java: nameSet(
		"main", "toString", "equals", "hashCode", "compareTo", "compare",
		"run", "call", "close", "iterator", "finalize", "clone",
		"readObject", "writeObject",
	),
	tokenizer.Rust: nameSet(
		"main",
		// Trait impls and operator overloads dispatched syntactically:
		// println! calls fmt, `a + b` calls add, `for` calls next.
		"fmt", "drop", "default", "deref", "deref_mut",
		"index", "index_mut", "add", "sub", "mul", "div", "rem", "neg", "not",
		"eq", "ne", "cmp", "partial_cmp", "hash",
		"next", "into_iter", "from_iter", "from_str", "from", "into",
		"try_from", "try_into", "clone", "as_ref", "as_mut", "borrow",
	),
	tokenizer.Elixir: nameSet(
		// OTP / GenServer / Supervisor callbacks: invoked by the runtime,
		// and start_link is called via `{Mod, arg}` child specs without
		// its name appearing.
		"init", "handle_call", "handle_cast", "handle_info",
		"handle_continue", "terminate", "code_change", "format_status",
		"child_spec", "start_link",
		// Phoenix / LiveView lifecycle.
		"mount", "render", "update", "handle_event", "handle_params",
		"handle_in", "handle_out", "join",
	),
}
