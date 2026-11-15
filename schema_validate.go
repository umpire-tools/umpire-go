package umpire

import "fmt"

// ConditionDef declares a runtime condition's type.
type ConditionDef struct {
	Type        string
	Description string
}

// ValidatorEntry is a field-bound validator spec.
type ValidatorEntry struct {
	Op    *CheckOp
	Error string
	// HasError distinguishes an absent error message from an empty one.
	HasError bool
}

// FieldEntry pairs a field name with its definition, preserving schema order.
type FieldEntry struct {
	Name string
	Def  FieldDef
}

// Schema is a hydrated Umpire JSON schema.
type Schema struct {
	FieldOrder []string
	Fields     map[string]FieldDef
	Rules      []*Rule
	Validators map[string]ValidatorEntry
	Conditions map[string]ConditionDef
	Excluded   []*object
	Warnings   []string
}

// SchemaError describes an invalid schema.
type SchemaError struct{ Message string }

func (e *SchemaError) Error() string { return e.Message }

func schemaErr(format string, args ...any) error {
	return &SchemaError{Message: fmt.Sprintf(format, args...)}
}

// ParseSchema validates and hydrates a schema from JSON text.
func ParseSchema(data []byte) (*Schema, error) {
	raw, err := decodeOrdered(data)
	if err != nil {
		return nil, schemaErr("Invalid JSON: %s", err.Error())
	}
	return schemaFromRaw(raw)
}

// SchemaFromObject hydrates from an already-decoded value (an *object from
// ParseSchema's decoder, or a plain map for programmatic callers).
func SchemaFromObject(raw any) (*Schema, error) {
	return schemaFromRaw(raw)
}

func schemaFromRaw(raw any) (*Schema, error) {
	root, ok := asObject(raw)
	if !ok {
		return nil, schemaErr("[@umpire/json] Schema must be an object")
	}
	if err := validateSchemaObject(root); err != nil {
		return nil, err
	}
	return hydrateSchema(root)
}

// assertKnownMembers rejects unexpected object members.
func assertKnownMembers(o *object, allowed []string, context string) error {
	allowedSet := map[string]bool{}
	for _, k := range allowed {
		allowedSet[k] = true
	}
	for _, key := range o.Keys() {
		if !allowedSet[key] {
			return schemaErr(
				"[@umpire/json] %s has unknown member %q", context, key,
			)
		}
	}
	return nil
}

func assertString(v any, context string) error {
	if _, ok := v.(string); !ok {
		return schemaErr("[@umpire/json] %s must be a string", context)
	}
	return nil
}

func assertNumber(v any, context string) error {
	n, ok := v.(float64)
	if !ok || fmtIsNaNOrInf(n) {
		return schemaErr("[@umpire/json] %s must be a finite number", context)
	}
	return nil
}

func fmtIsNaNOrInf(f float64) bool {
	return f != f || f > 1.7976931348623157e308 || f < -1.7976931348623157e308
}

func assertPrimitive(v any, context string) error {
	if !isJSONPrimitive(v) {
		return schemaErr("[@umpire/json] %s must be a JSON primitive", context)
	}
	return nil
}

func assertPrimitiveArray(v any, context string) error {
	items, ok := v.([]any)
	if !ok {
		return schemaErr(
			"[@umpire/json] %s must be an array of JSON primitives", context,
		)
	}
	for _, item := range items {
		if !isJSONPrimitive(item) {
			return schemaErr(
				"[@umpire/json] %s must be an array of JSON primitives", context,
			)
		}
	}
	return nil
}

func assertStringArray(v any, context string) error {
	items, ok := v.([]any)
	if !ok {
		return schemaErr("[@umpire/json] %s must be an array of strings", context)
	}
	for _, item := range items {
		if _, ok := item.(string); !ok {
			return schemaErr(
				"[@umpire/json] %s must be an array of strings", context,
			)
		}
	}
	return nil
}

// assertMembershipField cross-checks a membership op against its field's
// declared emptiness. Membership is defined over scalars for in/notIn/
// fieldInCond and over arrays for contains, so a contradicting field
// definition is a schema error rather than a predicate that can never hold.
// Undeclared fields are reported by the rule walk that owns field-existence
// checks; this reports only the definition mismatch.
func assertMembershipField(op, field string, fields map[string]FieldDef) error {
	def, declared := fields[field]
	if !declared {
		return nil
	}
	switch op {
	case "in", "notIn", "fieldInCond":
		if def.IsEmpty != EmptyArray {
			return nil
		}
		return schemaErr(
			"[@umpire/json] Expression %q requires a scalar field, but field %q is declared \"isEmpty\": \"array\"",
			op, field,
		)
	case "contains":
		switch def.IsEmpty {
		case EmptyString, EmptyNumber, EmptyBoolean, EmptyObject:
			return schemaErr(
				"[@umpire/json] Expression %q requires an array field, but field %q is declared \"isEmpty\": %q",
				op, field, string(def.IsEmpty),
			)
		}
	}
	return nil
}

var conditionTypes = map[string]bool{
	"boolean": true, "string": true, "number": true,
	"string[]": true, "number[]": true,
}

var isEmptyStrategies = map[string]bool{
	"present": true, "string": true, "number": true,
	"boolean": true, "array": true, "object": true,
}

func validateCondition(name string, def any) error {
	o, ok := asObject(def)
	if !ok {
		return schemaErr(
			"[@umpire/json] condition %q definition must be an object", name,
		)
	}
	if err := assertKnownMembers(o, []string{"type", "description"}, "condition definition"); err != nil {
		return err
	}
	typeValue, _ := o.get("type")
	typeStr, _ := typeValue.(string)
	if !conditionTypes[typeStr] {
		return schemaErr(
			"[@umpire/json] condition %q must use a supported type", name,
		)
	}
	if desc, exists := o.get("description"); exists {
		if _, ok := desc.(string); !ok {
			return schemaErr(
				"[@umpire/json] condition %q description must be a string when provided", name,
			)
		}
	}
	return nil
}

func validateFieldDef(name string, def any) error {
	o, ok := asObject(def)
	if !ok {
		return schemaErr(
			"[@umpire/json] Field %q definition must be an object", name,
		)
	}
	if err := assertKnownMembers(o, []string{"required", "default", "isEmpty"}, "field definition"); err != nil {
		return err
	}
	if required, exists := o.get("required"); exists {
		if _, ok := required.(bool); !ok {
			return schemaErr(
				"[@umpire/json] Field %q required must be a boolean when provided", name,
			)
		}
	}
	if defaultValue, exists := o.get("default"); exists {
		if !isJSONPrimitive(defaultValue) {
			return schemaErr(
				"[@umpire/json] Field %q has a non-serializable default value", name,
			)
		}
	}
	if strategy, exists := o.get("isEmpty"); exists {
		s, _ := strategy.(string)
		if !isEmptyStrategies[s] {
			return schemaErr(
				"[@umpire/json] Unknown isEmpty strategy %q", strategy,
			)
		}
	}
	return nil
}

// validatorSpecMembers lists allowed spec members per op.
func validatorSpecMembers(op any) []string {
	switch op {
	case "email", "url", "integer":
		return []string{"op"}
	case "matches":
		return []string{"op", "pattern"}
	case "minLength", "maxLength", "min", "max":
		return []string{"op", "value"}
	case "range":
		return []string{"op", "min", "max"}
	default:
		return []string{"op"}
	}
}

func validateValidatorSpecMembers(spec *object, additionalMembers []string) error {
	allowed := append(validatorSpecMembers(spec.val("op")), additionalMembers...)
	if err := assertKnownMembers(spec, allowed, "Validator spec"); err != nil {
		return err
	}

	opValue, _ := spec.get("op")
	switch opValue {
	case "email", "url", "integer":
	case "matches":
		if err := assertString(spec.val("pattern"), `Validator "matches" pattern`); err != nil {
			return err
		}
	case "minLength", "maxLength", "min", "max":
		if err := assertNumber(
			spec.val("value"), fmt.Sprintf("Validator %q value", opValue),
		); err != nil {
			return err
		}
	case "range":
		if err := assertNumber(spec.val("min"), `Validator "range" min`); err != nil {
			return err
		}
		if err := assertNumber(spec.val("max"), `Validator "range" max`); err != nil {
			return err
		}
	}
	return assertValidValidatorSpec(spec)
}

func assertValidValidatorSpec(spec *object) error {
	opValue, _ := spec.get("op")
	switch opValue {
	case "email", "url", "integer":
		return nil
	case "matches":
		pattern, _ := spec.val("pattern").(string)
		if _, err := CheckOps.Matches(pattern); err != nil {
			return schemaErr(
				"[@umpire/json] Invalid regex pattern %q: %s",
				pattern, regexErrorDetail(pattern),
			)
		}
		return nil
	case "minLength", "maxLength", "min", "max":
		v, ok := spec.val("value").(float64)
		if !ok || fmtIsNaNOrInf(v) {
			return schemaErr(
				"[@umpire/json] Validator %q requires a numeric value", opValue,
			)
		}
		return nil
	case "range":
		min, minOk := spec.val("min").(float64)
		max, maxOk := spec.val("max").(float64)
		if !minOk || !maxOk || fmtIsNaNOrInf(min) || fmtIsNaNOrInf(max) {
			return schemaErr(
				"[@umpire/json] Validator \"range\" requires numeric min and max values",
			)
		}
		return nil
	default:
		return schemaErr(
			"[@umpire/json] Unknown validator op %q", spec.val("op"),
		)
	}
}

func validateValidator(field string, validator any, fieldNames map[string]bool) error {
	if !fieldNames[field] {
		return schemaErr(
			"[@umpire/json] Validator references unknown field %q (validator)", field,
		)
	}
	o, ok := asObject(validator)
	if !ok {
		return schemaErr(
			"[@umpire/json] Validator for field %q must be an object", field,
		)
	}
	if err := validateValidatorSpecMembers(o, []string{"error"}); err != nil {
		return err
	}
	if errValue, exists := o.get("error"); exists {
		if _, ok := errValue.(string); !ok {
			return schemaErr(
				"[@umpire/json] Validator for field %q must use a string error when provided", field,
			)
		}
	}
	return nil
}

func validateExcludedRule(rule any) error {
	o, ok := asObject(rule)
	if !ok {
		return schemaErr("[@umpire/json] Excluded rule must be an object")
	}
	if err := assertKnownMembers(
		o, []string{"type", "field", "description", "key", "signature"}, "Excluded rule",
	); err != nil {
		return err
	}
	typeValue, _ := o.get("type")
	typeStr, _ := typeValue.(string)
	if typeStr == "" {
		return schemaErr(
			"[@umpire/json] Excluded rules must include a non-empty string type",
		)
	}
	if fieldValue, exists := o.get("field"); exists {
		if _, ok := fieldValue.(string); !ok {
			return schemaErr(
				"[@umpire/json] Excluded rule field must be a string when provided",
			)
		}
	}
	descValue, _ := o.get("description")
	descStr, _ := descValue.(string)
	if descStr == "" {
		return schemaErr(
			"[@umpire/json] Excluded rules must include a non-empty string description",
		)
	}
	if keyValue, exists := o.get("key"); exists {
		if _, ok := keyValue.(string); !ok {
			return schemaErr(
				"[@umpire/json] Excluded rule key must be a string when provided",
			)
		}
	}
	if sigValue, exists := o.get("signature"); exists {
		if _, ok := sigValue.(string); !ok {
			return schemaErr(
				"[@umpire/json] Excluded rule signature must be a string when provided",
			)
		}
	}
	return nil
}

func validateExpression(raw any, fields map[string]FieldDef) error {
	o, ok := asObject(raw)
	if !ok {
		return schemaErr("[@umpire/json] Expression must be an object")
	}
	op, _ := o.string("op")

	switch op {
	case "eq", "neq":
		if err := assertKnownMembers(o, []string{"op", "field", "value"}, "Expression"); err != nil {
			return err
		}
		if err := assertString(o.val("field"), fmt.Sprintf("Expression %q field", op)); err != nil {
			return err
		}
		return assertPrimitive(o.val("value"), fmt.Sprintf("Expression %q value", op))
	case "gt", "gte", "lt", "lte":
		if err := assertKnownMembers(o, []string{"op", "field", "value"}, "Expression"); err != nil {
			return err
		}
		if err := assertString(o.val("field"), fmt.Sprintf("Expression %q field", op)); err != nil {
			return err
		}
		return assertNumber(o.val("value"), fmt.Sprintf("Expression %q value", op))
	case "present", "absent", "truthy", "falsy":
		if err := assertKnownMembers(o, []string{"op", "field"}, "Expression"); err != nil {
			return err
		}
		return assertString(o.val("field"), fmt.Sprintf("Expression %q field", op))
	case "in", "notIn":
		if err := assertKnownMembers(o, []string{"op", "field", "values"}, "Expression"); err != nil {
			return err
		}
		if err := assertString(o.val("field"), fmt.Sprintf("Expression %q field", op)); err != nil {
			return err
		}
		if err := assertPrimitiveArray(o.val("values"), fmt.Sprintf("Expression %q values", op)); err != nil {
			return err
		}
		return assertMembershipField(op, o.str("field"), fields)
	case "contains":
		if err := assertKnownMembers(o, []string{"op", "field", "value"}, "Expression"); err != nil {
			return err
		}
		if err := assertString(o.val("field"), `Expression "contains" field`); err != nil {
			return err
		}
		if _, exists := o.get("value"); !exists {
			return schemaErr(`[@umpire/json] Expression "contains" requires a value`)
		}
		if err := assertPrimitive(o.val("value"), `Expression "contains" value`); err != nil {
			return err
		}
		return assertMembershipField(op, o.str("field"), fields)
	case "cond":
		if err := assertKnownMembers(o, []string{"op", "condition"}, "Expression"); err != nil {
			return err
		}
		return assertString(o.val("condition"), `Expression "cond" condition`)
	case "condEq":
		if err := assertKnownMembers(o, []string{"op", "condition", "value"}, "Expression"); err != nil {
			return err
		}
		if err := assertString(o.val("condition"), `Expression "condEq" condition`); err != nil {
			return err
		}
		return assertPrimitive(o.val("value"), `Expression "condEq" value`)
	case "condIn":
		if err := assertKnownMembers(o, []string{"op", "condition", "values"}, "Expression"); err != nil {
			return err
		}
		if err := assertString(o.val("condition"), `Expression "condIn" condition`); err != nil {
			return err
		}
		return assertPrimitiveArray(o.val("values"), `Expression "condIn" values`)
	case "fieldInCond":
		if err := assertKnownMembers(o, []string{"op", "field", "condition"}, "Expression"); err != nil {
			return err
		}
		if err := assertString(o.val("field"), `Expression "fieldInCond" field`); err != nil {
			return err
		}
		if err := assertString(o.val("condition"), `Expression "fieldInCond" condition`); err != nil {
			return err
		}
		return assertMembershipField(op, o.str("field"), fields)
	case "and", "or":
		if err := assertKnownMembers(o, []string{"op", "exprs"}, "Expression"); err != nil {
			return err
		}
		exprs, ok := o.val("exprs").([]any)
		if !ok {
			return schemaErr(
				"[@umpire/json] Expression %q exprs must be an array", op,
			)
		}
		for _, entry := range exprs {
			if err := validateExpression(entry, fields); err != nil {
				return err
			}
		}
		return nil
	case "not":
		if err := assertKnownMembers(o, []string{"op", "expr"}, "Expression"); err != nil {
			return err
		}
		return validateExpression(o.val("expr"), fields)
	case "check":
		if err := assertKnownMembers(o, []string{"op", "field", "check"}, "Expression"); err != nil {
			return err
		}
		if err := assertString(o.val("field"), `Expression "check" field`); err != nil {
			return err
		}
		checkObj, ok := asObject(o.val("check"))
		if !ok {
			return schemaErr("[@umpire/json] Validator spec must be an object")
		}
		return validateValidatorSpecMembers(checkObj, nil)
	default:
		return schemaErr(
			"[@umpire/json] Unknown expression op %q", o.val("op"),
		)
	}
}
