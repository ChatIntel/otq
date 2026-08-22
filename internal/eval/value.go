// Package eval evaluates parsed queries (internal/parser AST) against
// flattened spans (internal/flatten).
package eval

import (
	"bytes"
	"encoding/json"
	"sort"
)

type Kind int

const (
	KNull Kind = iota
	KBool
	KNumber
	KString
	KArray
	KObject
)

// Value is otq's dynamic runtime representation of any JSON-shaped data —
// a span, a literal, a path result. KObject preserves field insertion order
// (via Keys) so JSON output can match the data model's documented field
// order / a project({...})'s field order, unlike Go's alphabetical
// map-marshal default.
type Value struct {
	Kind Kind
	B    bool
	N    float64
	S    string
	A    []Value
	Keys []string // KObject only: insertion order of O's keys
	O    map[string]Value
}

func Null() Value            { return Value{Kind: KNull} }
func Bool(b bool) Value      { return Value{Kind: KBool, B: b} }
func Number(n float64) Value { return Value{Kind: KNumber, N: n} }
func String(s string) Value  { return Value{Kind: KString, S: s} }
func Array(vs []Value) Value {
	if vs == nil {
		return Value{Kind: KNull}
	}
	return Value{Kind: KArray, A: vs}
}

// NewObject builds an empty ordered object; use Set to add fields in order.
func NewObject() Value {
	return Value{Kind: KObject, O: map[string]Value{}}
}

func (v *Value) Set(key string, val Value) {
	if _, exists := v.O[key]; !exists {
		v.Keys = append(v.Keys, key)
	}
	v.O[key] = val
}

// Get resolves a dotted path against v, returning KNull for any missing key
// at any depth. This is the single place otq's "absent field is null, not
// an error" rule lives — every consumer (comparison, strcall, sort_by,
// project) can call Get without a special-cased missing-field branch.
func (v Value) Get(segments []string) Value {
	cur := v
	for _, seg := range segments {
		if cur.Kind != KObject {
			return Null()
		}
		next, ok := cur.O[seg]
		if !ok {
			return Null()
		}
		cur = next
	}
	return cur
}

// Equal implements otq's `==`/`!=` semantics: structural equality, comparing
// across kinds (e.g. number vs string) is simply false/not-equal, never an
// error — consistent with the leniency of null-propagating path access.
func Equal(a, b Value) bool {
	if a.Kind != b.Kind {
		return false
	}
	switch a.Kind {
	case KNull:
		return true
	case KBool:
		return a.B == b.B
	case KNumber:
		return a.N == b.N
	case KString:
		return a.S == b.S
	case KArray:
		if len(a.A) != len(b.A) {
			return false
		}
		for i := range a.A {
			if !Equal(a.A[i], b.A[i]) {
				return false
			}
		}
		return true
	case KObject:
		if len(a.Keys) != len(b.Keys) {
			return false
		}
		for _, k := range a.Keys {
			bv, ok := b.O[k]
			if !ok || !Equal(a.O[k], bv) {
				return false
			}
		}
		return true
	}
	return false
}

// Less implements otq's `<`/`>`/`<=`/`>=` semantics: only meaningful for
// number-vs-number or string-vs-string (lexicographic); any other
// combination (including either side being null) is simply "not less" —
// ordering operators against a mismatched/absent value are a non-match,
// not a crash. Deliberate leniency, consistent with the rest of the query
// language never erroring on a type/shape mismatch alone.
func Less(a, b Value) bool {
	if a.Kind == KNumber && b.Kind == KNumber {
		return a.N < b.N
	}
	if a.Kind == KString && b.Kind == KString {
		return a.S < b.S
	}
	return false
}

// ToJSON converts a Value into the interface{} shape encoding/json expects,
// preserving KObject field order via orderedObject's custom MarshalJSON.
func (v Value) ToJSON() any {
	switch v.Kind {
	case KNull:
		return nil
	case KBool:
		return v.B
	case KNumber:
		return v.N
	case KString:
		return v.S
	case KArray:
		out := make([]any, len(v.A))
		for i, e := range v.A {
			out[i] = e.ToJSON()
		}
		return out
	case KObject:
		obj := orderedObject{}
		for _, k := range v.Keys {
			obj = append(obj, kv{Key: k, Val: v.O[k].ToJSON()})
		}
		return obj
	}
	return nil
}

// kv/orderedObject preserve JSON object key insertion order, which
// encoding/json's default map[string]any marshaling (alphabetical) does not.
type kv struct {
	Key string
	Val any
}
type orderedObject []kv

func (o orderedObject) MarshalJSON() ([]byte, error) {
	var buf bytes.Buffer
	buf.WriteByte('{')
	for i, e := range o {
		if i > 0 {
			buf.WriteByte(',')
		}
		keyBytes, err := json.Marshal(e.Key)
		if err != nil {
			return nil, err
		}
		buf.Write(keyBytes)
		buf.WriteByte(':')
		valBytes, err := json.Marshal(e.Val)
		if err != nil {
			return nil, err
		}
		buf.Write(valBytes)
	}
	buf.WriteByte('}')
	return buf.Bytes(), nil
}

// SortStableByLess is a small helper shared by the sort_by stage: stable
// sort of a []Value slice using an externally supplied less function so
// tie-breaking order (and therefore output determinism) matches input
// order, matching jq-like expectations.
func SortStableByLess(vs []Value, less func(i, j Value) bool) {
	sort.SliceStable(vs, func(i, j int) bool { return less(vs[i], vs[j]) })
}
