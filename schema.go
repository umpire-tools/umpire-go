package umpire

// hydrateSchema builds a *Schema from an already-validated root object.
func hydrateSchema(root *object) (*Schema, error) {
	fieldsObj := objOrEmpty(root.val("fields"))

	conditions := map[string]ConditionDef{}
	if rawConditions, exists := root.get("conditions"); exists {
		conditionsObj := objOrEmpty(rawConditions)
		for _, name := range conditionsObj.Keys() {
			defObj := objOrEmpty(conditionsObj.val(name))
			typeValue := asString(defObj.val("type"))
			def := ConditionDef{Type: typeValue}
			if desc := asString(defObj.val("description")); desc != "" {
				def.Description = desc
			}
			conditions[name] = def
		}
	}

	fieldNames := map[string]bool{}
	var fieldOrder []string
	fields := map[string]FieldDef{}
	for _, name := range fieldsObj.Keys() {
		fieldNames[name] = true
		fieldOrder = append(fieldOrder, name)
		fields[name] = fieldDefFromObject(objOrEmpty(fieldsObj.val(name)))
	}

	var warnings []string
	var excluded []*object
	if rawExcluded, exists := root.get("excluded"); exists {
		excludedList, _ := rawExcluded.([]any)
		for _, entry := range excludedList {
			entryObj := objOrEmpty(entry)
			excluded = append(excluded, entryObj)
			ruleType := asString(entryObj.val("type"))
			description := asString(entryObj.val("description"))
			warnings = append(warnings,
				"Excluded rule "+ruleType+": "+description)
		}
	}

	var rules []*Rule
	if rawRules, exists := root.get("rules"); exists {
		rulesList, _ := rawRules.([]any)
		for _, rawRule := range rulesList {
			ruleObj := objOrEmpty(rawRule)
			rule, err := buildRule(ruleObj, fieldNames, conditions)
			if err != nil {
				return nil, err
			}
			rules = append(rules, rule)
		}
	}

	validators := map[string]ValidatorEntry{}
	if rawValidators, exists := root.get("validators"); exists {
		validatorsObj, _ := asObject(rawValidators)
		for _, field := range validatorsObj.Keys() {
			specObj := objOrEmpty(validatorsObj.val(field))
			op, err := parseCheckOp(specObj)
			if err != nil {
				return nil, err
			}
			entry := ValidatorEntry{Op: op}
			if errValue, exists := specObj.get("error"); exists {
				entry.Error = asString(errValue)
				entry.HasError = true
			}
			validators[field] = entry
		}
	}

	return &Schema{
		FieldOrder: fieldOrder,
		Fields:     fields,
		Rules:      rules,
		Validators: validators,
		Conditions: conditions,
		Excluded:   excluded,
		Warnings:   warnings,
	}, nil
}

// fieldDefFromObject hydrates one field definition object. Field definitions
// are shape-checked before hydration, so unvalidated input never reaches it.
func fieldDefFromObject(defObj *object) FieldDef {
	def := FieldDef{IsEmpty: EmptyPresent, Required: asBool(defObj.val("required"))}
	if defaultValue, exists := defObj.get("default"); exists {
		def.Default = defaultValue
	}
	if strategy := asString(defObj.val("isEmpty")); strategy != "" {
		def.IsEmpty = IsEmptyStrategy(strategy)
	}
	return def
}

func buildRule(ruleObj *object, fieldNames map[string]bool, conditions map[string]ConditionDef) (*Rule, error) {
	ruleType, _ := ruleObj.string("type")
	reasonValue, hasReason := ruleObj.get("reason")
	reason, _ := reasonValue.(string)

	switch ruleType {
	case "requires":
		field, _ := ruleObj.string("field")
		if ruleObj.has("dependency") {
			dependency, _ := ruleObj.string("dependency")
			return RequiresDependency(field, dependency, reason), nil
		}
		if ruleObj.has("dependencies") {
			depsRaw, _ := ruleObj.get("dependencies")
			deps, _ := depsRaw.([]any)
			var parsed []dep
			for _, d := range deps {
				if depName, ok := d.(string); ok {
					parsed = append(parsed, dep{field: depName})
					continue
				}
				expr, err := compileExpr(d, fieldNames, conditions)
				if err != nil {
					return nil, err
				}
				parsed = append(parsed, dep{expr: expr})
			}
			return &Rule{
				kind:       "requires",
				targets:    []string{field},
				sources:    depSources(parsed),
				constraint: "enabled",
				deps:       parsed,
				hasReason:  hasReason,
				reason:     reason,
			}, nil
		}
		expr, err := compileExpr(ruleObj.val("when"), fieldNames, conditions)
		if err != nil {
			return nil, err
		}
		return &Rule{
			kind:       "requires",
			targets:    []string{field},
			sources:    exprSources(expr),
			constraint: "enabled",
			deps:       []dep{{expr: expr}},
			hasReason:  hasReason,
			reason:     reason,
		}, nil

	case "enabledWhen":
		field, _ := ruleObj.string("field")
		expr, err := compileExpr(ruleObj.val("when"), fieldNames, conditions)
		if err != nil {
			return nil, err
		}
		return &Rule{
			kind:       "enabledWhen",
			targets:    []string{field},
			constraint: "enabled",
			when:       expr,
			hasReason:  hasReason,
			reason:     reason,
		}, nil

	case "fairWhen":
		field, _ := ruleObj.string("field")
		expr, err := compileExpr(ruleObj.val("when"), fieldNames, conditions)
		if err != nil {
			return nil, err
		}
		return &Rule{
			kind:       "fairWhen",
			targets:    []string{field},
			constraint: "fair",
			when:       expr,
			fairPred: FairPredicateFromPredicate(func(values map[string]any, conds map[string]any) (bool, error) {
				return expr.eval(values, conds)
			}),
			hasReason: hasReason,
			reason:    reason,
		}, nil

	case "disables":
		rawTargets, _ := ruleObj.get("targets")
		rawList, _ := rawTargets.([]any)
		targets := make([]string, 0, len(rawList))
		for _, t := range rawList {
			targets = append(targets, t.(string))
		}
		if ruleObj.has("source") {
			source, _ := ruleObj.string("source")
			return DisablesField(source, targets, reason), nil
		}
		expr, err := compileExpr(ruleObj.val("when"), fieldNames, conditions)
		if err != nil {
			return nil, err
		}
		return &Rule{
			kind:       "disables",
			targets:    targets,
			sources:    exprSources(expr),
			constraint: "enabled",
			when:       expr,
			hasReason:  hasReason,
			reason:     reason,
		}, nil

	case "oneOf":
		group, _ := ruleObj.string("group")
		rawBranches, _ := ruleObj.get("branches")
		branchesObj, _ := asObject(rawBranches)
		var branches []Branch
		for _, branchName := range branchesObj.Keys() {
			rawFields, _ := branchesObj.val(branchName).([]any)
			var branchFields []string
			for _, f := range rawFields {
				branchFields = append(branchFields, f.(string))
			}
			branches = append(branches, Branch{Name: branchName, Fields: branchFields})
		}
		return OneOf(group, branches, reason), nil

	case "anyOf":
		rawRules, _ := ruleObj.get("rules")
		rulesList, _ := rawRules.([]any)
		var inner []*Rule
		for _, raw := range rulesList {
			innerObj := objOrEmpty(raw)
			innerRule, err := buildRule(innerObj, fieldNames, conditions)
			if err != nil {
				return nil, err
			}
			inner = append(inner, innerRule)
		}
		return AnyOf(inner...), nil

	case "eitherOf":
		group, _ := ruleObj.string("group")
		rawBranches, _ := ruleObj.get("branches")
		branchesObj, _ := asObject(rawBranches)
		var branches []NamedRules
		for _, branchName := range branchesObj.Keys() {
			rawRules, _ := branchesObj.val(branchName).([]any)
			var branchRules []*Rule
			for _, raw := range rawRules {
				innerObj := objOrEmpty(raw)
				innerRule, err := buildRule(innerObj, fieldNames, conditions)
				if err != nil {
					return nil, err
				}
				branchRules = append(branchRules, innerRule)
			}
			branches = append(branches, NamedRules{Name: branchName, Rules: branchRules})
		}
		return EitherOf(group, branches), nil

	case "check":
		field, _ := ruleObj.string("field")
		op, err := parseCheckOp(ruleObj)
		if err != nil {
			return nil, err
		}
		return Check(field, op, reason), nil

	default:
		return nil, schemaErr(
			"[@umpire/json] Unknown rule type %q", ruleObj.val("type"),
		)
	}
}

func depSources(deps []dep) []string {
	var out []string
	for _, d := range deps {
		if d.field != "" {
			out = append(out, d.field)
		} else if d.expr.checkField != "" {
			out = append(out, d.expr.checkField)
		}
	}
	return out
}

func exprSources(expr *compiledExpr) []string {
	if expr.checkField != "" {
		return []string{expr.checkField}
	}
	return nil
}
