package stdlib

import (
	"context"
	"fmt"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/internal/inventory"
)

// sbUnitCount sits above the 128-unit batch interval of core's builtin work
// budget, so a loop that steps once per argument, element or produced part
// reaches a sync point mid-call instead of finishing inside the local batch.
const sbUnitCount = 200

// sbWideLen makes a payload charge unmistakable next to any header-sized
// quantity.
const sbWideLen = 4096

// sbStringsFile is the File every work phase and result branch in this seam
// must name.
const sbStringsFile = "plugins/stdlib/strings.go"

// sbErrorsFile is the File of the shared wrap site the string family's parse
// failures render through.
const sbErrorsFile = "plugins/stdlib/errors.go"

// sbInvalidByte is a byte that is not valid UTF-8, and sbQuoteFactor is what
// strconv.Quote and %q expand it into: \xNN, four characters per source byte.
const (
	sbInvalidByte = "\x80"
	sbQuoteFactor = 4
)

// sbPrimitiveMaxWork is the ceiling on a core.String's bytes of V, not on its
// charged size: core.StringShallowBytes bills core.MeterStringHeaderBytes (16)
// plus one byte per byte, so a String that fits under
// core.DefaultMaxAllocationBytes (67108864) carries at most 67108848 bytes and
// a Go string primitive can scan no more than those. It is the ceiling count's
// rune scan already records for the same reason.
const sbPrimitiveMaxWork = int64(67_108_848)

func sbGoFunc(t *testing.T, env *core.Env, name string) core.GoFunc {
	t.Helper()
	v, ok := env.Get(name)
	require.Truef(t, ok, "%s is not registered", name)
	fn, ok := v.(core.GoFunc)
	require.Truef(t, ok, "%s is not a GoFunc: %T", name, v)
	return fn
}

func sbStrings(n int, s string) []core.Value {
	vs := make([]core.Value, n)
	for i := range vs {
		vs[i] = core.String{V: s}
	}
	return vs
}

// sbScalable is every string builtin whose loop runs once per argument, per
// collection element or per produced part, each over an input above the batch
// interval.
func sbScalable() []struct {
	name string
	args []core.Value
} {
	return []struct {
		name string
		args []core.Value
	}{
		{"str", sbStrings(sbUnitCount, "a")},
		{"string/join", []core.Value{core.String{V: ","}, core.NewList(sbStrings(sbUnitCount, "a"))}},
		{"string/split", []core.Value{core.String{V: strings.Repeat("a,", sbUnitCount)}, core.String{V: ","}}},
		{"string/lines", []core.Value{core.String{V: strings.Repeat("a\n", sbUnitCount)}}},
	}
}

// TestStrings_TerminalUnderLowReductions: every string builtin whose cost grows
// with its input must charge that growth, so a call over a 200-unit input under
// a ceiling below one batch ends terminally instead of running to completion
// unmetered.
func TestStrings_TerminalUnderLowReductions(t *testing.T) {
	env := setupEnv(t)
	for _, c := range sbScalable() {
		t.Run(c.name, func(t *testing.T) {
			fn := sbGoFunc(t, env, c.name)
			ctx := core.WithEvalResourceLimits(t.Context(), 100, 1<<30)
			_, err := fn.Fn(ctx, nil, c.args, env)
			requireResourceLimit(t, err)
			require.Truef(t, core.IsTerminalEvalError(err),
				"%s over a %d-unit input under a 100-reduction ceiling must fail terminally, got %v", c.name, sbUnitCount, err)
		})
	}
}

// TestStrings_TerminalUnderExpiredDeadline: the engine-owned deadline is
// observed at the budget's sync point, so a long string loop surfaces
// context.DeadlineExceeded even though its own parent context is still live.
func TestStrings_TerminalUnderExpiredDeadline(t *testing.T) {
	env := setupEnv(t)
	for _, c := range sbScalable() {
		t.Run(c.name, func(t *testing.T) {
			fn := sbGoFunc(t, env, c.name)
			ctx := core.WithEvalResourceLimits(t.Context(), 1_000_000, 1<<30)
			ctx = core.WithEvalDeadline(ctx, time.Now().Add(-time.Millisecond))
			_, err := fn.Fn(ctx, nil, c.args, env)
			require.ErrorIsf(t, err, context.DeadlineExceeded,
				"%s over a %d-unit input past the engine deadline must surface DeadlineExceeded, got %v", c.name, sbUnitCount, err)
		})
	}
}

// TestStrings_TerminalUnderCancellation: caller cancellation is observed at the
// same sync point, so a long string loop cannot outlive the context that
// started it.
func TestStrings_TerminalUnderCancellation(t *testing.T) {
	env := setupEnv(t)
	for _, c := range sbScalable() {
		t.Run(c.name, func(t *testing.T) {
			fn := sbGoFunc(t, env, c.name)
			parent, cancel := context.WithCancel(context.Background())
			ctx := core.WithEvalResourceLimits(parent, 1_000_000, 1<<30)
			cancel()
			_, err := fn.Fn(ctx, nil, c.args, env)
			require.ErrorIsf(t, err, context.Canceled,
				"%s over a %d-unit input under a cancelled caller must surface Canceled, got %v", c.name, sbUnitCount, err)
		})
	}
}

// sbUnary dispatches a one-argument string builtin through the apply site,
// where the unmarked-result fallback charge lives, and returns the ledger total.
func sbUnary(t *testing.T, env *core.Env, name, in string) int64 {
	t.Helper()
	total, err := cbApplyCharge(t, env, sbGoFunc(t, env, name), 1<<30, core.String{V: in})
	require.NoError(t, err)
	return total
}

// TestStrings_UnchangedResultIsZeroByte: strings.ToUpper/ToLower/TrimSpace
// return the input itself when nothing changes, so that return owns no new
// bytes and its charge must not scale with the input. A 4096-byte subject
// returned unchanged must cost exactly what a 1-byte one does.
func TestStrings_UnchangedResultIsZeroByte(t *testing.T) {
	env := setupEnv(t)
	for _, c := range []struct{ name, wide, tiny string }{
		{"string/upper", strings.Repeat("A", sbWideLen), "A"},
		{"string/lower", strings.Repeat("a", sbWideLen), "a"},
		{"string/trim", strings.Repeat("a", sbWideLen), "a"},
	} {
		t.Run(c.name, func(t *testing.T) {
			wide := sbUnary(t, env, c.name, c.wide)
			tiny := sbUnary(t, env, c.name, c.tiny)
			require.Equalf(t, tiny, wide,
				"(%s <%d-byte subject returned unchanged>) charged %d bytes against %d for a 1-byte one: a result that IS its input owns no new bytes",
				c.name, sbWideLen, wide, tiny)
		})
	}
}

// TestStrings_FreshResultChargesShallowBytes is the other half of the pair: a
// result the Go primitive actually rebuilt is fresh, and its shallow size must
// enter the ledger. Without it, marking every unary return zero-byte would
// satisfy TestStrings_UnchangedResultIsZeroByte while billing nothing for
// output the builtin allocated.
func TestStrings_FreshResultChargesShallowBytes(t *testing.T) {
	env := setupEnv(t)
	fresh := core.StringShallowBytes(sbWideLen)
	for _, c := range []struct{ name, changed, unchanged string }{
		{"string/upper", strings.Repeat("a", sbWideLen), strings.Repeat("A", sbWideLen)},
		{"string/lower", strings.Repeat("A", sbWideLen), strings.Repeat("a", sbWideLen)},
	} {
		t.Run(c.name, func(t *testing.T) {
			changed := sbUnary(t, env, c.name, c.changed)
			unchanged := sbUnary(t, env, c.name, c.unchanged)
			require.GreaterOrEqualf(t, changed, unchanged+fresh,
				"(%s <%d-byte subject it rebuilt>) charged %d bytes against %d for one it returned unchanged: a rebuilt result must add its own %d shallow bytes",
				c.name, sbWideLen, changed, unchanged, fresh)
		})
	}
}

// sbSplitCharge splits a subject of parts copies of a payload and returns the
// ledger total for the dispatch.
func sbSplitCharge(t *testing.T, env *core.Env, parts, payloadLen int) int64 {
	t.Helper()
	subject := strings.TrimSuffix(strings.Repeat(strings.Repeat("a", payloadLen)+",", parts), ",")
	total, err := cbApplyCharge(t, env, sbGoFunc(t, env, "string/split"), 1<<30,
		core.String{V: subject}, core.String{V: ","})
	require.NoError(t, err)
	return total
}

// TestStrings_SplitChargesHeadersNotContents: strings.Split's parts share the
// subject's backing array, so the fresh bytes are the List plus one
// core.String header per part - never the part contents, which the subject
// already paid for.
//
// The first assertion is the contents half and holds today for a different
// reason (the apply-site fallback is shallow, so it never saw the contents
// either); it stays as the guard against a deep charge. The second is the
// header half: doubling the part count at a fixed subject length must move the
// ledger by one slot AND one string header per new part, which nothing charges
// today.
func TestStrings_SplitChargesHeadersNotContents(t *testing.T) {
	env := setupEnv(t)
	const parts = 64

	narrow := sbSplitCharge(t, env, parts, 1)
	wide := sbSplitCharge(t, env, parts, sbWideLen)
	require.Equalf(t, narrow, wide,
		"splitting into %d %d-byte parts charged %d bytes against %d for 1-byte parts: parts share the subject's bytes and must not be billed again",
		parts, sbWideLen, wide, narrow)

	doubled := sbSplitCharge(t, env, 2*parts, 1)
	headerDelta := core.ListShallowBytes(2*parts) - core.ListShallowBytes(parts) + int64(parts)*core.MeterStringHeaderBytes
	require.GreaterOrEqualf(t, doubled, narrow+headerDelta,
		"splitting into %d parts charged %d bytes against %d for %d parts: each part is a fresh core.String header, so %d more parts must add %d bytes",
		2*parts, doubled, narrow, parts, parts, headerDelta)
}

// sbParseCharge runs a failing parse over a subject of n copies of unit and
// returns the ledger total for the dispatch.
func sbParseCharge(t *testing.T, env *core.Env, name, unit string, n int) int64 {
	t.Helper()
	total, err := cbApplyCharge(t, env, sbGoFunc(t, env, name), 1<<30, core.String{V: strings.Repeat(unit, n)})
	require.Errorf(t, err, "%s over %d invalid bytes must fail to parse", name, n)
	return total
}

// TestStrings_ParseFailureMessageIsOwned: strconv.NumError quotes the whole
// parsed argument and wrapCause renders that quoted form again, so a failed
// parse materializes the subject a second time. That work grows with a single
// flat core.String argument, which has no children and so no sharing to
// amplify it - its ledger cost is its length - and it must be charged.
//
// The escaped arm is the worst case the charge has to cover: strconv.Quote
// emits \xNN for a byte that is not valid UTF-8, so a subject of invalid bytes
// renders four characters per source byte while the ledger counts one.
// Measured over 1 MiB subjects: NumError.Error() is 1048620 bytes of ASCII
// against 4194348 of invalid UTF-8, a factor of 4.0000.
func TestStrings_ParseFailureMessageIsOwned(t *testing.T) {
	env := setupEnv(t)
	const (
		small = 1 << 10
		large = 1 << 20
	)
	for _, name := range []string{"string->int", "string->float"} {
		t.Run(name, func(t *testing.T) {
			t.Run("ascii", func(t *testing.T) {
				tiny := sbParseCharge(t, env, name, "z", small)
				huge := sbParseCharge(t, env, name, "z", large)
				require.GreaterOrEqualf(t, huge-tiny, int64(large-small),
					"(%s <%d invalid bytes>) charged %d against %d for %d bytes: the failure branch renders the whole subject and must bill it",
					name, large, huge, tiny, small)
			})
			t.Run("escaped", func(t *testing.T) {
				tiny := sbParseCharge(t, env, name, sbInvalidByte, small)
				huge := sbParseCharge(t, env, name, sbInvalidByte, large)
				require.GreaterOrEqualf(t, huge-tiny, int64(sbQuoteFactor*(large-small)),
					"(%s <%d invalid UTF-8 bytes>) charged %d against %d for %d bytes: strconv.Quote renders each of them as \\xNN, so the charge must cover %d bytes per subject byte, not one",
					name, large, huge, tiny, small, sbQuoteFactor)
			})
		})
	}
	t.Run("inventory row", func(t *testing.T) {
		for _, p := range inventory.WorkPhases {
			if p.File != sbErrorsFile || p.Func != "wrapCause" || p.PhaseLabel != "strconv message format" {
				continue
			}
			require.Equalf(t, "bounded-exception", p.Disposition, "wrapCause/%s: disposition", p.PhaseLabel)
			require.Containsf(t, p.Families, "string", "wrapCause/%s: the strconv message is the string family's to bound", p.PhaseLabel)
			require.Containsf(t, p.Fn, "string->int", "wrapCause/%s: Fn must name string->int", p.PhaseLabel)
			require.Containsf(t, p.Fn, "string->float", "wrapCause/%s: Fn must name string->float", p.PhaseLabel)
			require.GreaterOrEqualf(t, p.MaxWork, int64(sbQuoteFactor)*sbPrimitiveMaxWork,
				"wrapCause/%s: MaxWork %d bills one byte per subject byte, but an invalid UTF-8 byte renders as \\xNN, so the ceiling is %d times the ledger's %d",
				p.PhaseLabel, p.MaxWork, sbQuoteFactor, sbPrimitiveMaxWork)
			return
		}
		t.Fatalf("inventory.WorkPhases has no %s row Func %q PhaseLabel %q: the strconv failure message the string family owns is unrecorded, and the support-family row still claims a bound it excludes",
			sbErrorsFile, "wrapCause", "strconv message format")
	})
}

// sbHostValue is a core.Value supplied by an embedding host: not one of the 13
// concrete kernel types, so str and format reach it only through its own
// String method.
type sbHostValue struct{ rendered string }

func (v sbHostValue) Type() core.Keyword { return core.Keyword{V: "host"} }
func (v sbHostValue) String() string     { return v.rendered }
func (v sbHostValue) Equals(o core.Value) bool {
	other, ok := o.(sbHostValue)
	return ok && other.rendered == v.rendered
}

// TestStrings_TrustedHostFormatting: a host value's String method is the host's
// own code, not stdlib's work to bound, so both formatting entries use it
// verbatim and the inventory records the arm as trusted-host.
func TestStrings_TrustedHostFormatting(t *testing.T) {
	env := setupEnv(t)
	host := sbHostValue{rendered: "#<host:token>"}

	got, err := cbApplyCharge(t, env, sbGoFunc(t, env, "str"), 1<<30, host)
	require.NoError(t, err)
	require.NotZero(t, got)

	str := sbGoFunc(t, env, "str")
	out, err := str.Fn(t.Context(), nil, []core.Value{host}, env)
	require.NoError(t, err)
	require.Equal(t, core.String{V: host.rendered}, out, "str must use a host value's String output verbatim")

	format := sbGoFunc(t, env, "format")
	out, err = format.Fn(t.Context(), nil, []core.Value{core.String{V: "%s"}, host}, env)
	require.NoError(t, err)
	require.Equal(t, core.String{V: host.rendered}, out, "format %%s must use a host value's String output verbatim")

	require.NotEmptyf(t, sbWorkPhases("toString", "trusted-host"),
		"inventory.WorkPhases has no %s row Func %q Disposition %q: the host-value arm of the formatting boundary is unrecorded",
		sbStringsFile, "toString", "trusted-host")
}

// sbWorkPhases returns the strings.go work phases for one function and
// disposition.
func sbWorkPhases(fn, disposition string) []inventory.WorkPhase {
	var out []inventory.WorkPhase
	for _, p := range inventory.WorkPhases {
		if p.File == sbStringsFile && p.Func == fn && p.Disposition == disposition {
			out = append(out, p)
		}
	}
	return out
}

// TestStrings_ToStringWalkIsUnboundedTracked: toString drops a core container
// into core boundedString, which walks the value as a tree while the ledger
// charged each shared node once, so a node reachable twice renders twice.
// str and string/join reach that walk with no pre-charge in front of it. No
// static ceiling bounds it and the allocation ledger does not either, so the
// phase is tracked against its owning change rather than given a false MaxWork.
func TestStrings_ToStringWalkIsUnboundedTracked(t *testing.T) {
	const owner = "Owned by core-value-walk-sharing-bound."
	rows := sbWorkPhases("toString", "unbounded-tracked")
	require.NotEmptyf(t, rows,
		"inventory.WorkPhases has no %s row Func %q Disposition %q: the container arm of the formatting boundary still claims a bound it does not have",
		sbStringsFile, "toString", "unbounded-tracked")
	for _, p := range rows {
		require.Zerof(t, p.MaxWork, "toString/%s: an unbounded-tracked phase must state no MaxWork", p.PhaseLabel)
		require.Truef(t, strings.HasSuffix(p.Proof, owner), "toString/%s: Proof must end with %q, got %q", p.PhaseLabel, owner, p.Proof)
	}
}

// TestStrings_FormatEstimatorWalkIsUnboundedTracked: estimateFormatAllocBytes
// reaches core.ValueDeepBytes through formatStringBytes for every non-String
// value, so the estimator itself runs the same unbounded tree walk - and it
// runs before the pre-charge, unguarded by it. The estimate is load-bearing and
// stays; what it needs is a row that stops claiming it is bounded.
func TestStrings_FormatEstimatorWalkIsUnboundedTracked(t *testing.T) {
	for _, fn := range []string{"estimateFormatAllocBytes", "estimateFormatValueBytes", "formatStringBytes"} {
		if len(sbWorkPhases(fn, "unbounded-tracked")) > 0 {
			return
		}
	}
	t.Fatalf("inventory.WorkPhases has no %s row Disposition %q for the format estimator (Func estimateFormatAllocBytes, estimateFormatValueBytes or formatStringBytes): its core.ValueDeepBytes walk runs before the pre-charge and is unrecorded",
		sbStringsFile, "unbounded-tracked")
}

// TestStrings_ToAnyRenderIsUnboundedTracked: toAny renders every non-scalar
// format argument through v.String() at strings.go:643, and that loop runs
// before fmt.Sprintf, so the render is materialized eagerly. What is unbounded
// there is the walk, not the escaping: the render visits a shared node once per
// path that reaches it while the ledger charged it once, exactly as toString's
// walk does, so the phase carries toString's owner.
//
// The %q expansion is a separate defect of the same site and not a second
// unboundedness: one byte becomes at most four, so the render stays a bounded
// multiple of a ledger-bounded quantity. What it breaks is the charge, which
// counts one byte per byte - measured, (list "<1048576 x 0x80>") estimates
// 1048648 bytes and renders 4194308 characters, a ratio of 3.9997.
func TestStrings_ToAnyRenderIsUnboundedTracked(t *testing.T) {
	const owner = "Owned by core-value-walk-sharing-bound."
	rows := sbWorkPhases("toAny", "unbounded-tracked")
	require.NotEmptyf(t, rows,
		"inventory.WorkPhases has no %s row Func %q Disposition %q: the eager render behind format's arguments is unrecorded, and the pre-charge does not bound it (measured render/estimate 3.9997 on a 1 MiB escaped leaf)",
		sbStringsFile, "toAny", "unbounded-tracked")
	for _, p := range rows {
		require.Zerof(t, p.MaxWork, "toAny/%s: an unbounded-tracked phase must state no MaxWork", p.PhaseLabel)
		require.Truef(t, strings.HasSuffix(p.Proof, owner), "toAny/%s: Proof must end with %q, got %q", p.PhaseLabel, owner, p.Proof)
	}
}

// TestStrings_LengthRuneScanIsBoundedException: []rune(s.V) converts the whole
// subject in one Go conversion with no point inside it where a Step could run,
// exactly as count's subject scan does. The bound comes from the subject: a
// core.String reaches the builtin only by already sitting in the allocation
// ledger.
func TestStrings_LengthRuneScanIsBoundedException(t *testing.T) {
	for _, p := range inventory.WorkPhases {
		if p.File != sbStringsFile || p.Fn != "string/length" {
			continue
		}
		require.Equalf(t, "bounded-exception", p.Disposition, "string/length/%s: disposition", p.PhaseLabel)
		require.Equalf(t, sbPrimitiveMaxWork, p.MaxWork, "string/length/%s: MaxWork", p.PhaseLabel)
		return
	}
	t.Fatalf("inventory.WorkPhases has no %s row Fn %q: the rune conversion at strings.go:225 is unrecorded", sbStringsFile, "string/length")
}

// TestStrings_PrimitiveScansAreBoundedExceptions: every builtin that hands its
// subject to an opaque Go string primitive owns a phase it cannot Step through.
// Stepping per byte would put a reduction charge on every string operation,
// which the design forbids, so each records the ledger-derived ceiling instead.
//
// What this certifies is the row: that inventory.WorkPhases carries one for the
// builtin, with the bounded-exception disposition and MaxWork
// sbPrimitiveMaxWork. What it does not certify is that the phase respects that
// ceiling. A phase whose output is a product of two ledger-sized operands - the
// separator string/join writes between every part, the replacement
// string/replace inserts between every rune - exceeds it while the row still
// reads exactly as asserted here. Those two are pinned behaviourally by
// runtime.TestStrings_JoinRejectsBeforeAllocating and
// runtime.TestStrings_ReplaceRejectsBeforeAllocating.
func TestStrings_PrimitiveScansAreBoundedExceptions(t *testing.T) {
	for _, fn := range []string{
		"string/split", "string/join", "string/replace",
		"string/trim", "string/upper", "string/lower",
		"string/contains?", "string/starts-with?", "string/ends-with?",
		"string->int", "string->float",
	} {
		t.Run(fn, func(t *testing.T) {
			for _, p := range inventory.WorkPhases {
				if p.File != sbStringsFile || p.Fn != fn || p.Disposition != "bounded-exception" {
					continue
				}
				require.Equalf(t, sbPrimitiveMaxWork, p.MaxWork,
					"%s/%s: a Go string primitive's ceiling is the allocation ledger's, %d", fn, p.PhaseLabel, sbPrimitiveMaxWork)
				return
			}
			t.Fatalf("inventory.WorkPhases has no %s row Fn %q Disposition %q: the opaque Go string scan it performs is unrecorded", sbStringsFile, fn, "bounded-exception")
		})
	}
}

// TestStrings_UnicodeAndEmptySeparatorUnchanged restates the goldens the sealed
// suites already pin for this family. It is green before the migration and must
// stay green after it: every value survives the budget work byte for byte.
// That is its whole purpose.
func TestStrings_UnicodeAndEmptySeparatorUnchanged(t *testing.T) {
	env := setupEnv(t)
	for _, tt := range []struct{ name, input, want string }{
		{"rune count multibyte", `(string/length "héllo")`, "5"},
		{"rune count cjk", `(string/length "日本語")`, "3"},
		{"rune count emoji", `(string/length "a😀b")`, "3"},
		{"empty separator splits runes", `(string/split "héllo" "")`, `("h" "é" "l" "l" "o")`},
		{"empty separator empty subject", `(string/split "" "")`, `()`},
		{"split no separator present", `(string/split "abc" ",")`, `("abc")`},
		{"upper multibyte", `(string/upper "héllo")`, `"HÉLLO"`},
		{"lower multibyte", `(string/lower "HÉLLO")`, `"héllo"`},
		{"join multibyte", `(string/join "，" (list "日" "本"))`, `"日，本"`},
		{"lines trailing newline", `(string/lines "a\nb\n")`, `("a" "b" "")`},
		{"str multibyte", `(str "日" "本")`, `"日本"`},
		{"format multibyte", `(format "%s-%s" "日" "本")`, `"日-本"`},
	} {
		t.Run(tt.name, func(t *testing.T) {
			require.Equalf(t, tt.want, eval(t, env, tt.input).String(), "%s value drift", tt.input)
		})
	}
}

// TestStrings_FormatEstimateTracksLiteralWidthRender pins the estimate to the
// render at Go's literal-width parse boundary. fmt scans a literal width digit
// by digit and abandons the whole verb the moment the running value passes 1e6,
// emitting %!(NOVERB) instead of the field: a width it refuses renders a few
// dozen bytes, a width it honours renders the field it asks for.
// parseFormatInt saturates at maxFormatEstimate instead, so a refused width is
// estimated eighteen orders of magnitude above what it materializes - and that
// estimate is the charge that overflows the allocation ledger.
//
// The running value is tested before each digit rather than after, so the edge
// is not 1e6: 10000009 is the last literal fmt honours and 10000010 the first
// it refuses. Both are rows here, because the mechanism this pins is that
// boundary and a table that straddles it without touching it holds while the
// ceiling moves. The honoured column is checked against fmt's own render rather
// than restated from the estimator, so a row cannot claim an edge fmt does not
// have.
//
// Dynamic widths are a different path: fmt takes them from an argument, not
// from parsenum, so they are not subject to this refusal and must keep the
// estimate they carry today.
func TestStrings_FormatEstimateTracksLiteralWidthRender(t *testing.T) {
	arg := core.Int{V: 1}
	args := []core.Value{arg}

	for _, tt := range []struct {
		name     string
		format   string
		honoured bool
	}{
		{"width at one million", "%1000000d", true},
		{"width above one million", "%10000001d", true},
		{"width at the last honoured literal", "%10000009d", true},
		{"width one past the last honoured literal", "%10000010d", false},
		{"width refused int32 ceiling", "%2147483647d", false},
		{"width refused int64 ceiling", "%9223372036854775807d", false},
		{"precision refused int64 ceiling", "%.9223372036854775807f", false},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := fmt.Sprintf(tt.format, toAny(arg))
			rendered := int64(len(out))
			honouredByFmt := !strings.Contains(out, "%!(NOVERB)")
			estimate := estimateFormatAllocBytes(tt.format, args)

			require.Equalf(t, tt.honoured, honouredByFmt,
				"fmt.Sprintf(%q) rendered %d bytes and honoured=%v: the row's honoured column is fmt's own parse answer, not the estimator's",
				tt.format, rendered, honouredByFmt)
			require.GreaterOrEqualf(t, estimate, rendered,
				"estimateFormatAllocBytes(%q) = %d against a render of %d bytes: the estimate must cover what fmt.Sprintf materializes",
				tt.format, estimate, rendered)
			if tt.honoured {
				return
			}

			verbOnly := "%" + tt.format[len(tt.format)-1:]
			ceiling := estimateFormatAllocBytes(verbOnly, args) + int64(len(tt.format))
			require.LessOrEqualf(t, estimate, ceiling,
				"estimateFormatAllocBytes(%q) = %d against a render of %d bytes and %d for %q with no width: fmt.Sprintf abandons a width it will not parse and emits %%!(NOVERB), so the estimate must mirror the refusal instead of saturating at %d",
				tt.format, estimate, rendered, ceiling, verbOnly, maxFormatEstimate)
		})
	}
}

// TestStrings_FormatEstimateTracksExplicitIndexRefusal pins the estimator and
// fmt to agreement on which argument a directive names. fmt refuses an
// explicit index past its parsenum ceiling, renders %!(BADINDEX) for the
// directive alone, and lets the following %s bind to the implicit cursor's
// argument - the 1MiB argument is never rendered. The estimator must refuse
// the index the same way, as a no-op: it must not wrap the decimal through
// index*10+d into an in-range argument and charge the unrendered 1MiB string
// for a render fmt materializes in a few dozen bytes.
func TestStrings_FormatEstimateTracksExplicitIndexRefusal(t *testing.T) {
	env := setupEnv(t)
	fn := formatGoFunc(t, env)

	format := "%[18446744073709551618]s%s"
	args := []core.Value{core.Int{V: 7}, core.String{V: strings.Repeat("x", 1<<20)}}
	rendered := fmt.Sprintf(format, toAny(args[0]), toAny(args[1]))
	require.Containsf(t, rendered, "BADINDEX",
		"render is %d bytes: the seeded index must be one fmt refuses, not one it honours", len(rendered))
	require.Lessf(t, len(rendered), 1<<20,
		"render is %d bytes: fmt refused the index, so the 1MiB argument must not be part of the render the estimate answers for", len(rendered))

	estimate := estimateFormatAllocBytes(format, args)
	require.GreaterOrEqualf(t, estimate, int64(len(rendered)),
		"estimateFormatAllocBytes(%q) = %d against a render of %d bytes: the estimate must cover what fmt.Sprintf materializes", format, estimate, len(rendered))
	refusedCeiling := core.MeterStringHeaderBytes + int64(len(rendered)) + int64(len(format))
	require.LessOrEqualf(t, estimate, refusedCeiling,
		"estimateFormatAllocBytes(%q) = %d against a render of %d bytes: fmt refused the explicit index and rendered nothing from the 1MiB argument, so the estimator must treat the refusal as a no-op too and not wrap the index into charging an argument the render never used", format, estimate, len(rendered))

	budget := int(core.StringShallowBytes(len(rendered)) - 1)
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	tightCtx := core.WithEvalResourceLimits(t.Context(), 1<<20, budget)
	_, err := fn.Fn(tightCtx, nil, append([]core.Value{core.String{V: format}}, args...), env)
	runtime.ReadMemStats(&after)
	requireResourceLimit(t, err)
	var lerr *core.LispicoError
	require.ErrorAs(t, err, &lerr)
	require.Equalf(t, fmt.Sprintf("allocation limit %d bytes exceeded", budget), lerr.Message,
		"the render is one byte over the budget, so the refusal must carry the allocation message, got %q", lerr.Message)
	// A real render here cannot exceed the 1MiB argument's materialization;
	// sub-kilobyte noise between the two ReadMemStats calls must not trip it.
	if got := after.TotalAlloc - before.TotalAlloc; got >= 1<<20 {
		t.Fatalf("format allocated %d bytes before refusing; a render materializing from the %d arguments must never have run", got, len(args))
	}

	generousCtx := core.WithEvalResourceLimits(t.Context(), 8<<20, 8<<20)
	got, err := fn.Fn(generousCtx, nil, append([]core.Value{core.String{V: format}}, args...), env)
	require.NoError(t, err)
	require.Equal(t, core.String{V: rendered}, got,
		"with the index refused the result must carry fmt's own render, %!(BADINDEX) text included")
	charged := core.EvalMeterFrom(generousCtx).Snapshot().AllocationBytes
	require.GreaterOrEqualf(t, charged, core.StringShallowBytes(len(rendered)),
		"charged %d bytes for a render of %d bytes: the settled charge must cover the render fmt actually produced", charged, len(rendered))
}

// TestStrings_ReplaceOutputBytesIsExact bounds string/replace's pre-charge from
// above as well as below. The sealed pre-charge tests only require the charge
// to be large enough to reject before strings.ReplaceAll runs, which a
// constant-factor over-bill satisfies too; a successful call must be billed the
// header plus the bytes its result actually owns, and no more.
func TestStrings_ReplaceOutputBytesIsExact(t *testing.T) {
	for _, tt := range []struct{ name, s, old, new string }{
		{"replacement grows", strings.Repeat("ab", 512), "a", "xyz"},
		{"replacement shrinks", strings.Repeat("abc", 512), "abc", "a"},
		{"replacement same width", strings.Repeat("ab", 512), "a", "z"},
		{"empty old inserts everywhere", strings.Repeat("x", 256), "", "yy"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			out := strings.ReplaceAll(tt.s, tt.old, tt.new)
			want := core.MeterStringHeaderBytes + int64(len(out))
			require.Equalf(t, want, replaceOutputBytes(tt.s, tt.old, tt.new),
				"replaceOutputBytes(%d bytes, %q, %q) must bill the string header plus the %d bytes strings.ReplaceAll writes",
				len(tt.s), tt.old, tt.new, len(out))
		})
	}
}

// sbJoinCharge charges string/join over parts empty parts and a separator of
// sepLen bytes, returning the ledger total the call cost. The parts are empty
// and their count is fixed, so the only quantity that moves between two calls
// is the separator the pre-charge sizes.
func sbJoinCharge(t *testing.T, env *core.Env, parts, sepLen int) int64 {
	t.Helper()
	fn := sbGoFunc(t, env, "string/join")
	ctx := core.WithEvalResourceLimits(t.Context(), 1<<24, 1<<24)
	_, err := fn.Fn(ctx, nil, []core.Value{
		core.String{V: strings.Repeat("s", sepLen)},
		core.NewList(sbStrings(parts, "")),
	}, env)
	require.NoError(t, err)
	return core.EvalMeterFrom(ctx).Snapshot().AllocationBytes
}

// TestStrings_JoinChargeIsExact is the same upper bound at the other
// product-shaped phase. strings.Join writes the separator between every part,
// so widening the separator moves the output by a known amount and the charge
// must move by exactly that - a pre-charge scaled by any constant factor moves
// by a multiple of it.
func TestStrings_JoinChargeIsExact(t *testing.T) {
	env := setupEnv(t)
	const parts = sbUnitCount

	narrow := sbJoinCharge(t, env, parts, 1)
	wide := sbJoinCharge(t, env, parts, sbWideLen)
	want := int64((parts - 1) * (sbWideLen - 1))

	require.Equalf(t, want, wide-narrow,
		"string/join charged %d against %d when the separator grew from 1 to %d bytes across %d parts: the output grows by %d bytes, and a pre-charge billed above that over-bills every successful call",
		wide, narrow, sbWideLen, parts, want)
}

// TestFormatEstimatorBoundsMismatchedVerbAndOperand pins the format
// pre-charge to fmt's own render for verbs the operand's type cannot
// satisfy. A mismatched directive falls through to fmt's
// %!verb(<type>=...) diagnostic, and the operand's rendered text is the
// part that grows with the input: a 1KiB core.String under %[1]d renders
// over a kilobyte of diagnostic while the mismatch arms estimate a fixed
// constant. Every expectation is derived from fmt.Sprintf itself, never
// restated from the estimator's arithmetic.
//
// The well-formed arms are regression anchors, not new demands: %s and %x
// over a core.String are already bounded and must survive the change
// untouched. The precision arm adds the task 1.3 invariant: a float verb
// carrying an explicit precision must never estimate below the same verb
// without one, because fmt renders the magnitude in full and the
// precision trims digits, not bytes already below it.
func TestFormatEstimatorBoundsMismatchedVerbAndOperand(t *testing.T) {
	type row struct {
		name   string
		format string
		args   []core.Value
	}
	estimateCoversRender := func(t *testing.T, format string, args []core.Value) {
		t.Helper()
		anyArgs := make([]any, len(args))
		for i, v := range args {
			anyArgs[i] = toAny(v)
		}
		render := fmt.Sprintf(format, anyArgs...)
		estimate := estimateFormatAllocBytes(format, args)
		require.GreaterOrEqualf(t, estimate, int64(len(render)),
			"%s produced %d bytes; the estimator must cover the render, got %d",
			format, len(render), estimate)
	}
	runRows := func(group string, rows []row) {
		t.Run(group, func(t *testing.T) {
			for _, tt := range rows {
				t.Run(tt.name, func(t *testing.T) {
					estimateCoversRender(t, tt.format, tt.args)
				})
			}
		})
	}
	// One explicit index per directive keeps every row independent of
	// arg-cursor bookkeeping.
	stringRows := func(verbs []string, operand core.Value, tag string) []row {
		rows := make([]row, 0, len(verbs))
		for _, verb := range verbs {
			format := "%[1]" + verb
			rows = append(rows, row{format + " over " + tag, format, []core.Value{operand}})
		}
		return rows
	}

	str1KiB := core.String{V: strings.Repeat("A", 1024)}
	str4KiB := core.String{V: strings.Repeat("A", 4096)}

	runRows("string-mismatch", append(
		stringRows([]string{"d", "b", "c", "o", "O", "U", "t"}, str1KiB, "1KiB string"),
		stringRows([]string{"d", "b", "c", "o", "O", "U", "t"}, str4KiB, "4KiB string")...))

	runRows("well-formed-anchor", []row{
		{"%[1]s over 1KiB string", "%[1]s", []core.Value{str1KiB}},
		{"%[1]x over 1KiB string", "%[1]x", []core.Value{str1KiB}},
	})

	runRows("bool-mismatch",
		stringRows([]string{"d", "b", "c", "o", "O", "U", "x", "X"}, core.Bool{V: true}, "bool"))

	runRows("float-mismatch",
		stringRows([]string{"d", "b", "c", "o", "O", "U", "t", "x", "X"}, core.Float{V: 1.5}, "float 1.5"))

	runRows("int-mismatch",
		stringRows([]string{"t", "e", "E", "f", "F", "g", "G"}, core.Int{V: 1}, "int 1"))

	runRows("repeated-directives", []row{
		{"four %[1]d over 1KiB string", "%[1]d%[1]d%[1]d%[1]d", []core.Value{str1KiB}},
		{"four %[1]c over 1KiB string", "%[1]c%[1]c%[1]c%[1]c", []core.Value{str1KiB}},
	})

	for _, large := range []struct {
		name string
		v    float64
	}{
		{"1e200", 1e200},
		{"1e308", 1e308},
	} {
		args := []core.Value{core.Float{V: large.v}}
		runRows("precision-large-magnitude/"+large.name, []row{
			{"%.2f", "%.2f", args},
			{"%.0f", "%.0f", args},
		})
		t.Run("precision-arm-at-or-above-bare/"+large.name, func(t *testing.T) {
			precEstimate := estimateFormatAllocBytes("%.2f", args)
			bareEstimate := estimateFormatAllocBytes("%f", args)
			require.LessOrEqualf(t, bareEstimate, precEstimate,
				"estimateFormatAllocBytes(\"%%.2f\") = %d over %v falls below the same verb with no precision at %d: a precision must not estimate below the no-precision arm",
				precEstimate, large.v, bareEstimate)
		})
	}
}
