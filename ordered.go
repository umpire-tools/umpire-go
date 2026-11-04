package umpire

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"sort"
)

// object is a JSON object that remembers key order. The contract's
// conformance fixtures depend on object key order (branch names, rule
// ordering, reason collection), which Go's map type discards.
type object struct {
	keys []string
	vals map[string]any
}

func newObject() *object {
	return &object{vals: map[string]any{}}
}

func (o *object) set(key string, value any) {
	if _, exists := o.vals[key]; !exists {
		o.keys = append(o.keys, key)
	}
	o.vals[key] = value
}

func (o *object) get(key string) (any, bool) {
	v, ok := o.vals[key]
	return v, ok
}

func (o *object) has(key string) bool {
	_, ok := o.vals[key]
	return ok
}

// str returns the string at key, or "" when absent or not a string.
func (o *object) str(key string) string {
	s, _ := o.vals[key].(string)
	return s
}

// val returns the value at key, or nil when absent (single-value form).
func (o *object) val(key string) any {
	return o.vals[key]
}

// Keys returns the keys in document order.
func (o *object) Keys() []string {
	return o.keys
}

func (o *object) string(key string) (string, bool) {
	v, ok := o.vals[key].(string)
	return v, ok
}

func (o *object) requireString(key string) (string, error) {
	v, ok := o.vals[key].(string)
	if !ok {
		return "", fmt.Errorf("%q must be a string", key)
	}
	return v, nil
}

func (o *object) requireNumber(key string) (float64, error) {
	v, ok := o.vals[key].(float64)
	if !ok {
		return 0, fmt.Errorf("%q must be a number", key)
	}
	return v, nil
}

// asObject normalizes a decoded value into an *object. Plain Go maps are
// accepted for programmatic callers; their keys are sorted for determinism.
func asObject(v any) (*object, bool) {
	switch val := v.(type) {
	case *object:
		return val, true
	case map[string]any:
		o := newObject()
		keys := make([]string, 0, len(val))
		for k := range val {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			o.set(k, val[k])
		}
		return o, true
	default:
		return nil, false
	}
}

// objOrEmpty normalizes a decoded value into an *object, returning an empty
// one for non-objects. Accepts a two-value get() result directly.
func objOrEmpty(v any, _ ...bool) *object {
	if o, ok := asObject(v); ok {
		return o
	}
	return newObject()
}

func asString(v any, _ ...bool) string {
	s, _ := v.(string)
	return s
}

func asBool(v any, _ ...bool) bool {
	b, _ := v.(bool)
	return b
}

// asArray normalizes a decoded value into a []any.
func asArray(v any) ([]any, bool) {
	a, ok := v.([]any)
	return a, ok
}

// decodeOrdered decodes JSON text preserving object key order throughout.
func decodeOrdered(data []byte) (any, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	v, err := decodeOrderedValue(dec)
	if err != nil {
		return nil, err
	}
	// Ensure no trailing garbage.
	if _, err := dec.Token(); err != io.EOF {
		return nil, fmt.Errorf("invalid JSON: trailing data")
	}
	return v, nil
}

func decodeOrderedValue(dec *json.Decoder) (any, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	return decodeOrderedFromToken(dec, tok)
}

func decodeOrderedFromToken(dec *json.Decoder, tok json.Token) (any, error) {
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			o := newObject()
			for dec.More() {
				keyTok, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, ok := keyTok.(string)
				if !ok {
					return nil, fmt.Errorf("invalid JSON: object key is not a string")
				}
				v, err := decodeOrderedValue(dec)
				if err != nil {
					return nil, err
				}
				o.set(key, v)
			}
			if _, err := dec.Token(); err != nil { // closing }
				return nil, err
			}
			return o, nil
		case '[':
			a := []any{}
			for dec.More() {
				v, err := decodeOrderedValue(dec)
				if err != nil {
					return nil, err
				}
				a = append(a, v)
			}
			if _, err := dec.Token(); err != nil { // closing ]
				return nil, err
			}
			return a, nil
		default:
			return nil, fmt.Errorf("invalid JSON: unexpected delimiter %v", t)
		}
	case json.Number:
		f, err := t.Float64()
		if err != nil {
			return nil, err
		}
		return f, nil
	default:
		// string, bool, nil pass through
		return tok, nil
	}
}
