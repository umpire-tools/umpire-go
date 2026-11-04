package umpire

import (
	"math"
	"reflect"
)

// jsonEqual mirrors JavaScript strict equality (===) over JSON-decoded
// values: numbers compare numerically, strings/booleans by value, and
// containers never equal across types.
func jsonEqual(a, b any) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}

	switch av := a.(type) {
	case bool:
		bv, ok := b.(bool)
		return ok && av == bv
	case float64:
		bv, ok := b.(float64)
		return ok && av == bv
	case string:
		bv, ok := b.(string)
		return ok && av == bv
	default:
		if reflect.TypeOf(a) != reflect.TypeOf(b) {
			return false
		}
		return reflect.DeepEqual(a, b)
	}
}

// jsonContains mirrors Array.prototype.includes: strict equality against
// each element.
func jsonContains(items []any, needle any) bool {
	for _, item := range items {
		if jsonEqual(item, needle) {
			return true
		}
	}
	return false
}

// isTruthy mirrors JavaScript Boolean() coercion over JSON values.
// Empty arrays and objects are truthy in JavaScript, so they stay truthy here.
func isTruthy(v any) bool {
	switch val := v.(type) {
	case nil:
		return false
	case bool:
		return val
	case float64:
		return val != 0 && !math.IsNaN(val)
	case string:
		return len(val) > 0
	default:
		return true
	}
}

// isNumber reports whether v is a JSON number (bools are not numbers).
func isNumber(v any) bool {
	_, ok := v.(float64)
	return ok
}

// isJSONPrimitive reports whether v is a JSON primitive value.
func isJSONPrimitive(v any) bool {
	switch v.(type) {
	case nil, bool, float64, string:
		return true
	default:
		return false
	}
}

// formatJSONNumber renders a number the way JavaScript would interpolate
// it into a string: 8.0 becomes "8", 2.5 stays "2.5".
func formatJSONNumber(v float64) string {
	if v == math.Trunc(v) && !math.IsInf(v, 0) && math.Abs(v) < 1e15 {
		return trimFloat(v)
	}
	return trimFloat(v)
}

func trimFloat(v float64) string {
	s := strconvFormatFloat(v)
	return s
}
