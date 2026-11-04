package umpire

import (
	"fmt"
	"math"
	"net/url"
	"regexp"
	"strings"
)

// CheckOp is a named validation check with metadata for introspection.
type CheckOp struct {
	Name     string
	Params   map[string]any
	Validate func(value any) bool
	Reason   string
}

// emailLocalRe is the RE2-safe remainder of the @umpire/json email pattern.
// The original lookahead guards (no leading dot, no "..") are checked in code
// because RE2 does not support lookaheads.
var emailLocalRe = regexp.MustCompile(
	`^([A-Za-z0-9_'+\-.]*)[A-Za-z0-9_+-]@([A-Za-z0-9][A-Za-z0-9\-]*\.)+[A-Za-z]{2,}$`,
)

func validEmail(value any) bool {
	s, ok := value.(string)
	if !ok || len(s) == 0 {
		return false
	}
	if strings.HasPrefix(s, ".") || strings.Contains(s, "..") {
		return false
	}
	return emailLocalRe.MatchString(s)
}

var urlSchemeRe = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9+.-]*:`)

func validURL(value any) bool {
	s, ok := value.(string)
	if !ok {
		return false
	}
	// Mirror WHATWG new URL(): a scheme is mandatory. Go's url.Parse accepts
	// schemeless input as a relative reference, so screen for a scheme first.
	if !urlSchemeRe.MatchString(s) {
		return false
	}
	u, err := url.Parse(s)
	if err != nil {
		return false
	}
	if u.Host == "" && u.Opaque == "" {
		// WHATWG rejects special schemes without a host (e.g. "http://").
		switch strings.ToLower(u.Scheme) {
		case "http", "https", "ws", "wss", "ftp", "file":
			return false
		case "":
			return false
		}
	}
	return true
}

// checkOps provides the named validators shared with the JSON contract.
var CheckOps = struct {
	Email     func() *CheckOp
	URL       func() *CheckOp
	Matches   func(pattern string) (*CheckOp, error)
	MinLength func(min float64) *CheckOp
	MaxLength func(max float64) *CheckOp
	Min       func(min float64) *CheckOp
	Max       func(max float64) *CheckOp
	Range     func(min, max float64) *CheckOp
	Integer   func() *CheckOp
}{
	Email: func() *CheckOp {
		return &CheckOp{
			Name:     "email",
			Reason:   "Must be a valid email address",
			Validate: validEmail,
		}
	},
	URL: func() *CheckOp {
		return &CheckOp{
			Name:     "url",
			Reason:   "Must be a valid URL",
			Validate: validURL,
		}
	},
	Matches: func(pattern string) (*CheckOp, error) {
		re, err := regexp.Compile(pattern)
		if err != nil {
			return nil, fmt.Errorf(
				"[@umpire/json] Invalid regex pattern %q: %s", pattern, err.Error(),
			)
		}
		return &CheckOp{
			Name:     "matches",
			Params:   map[string]any{"pattern": pattern},
			Reason:   "Must match the required format",
			Validate: func(value any) bool { return isString(value) && re.MatchString(value.(string)) },
		}, nil
	},
	MinLength: func(min float64) *CheckOp {
		return &CheckOp{
			Name:     "minLength",
			Params:   map[string]any{"value": min},
			Reason:   fmt.Sprintf("Must be at least %s characters", formatJSONNumber(min)),
			Validate: func(value any) bool { return lengthOf(value) >= min },
		}
	},
	MaxLength: func(max float64) *CheckOp {
		return &CheckOp{
			Name:     "maxLength",
			Params:   map[string]any{"value": max},
			Reason:   fmt.Sprintf("Must be %s characters or fewer", formatJSONNumber(max)),
			Validate: func(value any) bool { return isLengthLike(value) && lengthOf(value) <= max },
		}
	},
	Min: func(min float64) *CheckOp {
		return &CheckOp{
			Name:     "min",
			Params:   map[string]any{"value": min},
			Reason:   fmt.Sprintf("Must be at least %s", formatJSONNumber(min)),
			Validate: func(value any) bool { return isNumber(value) && value.(float64) >= min },
		}
	},
	Max: func(max float64) *CheckOp {
		return &CheckOp{
			Name:     "max",
			Params:   map[string]any{"value": max},
			Reason:   fmt.Sprintf("Must be %s or less", formatJSONNumber(max)),
			Validate: func(value any) bool { return isNumber(value) && value.(float64) <= max },
		}
	},
	Range: func(min, max float64) *CheckOp {
		return &CheckOp{
			Name:   "range",
			Params: map[string]any{"min": min, "max": max},
			Reason: fmt.Sprintf(
				"Must be between %s and %s", formatJSONNumber(min), formatJSONNumber(max),
			),
			Validate: func(value any) bool {
				return isNumber(value) && value.(float64) >= min && value.(float64) <= max
			},
		}
	},
	Integer: func() *CheckOp {
		return &CheckOp{
			Name:     "integer",
			Reason:   "Must be a whole number",
			Validate: func(value any) bool { return isNumber(value) && value.(float64) == math.Trunc(value.(float64)) },
		}
	},
}

func isString(v any) bool {
	_, ok := v.(string)
	return ok
}

func isLengthLike(v any) bool {
	switch v.(type) {
	case string, []any:
		return true
	default:
		return false
	}
}

func lengthOf(v any) float64 {
	switch val := v.(type) {
	case string:
		return float64(len(val))
	case []any:
		return float64(len(val))
	default:
		return -1
	}
}

// defaultValidatorMessage mirrors @umpire/json's defaultValidatorMessage.
func defaultValidatorMessage(op *CheckOp) string {
	if op.Reason != "" {
		return op.Reason
	}
	return "Invalid value"
}

func regexErrorDetail(pattern string) string {
	_, err := regexp.Compile(pattern)
	if err != nil {
		return err.Error()
	}
	return ""
}

// parseCheckOp builds a CheckOp from a flat spec object (op plus params).
func parseCheckOp(spec *object) (*CheckOp, error) {
	opName, _ := spec.string("op")
	switch opName {
	case "email":
		return CheckOps.Email(), nil
	case "url":
		return CheckOps.URL(), nil
	case "matches":
		pattern, err := spec.requireString("pattern")
		if err != nil {
			return nil, err
		}
		return CheckOps.Matches(pattern)
	case "minLength":
		v, err := spec.requireNumber("value")
		if err != nil {
			return nil, err
		}
		return CheckOps.MinLength(v), nil
	case "maxLength":
		v, err := spec.requireNumber("value")
		if err != nil {
			return nil, err
		}
		return CheckOps.MaxLength(v), nil
	case "min":
		v, err := spec.requireNumber("value")
		if err != nil {
			return nil, err
		}
		return CheckOps.Min(v), nil
	case "max":
		v, err := spec.requireNumber("value")
		if err != nil {
			return nil, err
		}
		return CheckOps.Max(v), nil
	case "range":
		min, err := spec.requireNumber("min")
		if err != nil {
			return nil, err
		}
		max, err := spec.requireNumber("max")
		if err != nil {
			return nil, err
		}
		return CheckOps.Range(min, max), nil
	case "integer":
		return CheckOps.Integer(), nil
	default:
		return nil, fmt.Errorf("unknown check op: %q", opName)
	}
}
