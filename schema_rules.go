package umpire

import "fmt"

const ruleMembersContext = "Rule"

func assertRuleField(field string, fields map[string]FieldDef, context string) error {
	if _, ok := fields[field]; !ok {
		return schemaErr(
			"[@umpire/json] Rule %s references unknown field %q", context, field,
		)
	}
	return nil
}

// ruleConstraint reports the rule's constraint phase.
func ruleConstraint(rule *object) (string, error) {
	ruleType := rule.str("type")
	switch ruleType {
	case "fairWhen", "check":
		return "fair", nil
	case "anyOf":
		rawRules, _ := rule.get("rules")
		rules, _ := rawRules.([]*object)
		shape, shapeErr := resolveCompositeShape("anyOf()", objectsToAny(rules))
		if shapeErr != nil {
			return "", shapeErr
		}
		return shape.constraint, nil
	case "eitherOf":
		shape, err := resolveEitherOfShape(rule)
		if err != nil {
			return "", err
		}
		return shape.constraint, nil
	default:
		return "enabled", nil
	}
}

// ruleTargets lists a rule's target fields.
func ruleTargets(rule *object) ([]string, error) {
	ruleType := rule.str("type")
	switch ruleType {
	case "requires", "enabledWhen", "fairWhen", "check":
		field, _ := rule.string("field")
		return []string{field}, nil
	case "disables":
		rawTargets, _ := rule.get("targets")
		targets, _ := rawTargets.([]any)
		out := make([]string, 0, len(targets))
		for _, t := range targets {
			out = append(out, t.(string))
		}
		return out, nil
	case "oneOf":
		rawBranches, _ := rule.get("branches")
		branches, ok := asObject(rawBranches)
		if !ok {
			return nil, schemaErr(`[@umpire/json] Rule "oneOf" branches must be an object`)
		}
		seen := map[string]bool{}
		var out []string
		for _, branchName := range branches.Keys() {
			fields, _ := branches.val(branchName).([]any)
			for _, f := range fields {
				name := f.(string)
				if !seen[name] {
					seen[name] = true
					out = append(out, name)
				}
			}
		}
		return out, nil
	case "anyOf":
		rawRules, _ := rule.get("rules")
		rules, _ := rawRules.([]*object)
		shape, err := resolveCompositeShape("anyOf()", objectsToAny(rules))
		if err != nil {
			return nil, err
		}
		return shape.targets, nil
	case "eitherOf":
		shape, err := resolveEitherOfShape(rule)
		if err != nil {
			return nil, err
		}
		return shape.targets, nil
	default:
		return nil, schemaErr(
			"[@umpire/json] Unknown rule type %q", rule.val("type"),
		)
	}
}

type compositeShape struct {
	targets    []string
	constraint string
}

func objectsToAny(rules []*object) []any {
	out := make([]any, 0, len(rules))
	for _, r := range rules {
		out = append(out, r)
	}
	return out
}

func resolveCompositeShape(label string, rules []any) (compositeShape, error) {
	if len(rules) == 0 {
		return compositeShape{}, schemaErr(
			"[@umpire/json] %s requires at least one rule", label,
		)
	}

	first, _ := asObject(rules[0])
	firstTargets, err := ruleTargets(first)
	if err != nil {
		return compositeShape{}, err
	}
	expected := uniqueSorted(firstTargets)
	constraint, err := ruleConstraint(first)
	if err != nil {
		return compositeShape{}, err
	}

	for _, raw := range rules[1:] {
		rule, _ := asObject(raw)
		currentTargets, err := ruleTargets(rule)
		if err != nil {
			return compositeShape{}, err
		}
		current := uniqueSorted(currentTargets)
		if len(current) != len(expected) {
			return compositeShape{}, schemaErr(
				"[@umpire/json] %s rules must target the same fields", label,
			)
		}
		for i := range current {
			if current[i] != expected[i] {
				return compositeShape{}, schemaErr(
					"[@umpire/json] %s rules must target the same fields", label,
				)
			}
		}
		innerConstraint, err := ruleConstraint(rule)
		if err != nil {
			return compositeShape{}, err
		}
		if innerConstraint != constraint {
			return compositeShape{}, schemaErr(
				"[@umpire/json] %s cannot mix fairWhen rules with availability rules", label,
			)
		}
	}

	return compositeShape{targets: firstTargets, constraint: constraint}, nil
}

func uniqueSorted(fields []string) []string {
	seen := map[string]bool{}
	out := make([]string, 0, len(fields))
	for _, f := range fields {
		if !seen[f] {
			seen[f] = true
			out = append(out, f)
		}
	}
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

func resolveEitherOfShape(rule *object) (compositeShape, error) {
	group, _ := rule.string("group")
	rawBranches, _ := rule.get("branches")
	branches, ok := asObject(rawBranches)
	if !ok {
		return compositeShape{}, schemaErr(
			`[@umpire/json] Rule "eitherOf" branches must be an object`,
		)
	}
	if len(branches.Keys()) == 0 {
		return compositeShape{}, schemaErr(
			"[@umpire/json] eitherOf(%q) must include at least one branch", group,
		)
	}
	var all []any
	for _, branchName := range branches.Keys() {
		branchRules, ok := branches.val(branchName).([]any)
		if !ok {
			return compositeShape{}, schemaErr(
				"[@umpire/json] Rule \"eitherOf\" branch %q must be an array", branchName,
			)
		}
		if len(branchRules) == 0 {
			return compositeShape{}, schemaErr(
				"[@umpire/json] eitherOf(%q) branch %q must not be empty", group, branchName,
			)
		}
		all = append(all, branchRules...)
	}
	return resolveCompositeShape(
		fmt.Sprintf("eitherOf(%q)", group), all,
	)
}

func validateRuleMembers(rule *object) error {
	ruleType := rule.str("type")
	switch ruleType {
	case "requires":
		allowed := []string{"type", "field", "when", "reason"}
		if rule.has("dependency") {
			allowed = []string{"type", "field", "dependency", "reason"}
		} else if rule.has("dependencies") {
			allowed = []string{"type", "field", "dependencies", "reason"}
		}
		return assertKnownMembers(rule, allowed, ruleMembersContext)
	case "enabledWhen", "fairWhen":
		return assertKnownMembers(rule, []string{"type", "field", "when", "reason"}, ruleMembersContext)
	case "disables":
		allowed := []string{"type", "when", "targets", "reason"}
		if rule.has("source") {
			allowed = []string{"type", "source", "targets", "reason"}
		}
		return assertKnownMembers(rule, allowed, ruleMembersContext)
	case "oneOf", "eitherOf":
		return assertKnownMembers(rule, []string{"type", "group", "branches"}, ruleMembersContext)
	case "anyOf":
		return assertKnownMembers(rule, []string{"type", "rules"}, ruleMembersContext)
	case "check":
		allowed := append(
			[]string{"type", "field", "reason"},
			validatorSpecMembers(rule.val("op"))...,
		)
		return assertKnownMembers(rule, allowed, ruleMembersContext)
	default:
		return schemaErr(
			"[@umpire/json] Unknown rule type %q", rule.val("type"),
		)
	}
}

func validateRuleShape(rule *object, fields map[string]FieldDef) error {
	if err := validateRuleMembers(rule); err != nil {
		return err
	}
	if reason, exists := rule.get("reason"); exists {
		if err := assertString(reason, "Rule reason"); err != nil {
			return err
		}
	}

	ruleType := rule.str("type")
	switch ruleType {
	case "requires":
		if err := assertString(rule.val("field"), `Rule "requires" field`); err != nil {
			return err
		}
		if rule.has("dependency") {
			return assertString(rule.val("dependency"), `Rule "requires" dependency`)
		}
		if rule.has("dependencies") {
			deps, ok := rule.val("dependencies").([]any)
			if !ok {
				return schemaErr(
					`[@umpire/json] Rule "requires" dependencies must be an array`,
				)
			}
			for _, d := range deps {
				if _, isString := d.(string); isString {
					continue
				}
				if err := validateExpression(d, fields); err != nil {
					return err
				}
			}
			return nil
		}
		return validateExpression(rule.val("when"), fields)
	case "enabledWhen", "fairWhen":
		if err := assertString(
			rule.val("field"), fmt.Sprintf("Rule %q field", ruleType),
		); err != nil {
			return err
		}
		return validateExpression(rule.val("when"), fields)
	case "disables":
		if err := assertStringArray(rule.val("targets"), `Rule "disables" targets`); err != nil {
			return err
		}
		if rule.has("source") {
			return assertString(rule.val("source"), `Rule "disables" source`)
		}
		return validateExpression(rule.val("when"), fields)
	case "oneOf":
		if err := assertString(rule.val("group"), `Rule "oneOf" group`); err != nil {
			return err
		}
		rawBranches, _ := rule.get("branches")
		branches, ok := asObject(rawBranches)
		if !ok {
			return schemaErr(`[@umpire/json] Rule "oneOf" branches must be an object`)
		}
		for _, branchName := range branches.Keys() {
			if err := assertStringArray(
				branches.val(branchName),
				fmt.Sprintf("Rule \"oneOf\" branch %q", branchName),
			); err != nil {
				return err
			}
		}
		return nil
	case "eitherOf":
		if err := assertString(rule.val("group"), `Rule "eitherOf" group`); err != nil {
			return err
		}
		rawBranches, _ := rule.get("branches")
		branches, ok := asObject(rawBranches)
		if !ok {
			return schemaErr(`[@umpire/json] Rule "eitherOf" branches must be an object`)
		}
		for _, branchName := range branches.Keys() {
			if _, ok := branches.val(branchName).([]any); !ok {
				return schemaErr(
					"[@umpire/json] Rule \"eitherOf\" branch %q must be an array", branchName,
				)
			}
		}
		return nil
	case "anyOf":
		if _, ok := rule.val("rules").([]any); !ok {
			return schemaErr(`[@umpire/json] Rule "anyOf" rules must be an array`)
		}
		return nil
	case "check":
		if err := assertString(rule.val("field"), `Rule "check" field`); err != nil {
			return err
		}
		return validateValidatorSpecMembers(rule, []string{"type", "field", "reason"})
	default:
		return schemaErr(
			"[@umpire/json] Unknown rule type %q", rule.val("type"),
		)
	}
}

// validateRule performs shape plus semantic validation for one rule,
// recursing into composites. Mirrors the TS core's validateRule.
func validateRule(raw any, fields map[string]FieldDef, conditions map[string]ConditionDef) error {
	rule, ok := asObject(raw)
	if !ok {
		return schemaErr("[@umpire/json] Rule must be an object")
	}
	if err := validateRuleShape(rule, fields); err != nil {
		return err
	}

	ruleType := rule.str("type")
	switch ruleType {
	case "requires":
		field, _ := rule.string("field")
		if err := assertRuleField(field, fields, `"requires"`); err != nil {
			return err
		}
		if rule.has("dependency") {
			dependency, _ := rule.string("dependency")
			return assertRuleField(dependency, fields, `"requires"`)
		}
		if rule.has("dependencies") {
			deps, _ := rule.val("dependencies").([]any)
			if len(deps) == 0 {
				return schemaErr(
					`[@umpire/json] "requires" rules with dependencies must include at least one entry`,
				)
			}
			for _, d := range deps {
				if depName, isString := d.(string); isString {
					if err := assertRuleField(depName, fields, `"requires"`); err != nil {
						return err
					}
					continue
				}
				if _, err := compileExpr(d, fields, conditions); err != nil {
					return err
				}
			}
			return nil
		}
		_, err := compileExpr(rule.val("when"), fields, conditions)
		return err
	case "enabledWhen":
		field, _ := rule.string("field")
		if err := assertRuleField(field, fields, `"enabledWhen"`); err != nil {
			return err
		}
		_, err := compileExpr(rule.val("when"), fields, conditions)
		return err
	case "disables":
		rawTargets, _ := rule.get("targets")
		targets, _ := rawTargets.([]any)
		for _, t := range targets {
			if err := assertRuleField(t.(string), fields, `"disables"`); err != nil {
				return err
			}
		}
		if rule.has("source") {
			source, _ := rule.string("source")
			return assertRuleField(source, fields, `"disables"`)
		}
		_, err := compileExpr(rule.val("when"), fields, conditions)
		return err
	case "oneOf":
		rawBranches, _ := rule.get("branches")
		branches, _ := asObject(rawBranches)
		for _, branchName := range branches.Keys() {
			branchFields, _ := branches.val(branchName).([]any)
			for _, f := range branchFields {
				if err := assertRuleField(f.(string), fields, `"oneOf"`); err != nil {
					return err
				}
			}
		}
		return nil
	case "fairWhen":
		field, _ := rule.string("field")
		if err := assertRuleField(field, fields, `"fairWhen"`); err != nil {
			return err
		}
		_, err := compileExpr(rule.val("when"), fields, conditions)
		return err
	case "eitherOf":
		rawBranches, _ := rule.get("branches")
		branches, _ := asObject(rawBranches)
		for _, branchName := range branches.Keys() {
			branchRules, _ := branches.val(branchName).([]any)
			for _, inner := range branchRules {
				if err := validateRule(inner, fields, conditions); err != nil {
					return err
				}
			}
		}
		_, err := resolveEitherOfShape(rule)
		return err
	case "anyOf":
		rawRules, _ := rule.get("rules")
		rules, _ := rawRules.([]any)
		for _, inner := range rules {
			if err := validateRule(inner, fields, conditions); err != nil {
				return err
			}
		}
		_, err := resolveCompositeShape("anyOf()", rules)
		return err
	case "check":
		field, _ := rule.string("field")
		return assertRuleField(field, fields, `"check"`)
	default:
		return schemaErr(
			"[@umpire/json] Unknown rule type %q", rule.val("type"),
		)
	}
}

// validateSchemaObject mirrors @umpire/json's validateSchema.
func validateSchemaObject(root *object) error {
	if err := assertKnownMembers(
		root,
		[]string{"version", "conditions", "fields", "rules", "validators", "excluded"},
		"Schema has unknown members; expected version, conditions, fields, rules, validators, excluded",
	); err != nil {
		return err
	}

	versionValue, exists := root.get("version")
	if !exists {
		return schemaErr(`[@umpire/json] Schema must include a "version" field`)
	}
	version, ok := versionValue.(float64)
	if !ok || version != 1 {
		if !ok {
			return schemaErr(
				"[@umpire/json] Unsupported schema version %q", versionValue,
			)
		}
		return schemaErr(
			"[@umpire/json] Unsupported schema version %q", formatJSONNumber(version),
		)
	}

	rawFields, _ := root.get("fields")
	fieldsObj, fieldsOk := asObject(rawFields)
	if !fieldsOk {
		return schemaErr(`[@umpire/json] Schema must include a "fields" object`)
	}

	if rawRules, exists := root.get("rules"); exists {
		if _, ok := rawRules.([]any); !ok {
			return schemaErr(`[@umpire/json] Schema must include a "rules" array`)
		}
	} else {
		return schemaErr(`[@umpire/json] Schema must include a "rules" array`)
	}

	if rawValidators, exists := root.get("validators"); exists {
		if _, ok := asObject(rawValidators); !ok {
			return schemaErr(
				`[@umpire/json] Schema "validators" must be an object when provided`,
			)
		}
	}
	if rawConditions, exists := root.get("conditions"); exists {
		if _, ok := asObject(rawConditions); !ok {
			return schemaErr(
				`[@umpire/json] Schema "conditions" must be an object when provided`,
			)
		}
	}
	if rawExcluded, exists := root.get("excluded"); exists {
		if _, ok := rawExcluded.([]any); !ok {
			return schemaErr(
				`[@umpire/json] Schema "excluded" must be an array when provided`,
			)
		}
	}

	conditions := map[string]ConditionDef{}
	if rawConditions, exists := root.get("conditions"); exists {
		conditionsObj, _ := asObject(rawConditions)
		for _, name := range conditionsObj.Keys() {
			if err := validateCondition(name, conditionsObj.val(name)); err != nil {
				return err
			}
			defObj, _ := asObject(conditionsObj.val(name))
			def := ConditionDef{Type: defObj.str("type")}
			if desc, ok := defObj.val("description").(string); ok {
				def.Description = desc
			}
			conditions[name] = def
		}
	}

	fields := map[string]FieldDef{}
	for _, name := range fieldsObj.Keys() {
		if err := validateFieldDef(name, fieldsObj.val(name)); err != nil {
			return err
		}
		fields[name] = fieldDefFromObject(objOrEmpty(fieldsObj.val(name)))
	}

	if rawRules, exists := root.get("rules"); exists {
		rules, _ := rawRules.([]any)
		for _, rule := range rules {
			if err := validateRule(rule, fields, conditions); err != nil {
				return err
			}
		}
	}

	if rawValidators, exists := root.get("validators"); exists {
		validatorsObj, _ := asObject(rawValidators)
		for _, field := range validatorsObj.Keys() {
			if err := validateValidator(field, validatorsObj.val(field), fields); err != nil {
				return err
			}
		}
	}

	if rawExcluded, exists := root.get("excluded"); exists {
		excluded, _ := rawExcluded.([]any)
		for _, entry := range excluded {
			if err := validateExcludedRule(entry); err != nil {
				return err
			}
		}
	}

	if len(fieldsObj.Keys()) == 0 {
		return schemaErr("[@umpire/json] Schema must include at least one field")
	}

	return nil
}
