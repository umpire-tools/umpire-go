package umpire

import "math"

// IsEmptyStrategy names how a field decides its value is "empty".
type IsEmptyStrategy string

const (
	// EmptyPresent treats only null/absent values as empty (the default).
	EmptyPresent IsEmptyStrategy = "present"
	// EmptyString treats non-strings and empty strings as empty.
	EmptyString IsEmptyStrategy = "string"
	// EmptyNumber treats non-numbers and NaN as empty.
	EmptyNumber IsEmptyStrategy = "number"
	// EmptyBoolean treats non-booleans as empty.
	EmptyBoolean IsEmptyStrategy = "boolean"
	// EmptyArray treats non-arrays and empty arrays as empty.
	EmptyArray IsEmptyStrategy = "array"
	// EmptyObject treats non-objects and empty objects as empty.
	EmptyObject IsEmptyStrategy = "object"
)

// FieldDef defines a field's structure and emptiness semantics.
type FieldDef struct {
	Required bool
	Default  any
	IsEmpty  IsEmptyStrategy
}

// isEmpty mirrors the @umpire/core emptiness strategies over JSON-decoded
// values.
func isEmpty(strategy IsEmptyStrategy, value any) bool {
	switch strategy {
	case EmptyString:
		s, ok := value.(string)
		return !ok || len(s) == 0
	case EmptyNumber:
		n, ok := value.(float64)
		return !ok || math.IsNaN(n)
	case EmptyBoolean:
		_, ok := value.(bool)
		return !ok
	case EmptyArray:
		a, ok := value.([]any)
		return !ok || len(a) == 0
	case EmptyObject:
		if o, ok := asObject(value); ok {
			return len(o.keys) == 0
		}
		return true
	default:
		return value == nil
	}
}

// isSatisfied reports whether a value counts as present for a field.
func isSatisfied(value any, def FieldDef) bool {
	return !isEmpty(def.IsEmpty, value)
}
