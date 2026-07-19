package data

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/victorzhuk/go-lispico/core"
	"github.com/victorzhuk/go-lispico/plugins/stdlib"
)

func setupEnv(t *testing.T) *core.Env {
	t.Helper()
	env := core.NewEnv(nil)
	sp := stdlib.New()
	if err := sp.Init(env); err != nil {
		t.Fatalf("stdlib init: %v", err)
	}
	dp := New()
	if err := dp.Init(env); err != nil {
		t.Fatalf("data plugin init: %v", err)
	}
	return env
}

func eval(t *testing.T, env *core.Env, code string) core.Value {
	t.Helper()
	forms, err := core.Read(code)
	require.NoError(t, err, "read error")
	require.NotEmpty(t, forms, "empty input")

	evaluator := core.NewEvaluator()
	result, err := evaluator.Eval(context.Background(), forms[0], env)
	require.NoError(t, err, "eval error")
	return result
}

func evalErr(t *testing.T, env *core.Env, code string) error {
	t.Helper()
	forms, err := core.Read(code)
	if err != nil {
		return err
	}
	if len(forms) == 0 {
		t.Fatal("empty input")
	}
	evaluator := core.NewEvaluator()
	_, err = evaluator.Eval(context.Background(), forms[0], env)
	return err
}

func TestEncode(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"nil", "nil", "null"},
		{"bool true", "true", "true"},
		{"bool false", "false", "false"},
		{"int", "42", "42"},
		{"int negative", "-17", "-17"},
		{"float", "3.14", "3.14"},
		{"string", `"hello"`, `"hello"`},
		{"vector empty", "[]", "[]"},
		{"vector", "[1 2 3]", "[1,2,3]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result := eval(t, env, `(json/encode `+tt.input+`)`)
			str, ok := result.(core.String)
			require.True(t, ok, "expected String, got %T", result)
			assert.Equal(t, tt.expected, str.V)
		})
	}

	t.Run("string with escape", func(t *testing.T) {
		result := eval(t, env, `(json/encode "hello \"world\"")`)
		str, ok := result.(core.String)
		require.True(t, ok)
		assert.Contains(t, str.V, `hello`)
		assert.Contains(t, str.V, `world`)
	})

	t.Run("list empty", func(t *testing.T) {
		result := eval(t, env, `(json/encode (list))`)
		str, ok := result.(core.String)
		require.True(t, ok)
		assert.Equal(t, "[]", str.V)
	})

	t.Run("list", func(t *testing.T) {
		result := eval(t, env, `(json/encode (list 1 2 3))`)
		str, ok := result.(core.String)
		require.True(t, ok)
		assert.Equal(t, "[1,2,3]", str.V)
	})

	t.Run("map", func(t *testing.T) {
		result := eval(t, env, `(json/encode (hash-map :a 1))`)
		str, ok := result.(core.String)
		require.True(t, ok, "expected String, got %T", result)
		assert.Contains(t, str.V, `"a":1`)
	})

	t.Run("nested", func(t *testing.T) {
		result := eval(t, env, `(json/encode (hash-map :items [1 2 3] :inner (hash-map :x "y")))`)
		str, ok := result.(core.String)
		require.True(t, ok, "expected String, got %T", result)
		assert.Contains(t, str.V, `"items"`)
		assert.Contains(t, str.V, `"inner"`)
		assert.Contains(t, str.V, `"x":"y"`)
	})
}

func TestDecode(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name      string
		jsonInput string
		lispExpr  string
		expected  core.Value
	}{
		{"null", "null", "nil", core.Nil{}},
		{"bool true", "true", "true", core.Bool{V: true}},
		{"bool false", "false", "false", core.Bool{V: false}},
		{"int small", "42", "42", core.Int{V: 42}},
		{"int negative", "-17", "-17", core.Int{V: -17}},
		{"int zero", "0", "0", core.Int{V: 0}},
		{"float", "3.14", "3.14", core.Float{V: 3.14}},
		{"float negative", "-2.5", "-2.5", core.Float{V: -2.5}},
		{"array empty", "[]", "[]", core.Vector{Items: []core.Value{}}},
		{"object empty", "{}", "(hash-map)", core.NewHashMap()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			encoded := eval(t, env, `(json/encode `+tt.lispExpr+`)`)
			encStr := encoded.(core.String)
			result := eval(t, env, `(json/decode `+encStr.String()+`)`)
			assert.True(t, result.Equals(tt.expected), "expected %v, got %v", tt.expected, result)
		})
	}

	t.Run("string", func(t *testing.T) {
		result := eval(t, env, `(json/decode "\"hello\"")`)
		assert.True(t, result.Equals(core.String{V: "hello"}))
	})

	t.Run("string empty", func(t *testing.T) {
		result := eval(t, env, `(json/decode "\"\"")`)
		assert.True(t, result.Equals(core.String{V: ""}))
	})

	t.Run("int not float", func(t *testing.T) {
		t.Parallel()
		result := eval(t, env, `(json/decode "42")`)
		_, isInt := result.(core.Int)
		assert.True(t, isInt, "expected Int, got %T", result)
	})

	t.Run("array", func(t *testing.T) {
		t.Parallel()
		result := eval(t, env, `(json/decode "[1,2,3]")`)
		vec, ok := result.(core.Vector)
		require.True(t, ok, "expected Vector, got %T", result)
		require.Len(t, vec.Items, 3)
		assert.True(t, vec.Items[0].Equals(core.Int{V: 1}))
		assert.True(t, vec.Items[1].Equals(core.Int{V: 2}))
		assert.True(t, vec.Items[2].Equals(core.Int{V: 3}))
	})

	t.Run("object", func(t *testing.T) {
		t.Parallel()
		result := eval(t, env, `(json/decode "{\"a\":1}")`)
		m, ok := result.(*core.HashMap)
		require.True(t, ok, "expected HashMap, got %T", result)
		val, found := m.Get(core.Keyword{V: "a"})
		assert.True(t, found)
		assert.True(t, val.Equals(core.Int{V: 1}))
	})

	t.Run("nested", func(t *testing.T) {
		t.Parallel()
		result := eval(t, env, `(json/decode "{\"items\":[1,{\"x\":\"y\"}]}")`)
		m, ok := result.(*core.HashMap)
		require.True(t, ok, "expected HashMap, got %T", result)

		itemsVal, found := m.Get(core.Keyword{V: "items"})
		require.True(t, found)

		items, ok := itemsVal.(core.Vector)
		require.True(t, ok)
		require.Len(t, items.Items, 2)

		inner, ok := items.Items[1].(*core.HashMap)
		require.True(t, ok)
		xVal, found := inner.Get(core.Keyword{V: "x"})
		require.True(t, found)
		assert.True(t, xVal.Equals(core.String{V: "y"}))
	})
}

func TestRoundTrip(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name  string
		value string
	}{
		{"nil", "nil"},
		{"bool true", "true"},
		{"bool false", "false"},
		{"int", "42"},
		{"float", "3.14"},
		{"string", `"hello"`},
		{"vector empty", "[]"},
		{"vector", "[1 2 3]"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			original := eval(t, env, tt.value)
			encoded := eval(t, env, `(json/encode `+tt.value+`)`)
			str := encoded.(core.String)

			decoded := eval(t, env, `(json/decode `+str.String()+`)`)
			assert.True(t, original.Equals(decoded), "round-trip failed: %v != %v", original, decoded)
		})
	}

	t.Run("list becomes vector", func(t *testing.T) {
		encoded := eval(t, env, `(json/encode (list 1 2 3))`)
		str := encoded.(core.String)
		decoded := eval(t, env, `(json/decode `+str.String()+`)`)

		vec, ok := decoded.(core.Vector)
		require.True(t, ok, "expected Vector, got %T", decoded)
		require.Len(t, vec.Items, 3)
	})

	t.Run("map", func(t *testing.T) {
		original := eval(t, env, `(hash-map :a 1 :b 2)`)
		encoded := eval(t, env, `(json/encode (hash-map :a 1 :b 2))`)
		str := encoded.(core.String)

		decoded := eval(t, env, `(json/decode `+str.String()+`)`)
		origMap := original.(*core.HashMap)
		decMap := decoded.(*core.HashMap)

		assert.Equal(t, origMap.Len(), decMap.Len())
		origMap.Each(func(k, v core.Value) {
			decVal, found := decMap.Get(k)
			assert.True(t, found, "key %v not found in decoded map", k)
			assert.True(t, v.Equals(decVal), "value mismatch for key %v", k)
		})
	})

	t.Run("nested structures", func(t *testing.T) {
		original := eval(t, env, `(hash-map :items [1 2 3] :inner (hash-map :x "y"))`)
		encoded := eval(t, env, `(json/encode (hash-map :items [1 2 3] :inner (hash-map :x "y")))`)
		str := encoded.(core.String)

		decoded := eval(t, env, `(json/decode `+str.String()+`)`)

		origMap := original.(*core.HashMap)
		decMap := decoded.(*core.HashMap)
		assert.Equal(t, origMap.Len(), decMap.Len())
	})

	t.Run("int stays int after round-trip", func(t *testing.T) {
		encoded := eval(t, env, `(json/encode 42)`)
		str := encoded.(core.String)
		decoded := eval(t, env, `(json/decode `+str.String()+`)`)

		_, isInt := decoded.(core.Int)
		assert.True(t, isInt, "expected Int after round-trip, got %T", decoded)
	})
}

func TestDecodeIntegration(t *testing.T) {
	env := setupEnv(t)

	t.Run("get-in works on decoded JSON", func(t *testing.T) {
		decoded := eval(t, env, `(json/decode "{\"outer\":{\"inner\":42}}")`)
		env.Set("data", decoded)

		result := eval(t, env, `(get-in data (list :outer :inner))`)
		assert.True(t, result.Equals(core.Int{V: 42}), "expected 42, got %v", result)
	})

	t.Run("map? returns true for decoded objects", func(t *testing.T) {
		result := eval(t, env, `(map? (json/decode "{\"a\":1}"))`)
		assert.True(t, result.Equals(core.Bool{V: true}))
	})

	t.Run("vector? returns true for decoded arrays", func(t *testing.T) {
		result := eval(t, env, `(vector? (json/decode "[1,2,3]"))`)
		assert.True(t, result.Equals(core.Bool{V: true}))
	})

	t.Run("count works on decoded array", func(t *testing.T) {
		result := eval(t, env, `(count (json/decode "[1,2,3]"))`)
		assert.True(t, result.Equals(core.Int{V: 3}))
	})

	t.Run("get works on decoded object", func(t *testing.T) {
		result := eval(t, env, `(get (json/decode "{\"name\":\"test\"}") :name)`)
		assert.True(t, result.Equals(core.String{V: "test"}))
	})

	t.Run("map function works on decoded array", func(t *testing.T) {
		result := eval(t, env, `(map (fn [x] (+ x 1)) (json/decode "[1,2,3]"))`)
		list, ok := result.(core.List)
		require.True(t, ok)
		require.Len(t, list.Items, 3)
		assert.True(t, list.Items[0].Equals(core.Int{V: 2}))
	})
}

func TestPrettyEncode(t *testing.T) {
	env := setupEnv(t)

	t.Run("contains newlines", func(t *testing.T) {
		result := eval(t, env, `(json/pretty-encode (hash-map :a 1 :b 2))`)
		str, ok := result.(core.String)
		require.True(t, ok)
		assert.Contains(t, str.V, "\n")
	})

	t.Run("contains indentation", func(t *testing.T) {
		result := eval(t, env, `(json/pretty-encode (hash-map :items [1 2 3]))`)
		str, ok := result.(core.String)
		require.True(t, ok)
		assert.Contains(t, str.V, "  ")
	})

	t.Run("structure preserved", func(t *testing.T) {
		result := eval(t, env, `(json/pretty-encode (hash-map :a 1))`)
		str, ok := result.(core.String)
		require.True(t, ok)
		assert.Contains(t, str.V, `"a"`)
		assert.Contains(t, str.V, "1")
	})

	t.Run("nested structure", func(t *testing.T) {
		result := eval(t, env, `(json/pretty-encode (hash-map :outer (hash-map :inner 42)))`)
		str, ok := result.(core.String)
		require.True(t, ok)
		assert.Contains(t, str.V, `"outer"`)
		assert.Contains(t, str.V, `"inner"`)
		assert.Contains(t, str.V, "42")
	})
}

func TestErrors(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name        string
		input       string
		errContains string
	}{
		{
			name:        "bad JSON",
			input:       `(json/decode "not json")`,
			errContains: "json/decode:",
		},
		{
			name:        "decode wrong type",
			input:       `(json/decode 42)`,
			errContains: "requires string argument",
		},
		{
			name:        "encode arity 0",
			input:       `(json/encode)`,
			errContains: "requires 1 argument",
		},
		{
			name:        "decode arity 0",
			input:       `(json/decode)`,
			errContains: "requires 1 argument",
		},
		{
			name:        "decode arity 2",
			input:       `(json/decode "a" "b")`,
			errContains: "requires 1 argument",
		},
		{
			name:        "pretty-encode arity 0",
			input:       `(json/pretty-encode)`,
			errContains: "requires 1 argument",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := evalErr(t, env, tt.input)
			require.Error(t, err)
			assert.Contains(t, err.Error(), tt.errContains)
		})
	}

	t.Run("unencodable lambda", func(t *testing.T) {
		err := evalErr(t, env, `(json/encode (fn [x] x))`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "json/encode:")
	})

	t.Run("unencodable gofunc", func(t *testing.T) {
		err := evalErr(t, env, `(json/encode +)`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "json/encode:")
	})
}

func TestDecodeLargeIntegers(t *testing.T) {
	env := setupEnv(t)

	t.Run("large int within safe range", func(t *testing.T) {
		result := eval(t, env, `(json/decode "9007199254740991")`)
		_, isInt := result.(core.Int)
		assert.True(t, isInt, "expected Int for safe large integer, got %T", result)
	})

	t.Run("float for large int outside safe range", func(t *testing.T) {
		result := eval(t, env, `(json/decode "9007199254740992")`)
		_, isFloat := result.(core.Float)
		assert.True(t, isFloat, "expected Float for large integer, got %T", result)
	})
}

func TestDecodeMixedArray(t *testing.T) {
	env := setupEnv(t)

	result := eval(t, env, `(json/decode "[1, \"two\", true, null, [3, 4]]")`)
	vec, ok := result.(core.Vector)
	require.True(t, ok, "expected Vector, got %T", result)
	require.Len(t, vec.Items, 5)

	assert.True(t, vec.Items[0].Equals(core.Int{V: 1}))
	assert.True(t, vec.Items[1].Equals(core.String{V: "two"}))
	assert.True(t, vec.Items[2].Equals(core.Bool{V: true}))
	assert.True(t, vec.Items[3].Equals(core.Nil{}))

	innerVec, ok := vec.Items[4].(core.Vector)
	require.True(t, ok)
	require.Len(t, innerVec.Items, 2)
}

func TestPluginMetadata(t *testing.T) {
	p := New()

	t.Run("name", func(t *testing.T) {
		assert.Equal(t, "json", p.Name())
	})

	t.Run("metadata", func(t *testing.T) {
		meta := p.Metadata()
		assert.Equal(t, "1.0.0", meta.Version)
		assert.NotEmpty(t, meta.Description)
		assert.NotEmpty(t, meta.Author)
	})
}

func TestEncodeUnicode(t *testing.T) {
	env := setupEnv(t)

	tests := []struct {
		name     string
		input    string
		contains string
	}{
		{"unicode string", `(json/encode "日本語")`, "日本語"},
		{"emoji", `(json/encode "🎉")`, "🎉"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := eval(t, env, tt.input)
			str, ok := result.(core.String)
			require.True(t, ok)
			assert.Contains(t, str.V, tt.contains)
		})
	}
}

func TestDecodeUnicode(t *testing.T) {
	env := setupEnv(t)

	t.Run("unicode in string", func(t *testing.T) {
		encoded := eval(t, env, `(json/encode "日本語")`)
		encStr := encoded.(core.String)
		result := eval(t, env, `(json/decode `+encStr.String()+`)`)
		assert.True(t, result.Equals(core.String{V: "日本語"}))
	})

	t.Run("unicode in object key", func(t *testing.T) {
		result := eval(t, env, `(json/decode "{\"键\":\"值\"}")`)
		m, ok := result.(*core.HashMap)
		require.True(t, ok)
		val, found := m.Get(core.Keyword{V: "键"})
		assert.True(t, found)
		assert.True(t, val.Equals(core.String{V: "值"}))
	})
}

func TestEncodeSpecialStrings(t *testing.T) {
	env := setupEnv(t)

	t.Run("quotes roundtrip", func(t *testing.T) {
		encoded := eval(t, env, `(json/encode "say \"hello\"")`)
		encStr := encoded.(core.String)
		decoded := eval(t, env, `(json/decode `+encStr.String()+`)`)
		assert.True(t, decoded.Equals(core.String{V: `say "hello"`}))
	})

	t.Run("empty string", func(t *testing.T) {
		encoded := eval(t, env, `(json/encode "")`)
		encStr := encoded.(core.String)
		assert.Equal(t, `""`, encStr.V)
		decoded := eval(t, env, `(json/decode `+encStr.String()+`)`)
		assert.True(t, decoded.Equals(core.String{V: ""}))
	})

	t.Run("unicode in string roundtrip", func(t *testing.T) {
		encoded := eval(t, env, `(json/encode "hello 世界")`)
		encStr := encoded.(core.String)
		decoded := eval(t, env, `(json/decode `+encStr.String()+`)`)
		assert.True(t, decoded.Equals(core.String{V: "hello 世界"}))
	})
}

func TestPrettyEncodeErrors(t *testing.T) {
	env := setupEnv(t)

	t.Run("pretty-encode with unencodable value", func(t *testing.T) {
		err := evalErr(t, env, `(json/pretty-encode (fn [x] x))`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "json/pretty-encode:")
	})

	t.Run("pretty-encode arity 2", func(t *testing.T) {
		err := evalErr(t, env, `(json/pretty-encode 1 2)`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "requires 1 argument")
	})
}

func TestDecodeErrors(t *testing.T) {
	env := setupEnv(t)

	t.Run("decode truncated JSON", func(t *testing.T) {
		err := evalErr(t, env, `(json/decode "{")`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "json/decode:")
	})

	t.Run("decode invalid array", func(t *testing.T) {
		err := evalErr(t, env, `(json/decode "[1, 2,]")`)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "json/decode:")
	})
}

func TestEncodeWithLargeVector(t *testing.T) {
	env := setupEnv(t)

	result := eval(t, env, `(json/encode [1 2 3 4 5 6 7 8 9 10])`)
	str, ok := result.(core.String)
	require.True(t, ok)
	assert.Contains(t, str.V, "1")
	assert.Contains(t, str.V, "10")
}

func TestDecodeWithDeepNesting(t *testing.T) {
	env := setupEnv(t)

	result := eval(t, env, `(json/decode "{\"a\":{\"b\":{\"c\":1}}}")`)
	m, ok := result.(*core.HashMap)
	require.True(t, ok)

	bVal, found := m.Get(core.Keyword{V: "a"})
	require.True(t, found)
	bMap, ok := bVal.(*core.HashMap)
	require.True(t, ok)

	cVal, found := bMap.Get(core.Keyword{V: "b"})
	require.True(t, found)
	cMap, ok := cVal.(*core.HashMap)
	require.True(t, ok)

	innerVal, found := cMap.Get(core.Keyword{V: "c"})
	require.True(t, found)
	assert.True(t, innerVal.Equals(core.Int{V: 1}))
}

func TestDecodeHashMap_RoundTrip(t *testing.T) {
	t.Parallel()
	env := setupEnv(t)

	t.Run("map keys become keywords", func(t *testing.T) {
		result := eval(t, env, `(json/decode "{\"a\":1,\"b\":2}")`)
		m, ok := result.(*core.HashMap)
		require.True(t, ok)
		require.Equal(t, 2, m.Len())

		v, found := m.Get(core.Keyword{V: "a"})
		require.True(t, found)
		assert.True(t, v.Equals(core.Int{V: 1}))

		v, found = m.Get(core.Keyword{V: "b"})
		require.True(t, found)
		assert.True(t, v.Equals(core.Int{V: 2}))
	})

	t.Run("encode decode round-trips", func(t *testing.T) {
		original := eval(t, env, `(json/decode "{\"x\":10}")`)
		encoded := eval(t, env, `(json/encode (json/decode "{\"x\":10}"))`)
		str, ok := encoded.(core.String)
		require.True(t, ok)
		decoded := eval(t, env, `(json/decode `+str.String()+`)`)
		assert.True(t, original.Equals(decoded), "round-trip failed")
	})

	t.Run("whole number as Int", func(t *testing.T) {
		result := eval(t, env, `(json/decode "{\"v\":42}")`)
		m, ok := result.(*core.HashMap)
		require.True(t, ok)
		v, found := m.Get(core.Keyword{V: "v"})
		require.True(t, found)
		_, isInt := v.(core.Int)
		assert.True(t, isInt, "expected Int, got %T", v)
	})

	t.Run("deeply nested encode decode", func(t *testing.T) {
		result := eval(t, env, `(json/decode "{\"a\":{\"b\":{\"c\":[1,2]}}}")`)
		m, ok := result.(*core.HashMap)
		require.True(t, ok)
		aVal, found := m.Get(core.Keyword{V: "a"})
		require.True(t, found)
		aMap, ok := aVal.(*core.HashMap)
		require.True(t, ok)
		bVal, found := aMap.Get(core.Keyword{V: "b"})
		require.True(t, found)
		bMap, ok := bVal.(*core.HashMap)
		require.True(t, ok)
		cVal, found := bMap.Get(core.Keyword{V: "c"})
		require.True(t, found)
		cVec, ok := cVal.(core.Vector)
		require.True(t, ok, "expected Vector, got %T", cVal)
		require.Equal(t, 2, len(cVec.Items))
		assert.True(t, cVec.Items[0].Equals(core.Int{V: 1}))
		assert.True(t, cVec.Items[1].Equals(core.Int{V: 2}))
	})

	t.Run("more than 8 keys round-trips through the promoted map form", func(t *testing.T) {
		src := `{"k0":0,"k1":1,"k2":2,"k3":3,"k4":4,"k5":5,"k6":6,"k7":7,"k8":8,"k9":9,"k10":10,"k11":11}`
		result := eval(t, env, `(json/decode `+strconv.Quote(src)+`)`)
		m, ok := result.(*core.HashMap)
		require.True(t, ok)
		require.Equal(t, 12, m.Len())

		for i := range 12 {
			v, found := m.Get(core.Keyword{V: fmt.Sprintf("k%d", i)})
			require.True(t, found, "key k%d missing", i)
			assert.True(t, v.Equals(core.Int{V: int64(i)}), "k%d = %v, want %d", i, v, i)
		}

		encoded := eval(t, env, `(json/encode (json/decode `+strconv.Quote(src)+`))`)
		encStr, ok := encoded.(core.String)
		require.True(t, ok)
		decoded := eval(t, env, `(json/decode `+strconv.Quote(encStr.V)+`)`)
		assert.True(t, result.Equals(decoded), "round-trip through re-encode changed the map")
	})
}

func TestDecodeHashMap_Scaling(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping scaling test in short mode")
	}

	buildJSON := func(n int) string {
		var b strings.Builder
		b.WriteByte('{')
		for i := range n {
			if i > 0 {
				b.WriteByte(',')
			}
			fmt.Fprintf(&b, `"k%d":%d`, i, i)
		}
		b.WriteByte('}')
		return b.String()
	}

	timeDecode := func(jsonStr string) time.Duration {
		var raw any
		if err := json.Unmarshal([]byte(jsonStr), &raw); err != nil {
			t.Fatal(err)
		}
		start := time.Now()
		_, err := fromJSONValue(raw)
		if err != nil {
			t.Fatal(err)
		}
		return time.Since(start)
	}

	samples := 5
	var best2000, best4000 time.Duration

	for range samples {
		d := timeDecode(buildJSON(2000))
		if d < best2000 || best2000 == 0 {
			best2000 = d
		}
	}

	for range samples {
		d := timeDecode(buildJSON(4000))
		if d < best4000 || best4000 == 0 {
			best4000 = d
		}
	}

	ratio := float64(best4000) / float64(best2000)
	t.Logf("2000 keys: %v, 4000 keys: %v, ratio: %.2f (linear~2, quadratic~4)", best2000, best4000, ratio)

	require.Less(t, ratio, 3.0, "decode should scale sub-quadratically")
}

func TestDecodeHashMap_Immutability(t *testing.T) {
	t.Parallel()
	env := setupEnv(t)

	result := eval(t, env, `(json/decode "{\"a\":1,\"b\":2}")`)
	m, ok := result.(*core.HashMap)
	require.True(t, ok)
	require.Equal(t, 2, m.Len())

	t.Run("Assoc does not mutate original", func(t *testing.T) {
		_, err := m.Assoc(core.Keyword{V: "c"}, core.Int{V: 3})
		require.NoError(t, err)
		assert.Equal(t, 2, m.Len(), "original len unchanged after Assoc")
		_, found := m.Get(core.Keyword{V: "c"})
		assert.False(t, found, "original should not have new key from Assoc")
	})

	t.Run("Dissoc does not mutate original", func(t *testing.T) {
		_, err := m.Dissoc(core.Keyword{V: "a"})
		require.NoError(t, err)
		assert.Equal(t, 2, m.Len(), "original len unchanged after Dissoc")
		v, found := m.Get(core.Keyword{V: "a"})
		assert.True(t, found, "original should still have key after Dissoc")
		assert.True(t, v.Equals(core.Int{V: 1}))
	})
}
