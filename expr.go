package umpire

import "fmt"

// compiledExpr is a DSL expression reduced to a predicate. checkField is set
// when the expression references exactly one field; the graph uses it for
// informational (non-ordering) edges.
type compiledExpr struct {
	pred       func(values, conditions map[string]any) (bool, error)
	checkField string
}

func (c *compiledExpr) eval(values, conditions map[string]any) (bool, error) {
	return c.pred(values, conditions)
}

func assertExprField(field, op string, fields map[string]FieldDef) error {
	if _, ok := fields[field]; !ok {
		return fmt.Errorf(
			"[@umpire/dsl] Unknown field %q in %q expression", field, op,
		)
	}
	return nil
}

func getConditionValue(condition string, conditions map[string]any) (any, error) {
	v, ok := conditions[condition]
	if !ok {
		return nil, fmt.Errorf(
			"[@umpire/dsl] Missing runtime condition %q", condition,
		)
	}
	return v, nil
}

// compileExpr compiles a JSON expression into a predicate, mirroring
// @umpire/dsl's compileExpr plus @umpire/json's check expressions.
func compileExpr(raw any, fields map[string]FieldDef, conditions map[string]ConditionDef) (*compiledExpr, error) {
	obj, ok := asObject(raw)
	if !ok {
		return nil, fmt.Errorf("[@umpire/dsl] Expression must be an object")
	}
	op, _ := obj.string("op")

	fieldPredicate, err := compileFieldPredicate(obj, op, fields)
	if err != nil {
		return nil, err
	}
	if fieldPredicate != nil {
		return fieldPredicate, nil
	}

	conditionPredicate, err := compileConditionPredicate(obj, op, conditions)
	if err != nil {
		return nil, err
	}
	if conditionPredicate != nil {
		return conditionPredicate, nil
	}

	switch op {
	case "fieldInCond":
		field, _ := obj.string("field")
		if err := assertExprField(field, op, fields); err != nil {
			return nil, err
		}
		condition, _ := obj.string("condition")
		def, declared := conditions[condition]
		if !declared {
			return nil, fmt.Errorf(
				"[@umpire/dsl] Unknown condition %q in %q expression", condition, op,
			)
		}
		if def.Type != "string[]" && def.Type != "number[]" {
			return nil, fmt.Errorf(
				"[@umpire/dsl] \"fieldInCond\" requires an array condition, but %q is %q",
				condition, def.Type,
			)
		}
		return &compiledExpr{
			pred: func(values, conds map[string]any) (bool, error) {
				cv, err := getConditionValue(condition, conds)
				if err != nil {
					return false, err
				}
				items, ok := cv.([]any)
				if !ok {
					return false, fmt.Errorf(
						"[@umpire/dsl] Runtime condition %q must be an array for \"fieldInCond\"",
						condition,
					)
				}
				return jsonContains(items, values[field]), nil
			},
			checkField: field,
		}, nil

	case "and", "or":
		rawExprs, _ := obj.get("exprs")
		exprs, ok := rawExprs.([]any)
		if !ok || len(exprs) == 0 {
			return nil, fmt.Errorf(
				"[@umpire/dsl] %q expression requires an exprs array", op,
			)
		}
		compiled := make([]*compiledExpr, 0, len(exprs))
		refs := map[string]bool{}
		for _, entry := range exprs {
			c, err := compileExpr(entry, fields, conditions)
			if err != nil {
				return nil, err
			}
			compiled = append(compiled, c)
			if c.checkField != "" {
				refs[c.checkField] = true
			}
		}
		return &compiledExpr{
			pred: func(values, conds map[string]any) (bool, error) {
				for _, c := range compiled {
					passed, err := c.eval(values, conds)
					if err != nil {
						return false, err
					}
					if op == "and" && !passed {
						return false, nil
					}
					if op == "or" && passed {
						return true, nil
					}
				}
				return op == "and", nil
			},
			checkField: singleRef(refs),
		}, nil

	case "not":
		rawInner, ok := obj.get("expr")
		if !ok {
			return nil, fmt.Errorf(
				"[@umpire/dsl] \"not\" expression requires an expr field",
			)
		}
		inner, err := compileExpr(rawInner, fields, conditions)
		if err != nil {
			return nil, err
		}
		return &compiledExpr{
			pred: func(values, conds map[string]any) (bool, error) {
				passed, err := inner.eval(values, conds)
				if err != nil {
					return false, err
				}
				return !passed, nil
			},
			checkField: inner.checkField,
		}, nil

	case "check":
		field, _ := obj.string("field")
		if err := assertExprField(field, op, fields); err != nil {
			return nil, err
		}
		checkObj := objOrEmpty(obj.val("check"))
		checkOp, err := parseCheckOp(checkObj)
		if err != nil {
			return nil, err
		}
		return &compiledExpr{
			pred: func(values, _ map[string]any) (bool, error) {
				value := values[field]
				if value == nil {
					return false, nil
				}
				return checkOp.Validate(value), nil
			},
			checkField: field,
		}, nil

	default:
		return nil, fmt.Errorf(
			"[@umpire/dsl] Unknown expression op %q", op,
		)
	}
}

// compileFieldPredicate handles the field-bound ops; returns nil when the op
// is not field-bound.
func compileFieldPredicate(obj *object, op string, fields map[string]FieldDef) (*compiledExpr, error) {
	switch op {
	case "eq", "neq":
		field, _ := obj.string("field")
		if err := assertExprField(field, op, fields); err != nil {
			return nil, err
		}
		value, _ := obj.get("value")
		return &compiledExpr{
			pred: func(values, _ map[string]any) (bool, error) {
				equal := jsonEqual(values[field], value)
				if op == "eq" {
					return equal, nil
				}
				return !equal, nil
			},
			checkField: field,
		}, nil
	case "gt", "gte", "lt", "lte":
		field, _ := obj.string("field")
		if err := assertExprField(field, op, fields); err != nil {
			return nil, err
		}
		value, _ := obj.get("value")
		bound, _ := value.(float64)
		return &compiledExpr{
			pred: func(values, _ map[string]any) (bool, error) {
				v, ok := values[field].(float64)
				if !ok {
					return false, nil
				}
				switch op {
				case "gt":
					return v > bound, nil
				case "gte":
					return v >= bound, nil
				case "lt":
					return v < bound, nil
				default:
					return v <= bound, nil
				}
			},
			checkField: field,
		}, nil
	case "present":
		return fieldTest(obj, op, fields, func(v any) bool { return v != nil })
	case "absent":
		return fieldTest(obj, op, fields, func(v any) bool { return v == nil })
	case "truthy":
		return fieldTest(obj, op, fields, isTruthy)
	case "falsy":
		return fieldTest(obj, op, fields, func(v any) bool { return !isTruthy(v) })
	case "in", "notIn":
		field, _ := obj.string("field")
		if err := assertExprField(field, op, fields); err != nil {
			return nil, err
		}
		rawValues, _ := obj.get("values")
		items, ok := rawValues.([]any)
		if !ok {
			return nil, fmt.Errorf(
				"[@umpire/dsl] %q expression requires a values array", op,
			)
		}
		return &compiledExpr{
			pred: func(values, _ map[string]any) (bool, error) {
				contained := jsonContains(items, values[field])
				if op == "in" {
					return contained, nil
				}
				return !contained, nil
			},
			checkField: field,
		}, nil
	case "contains":
		field, _ := obj.string("field")
		if err := assertExprField(field, op, fields); err != nil {
			return nil, err
		}
		value, _ := obj.get("value")
		return &compiledExpr{
			pred: func(values, _ map[string]any) (bool, error) {
				items, ok := values[field].([]any)
				if !ok {
					return false, nil
				}
				return jsonContains(items, value), nil
			},
			checkField: field,
		}, nil
	default:
		return nil, nil
	}
}

func fieldTest(obj *object, op string, fields map[string]FieldDef, test func(any) bool) (*compiledExpr, error) {
	field, _ := obj.string("field")
	if err := assertExprField(field, op, fields); err != nil {
		return nil, err
	}
	return &compiledExpr{
		pred: func(values, _ map[string]any) (bool, error) {
			return test(values[field]), nil
		},
		checkField: field,
	}, nil
}

// compileConditionPredicate handles cond/condEq/condIn; returns nil when the
// op is not condition-bound.
func compileConditionPredicate(obj *object, op string, conditions map[string]ConditionDef) (*compiledExpr, error) {
	switch op {
	case "cond":
		condition, _ := obj.string("condition")
		if err := assertConditionDeclared(condition, op, conditions); err != nil {
			return nil, err
		}
		return &compiledExpr{
			pred: func(_, conds map[string]any) (bool, error) {
				v, err := getConditionValue(condition, conds)
				if err != nil {
					return false, err
				}
				return isTruthy(v), nil
			},
		}, nil
	case "condEq":
		condition, _ := obj.string("condition")
		if err := assertConditionDeclared(condition, op, conditions); err != nil {
			return nil, err
		}
		value, _ := obj.get("value")
		return &compiledExpr{
			pred: func(_, conds map[string]any) (bool, error) {
				v, err := getConditionValue(condition, conds)
				if err != nil {
					return false, err
				}
				return jsonEqual(v, value), nil
			},
		}, nil
	case "condIn":
		condition, _ := obj.string("condition")
		if err := assertConditionDeclared(condition, op, conditions); err != nil {
			return nil, err
		}
		rawValues, _ := obj.get("values")
		items, ok := rawValues.([]any)
		if !ok {
			return nil, fmt.Errorf(
				"[@umpire/dsl] %q expression requires a values array", op,
			)
		}
		return &compiledExpr{
			pred: func(_, conds map[string]any) (bool, error) {
				v, err := getConditionValue(condition, conds)
				if err != nil {
					return false, err
				}
				return jsonContains(items, v), nil
			},
		}, nil
	default:
		return nil, nil
	}
}

func assertConditionDeclared(condition, op string, conditions map[string]ConditionDef) error {
	if _, ok := conditions[condition]; !ok {
		return fmt.Errorf(
			"[@umpire/dsl] Unknown condition %q in %q expression", condition, op,
		)
	}
	return nil
}

func singleRef(refs map[string]bool) string {
	if len(refs) == 1 {
		for k := range refs {
			return k
		}
	}
	return ""
}
