package umpire

import "fmt"

// Predicate is a rule condition over runtime values and conditions.
type Predicate func(values, conditions map[string]any) (bool, error)

// FairPredicate receives the field value plus values and conditions.
type FairPredicate func(value any, values, conditions map[string]any) (bool, error)

// RuleEvaluation is a rule's per-target result. Fair is a tri-state: nil
// means the rule does not speak to fairness (a pass).
type RuleEvaluation struct {
	Enabled bool
	Fair    *bool
	Reason  string
	Reasons []string
}

var defaultRuleEvaluation = RuleEvaluation{Enabled: true}

// dep is one requires-dependency: either a field name or a compiled
// expression, kept in declaration order.
type dep struct {
	field string
	expr  *compiledExpr
}

// namedFields is one oneOf branch.
type namedFields struct {
	name   string
	fields []string
}

// namedRules is one eitherOf branch.
type namedRules struct {
	name  string
	rules []*Rule
}

// Rule is a single availability or fairness rule targeting one or more
// fields. Construct rules with the exported constructors or by parsing a
// JSON schema.
type Rule struct {
	kind       string
	targets    []string
	sources    []string
	constraint string // "enabled" | "fair"
	reason     string
	hasReason  bool

	// requires
	deps []dep

	// enabledWhen / fairWhen / disables(when)
	when     *compiledExpr
	fairPred FairPredicate

	// disables(source)
	disableSource    string
	hasDisableSource bool

	// oneOf
	group    string
	branches []namedFields

	// composites
	inner       []*Rule
	branchRules []namedRules

	// check
	check *CheckOp
}

// Targets returns the fields this rule gates.
func (r *Rule) Targets() []string { return r.targets }

// Kind returns the rule kind ("requires", "enabledWhen", "fairWhen",
// "disables", "oneOf", "anyOf", "eitherOf", "check").
func (r *Rule) Kind() string { return r.kind }

// graphSources splits the rule's source fields into ordering edges (which
// drive topological ordering) and informational edges. Mirrors the TS core's
// getGraphSourceInfo.
func (r *Rule) graphSources() (ordering, informational []string) {
	switch r.kind {
	case "enabledWhen":
		if r.when != nil && r.when.checkField != "" {
			return nil, []string{r.when.checkField}
		}
		return nil, r.sources
	case "fairWhen":
		if r.when != nil && r.when.checkField != "" {
			return nil, []string{r.when.checkField}
		}
		return nil, r.sources
	case "anyOf", "eitherOf":
		var innerRules []*Rule
		if r.kind == "anyOf" {
			innerRules = r.inner
		} else {
			for _, b := range r.branchRules {
				innerRules = append(innerRules, b.rules...)
			}
		}
		orderingSet := map[string]bool{}
		for _, inner := range innerRules {
			o, _ := inner.graphSources()
			for _, f := range o {
				orderingSet[f] = true
			}
		}
		ordering = setToSorted(orderingSet)
		for _, inner := range innerRules {
			_, info := inner.graphSources()
			for _, f := range info {
				if !orderingSet[f] {
					informational = append(informational, f)
				}
			}
		}
		return ordering, informational
	default:
		return r.sources, nil
	}
}

func setToSorted(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for k := range set {
		out = append(out, k)
	}
	// deterministic order
	for i := 0; i < len(out); i++ {
		for j := i + 1; j < len(out); j++ {
			if out[j] < out[i] {
				out[i], out[j] = out[j], out[i]
			}
		}
	}
	return out
}

// resolveReason picks the custom reason when set, else the fallback.
func (r *Rule) resolveReason(fallback string) string {
	if r.hasReason {
		return r.reason
	}
	return fallback
}

// evaluateTarget evaluates the rule for one target field. Composites and
// oneOf route through evaluateRule instead.
func (r *Rule) evaluateTarget(field string, ctx *evalContext) (RuleEvaluation, error) {
	switch r.kind {
	case "requires":
		return r.evaluateRequires(ctx)
	case "enabledWhen":
		passed, err := r.when.eval(ctx.values, ctx.conditions)
		if err != nil {
			return RuleEvaluation{}, err
		}
		if passed {
			return RuleEvaluation{Enabled: true}, nil
		}
		return RuleEvaluation{
			Enabled: false,
			Reason:  r.resolveReason("condition not met"),
		}, nil
	case "fairWhen":
		value := ctx.values[field]
		if !isSatisfied(value, ctx.fields[field]) {
			return RuleEvaluation{Enabled: true, Fair: boolPtr(true)}, nil
		}
		passed, err := r.fairPred(value, ctx.values, ctx.conditions)
		if err != nil {
			return RuleEvaluation{}, err
		}
		if passed {
			return RuleEvaluation{Enabled: true, Fair: boolPtr(true)}, nil
		}
		return RuleEvaluation{
			Enabled: true,
			Fair:    boolPtr(false),
			Reason:  r.resolveReason("selection is no longer valid"),
		}, nil
	case "disables":
		var active bool
		if r.hasDisableSource {
			active = isSatisfied(ctx.values[r.disableSource], ctx.fields[r.disableSource])
		} else {
			var err error
			active, err = r.when.eval(ctx.values, ctx.conditions)
			if err != nil {
				return RuleEvaluation{}, err
			}
		}
		if !active {
			return RuleEvaluation{Enabled: true}, nil
		}
		label := "condition"
		if r.hasDisableSource {
			label = r.disableSource
		}
		return RuleEvaluation{
			Enabled: false,
			Reason:  r.resolveReason("overridden by " + label),
		}, nil
	default:
		return defaultRuleEvaluation, nil
	}
}

func (r *Rule) evaluateRequires(ctx *evalContext) (RuleEvaluation, error) {
	var reason string
	var reasons []string

	for _, d := range r.deps {
		var satisfied bool
		fallback := "required condition not met"

		if d.field != "" {
			fallback = "requires " + d.field
			depSatisfied := isSatisfied(ctx.values[d.field], ctx.fields[d.field])
			depAvail, ok := ctx.availability[d.field]
			depEnabled := !ok || depAvail.Enabled
			depFair := !ok || depAvail.Fair
			satisfied = depSatisfied && depEnabled && depFair
		} else {
			var err error
			satisfied, err = d.expr.eval(ctx.values, ctx.conditions)
			if err != nil {
				return RuleEvaluation{}, err
			}
		}

		if satisfied {
			continue
		}

		resolved := r.resolveReason(fallback)
		if reason == "" {
			reason = resolved
		}
		reasons = append(reasons, resolved)
	}

	if len(reasons) == 0 {
		return RuleEvaluation{Enabled: true}, nil
	}
	return RuleEvaluation{Enabled: false, Reason: reason, Reasons: reasons}, nil
}

// evaluateOneOf resolves the oneOf branch and returns per-target results.
// Mirrors the TS core's resolveOneOfState.
func (r *Rule) evaluateOneOf(ctx *evalContext) (map[string]RuleEvaluation, error) {
	satisfiedBranches := []string{}
	for _, branch := range r.branches {
		for _, f := range branch.fields {
			if isSatisfied(ctx.values[f], ctx.fields[f]) {
				satisfiedBranches = append(satisfiedBranches, branch.name)
				break
			}
		}
	}

	var active string
	switch {
	case len(satisfiedBranches) == 0:
		active = ""
	case len(satisfiedBranches) == 1:
		active = satisfiedBranches[0]
	default:
		if ctx.prev != nil {
			previouslySatisfied := map[string]bool{}
			for _, branch := range r.branches {
				for _, f := range branch.fields {
					if isSatisfied(ctx.prev[f], ctx.fields[f]) {
						previouslySatisfied[branch.name] = true
						break
					}
				}
			}
			newly := []string{}
			for _, name := range satisfiedBranches {
				if !previouslySatisfied[name] {
					newly = append(newly, name)
				}
			}
			if len(newly) == 1 {
				active = newly[0]
			}
		}
		if active == "" {
			// Ambiguous fallback: first satisfied branch.
			active = satisfiedBranches[0]
		}
	}

	result := map[string]RuleEvaluation{}
	for _, branch := range r.branches {
		for _, target := range branch.fields {
			if active == "" {
				result[target] = RuleEvaluation{Enabled: true}
				continue
			}
			inActive := false
			for _, b := range r.branches {
				if b.name == active {
					for _, f := range b.fields {
						if f == target {
							inActive = true
						}
					}
				}
			}
			if inActive {
				result[target] = RuleEvaluation{Enabled: true}
			} else {
				result[target] = RuleEvaluation{
					Enabled: false,
					Reason:  r.resolveReason("conflicts with " + active + " strategy"),
				}
			}
		}
	}
	return result, nil
}

// evaluateRule evaluates a rule for one target field, recursing through
// composites. Mirrors the TS evaluator's evaluateRuleForField /
// evaluateRuleForTarget pair.
func evaluateRule(rule *Rule, field string, ctx *evalContext) (RuleEvaluation, error) {
	switch rule.kind {
	case "anyOf":
		return evaluateAnyOf(rule, field, ctx)
	case "eitherOf":
		return evaluateEitherOf(rule, field, ctx)
	case "oneOf":
		if cached, ok := ctx.cache[rule]; ok {
			if result, ok := cached[field]; ok {
				return result, nil
			}
			return defaultRuleEvaluation, nil
		}
		results, err := rule.evaluateOneOf(ctx)
		if err != nil {
			return RuleEvaluation{}, err
		}
		ctx.cache[rule] = results
		if result, ok := results[field]; ok {
			return result, nil
		}
		return defaultRuleEvaluation, nil
	default:
		return rule.evaluateTarget(field, ctx)
	}
}

func evaluateAnyOf(rule *Rule, field string, ctx *evalContext) (RuleEvaluation, error) {
	passed := false
	var reasons []string

	for _, inner := range rule.inner {
		result, err := evaluateRule(inner, field, ctx)
		if err != nil {
			return RuleEvaluation{}, err
		}
		if compositePassed(rule.constraint, result) {
			passed = true
			reasons = nil
			break
		}
		appendCompositeFailureReasons(result, &reasons)
	}

	if passed {
		return compositePassResult(rule.constraint), nil
	}
	return compositeFailureResult(rule.constraint, reasons), nil
}

func evaluateEitherOf(rule *Rule, field string, ctx *evalContext) (RuleEvaluation, error) {
	branchResults := make([]RuleEvaluation, 0, len(rule.branchRules))

	for _, branch := range rule.branchRules {
		innerResults := make([]RuleEvaluation, 0, len(branch.rules))
		for _, inner := range branch.rules {
			result, err := evaluateRule(inner, field, ctx)
			if err != nil {
				return RuleEvaluation{}, err
			}
			innerResults = append(innerResults, result)
		}
		branchResults = append(
			branchResults, combineCompositeResults(rule.constraint, "and", innerResults),
		)
	}

	return combineCompositeResults(rule.constraint, "or", branchResults), nil
}

func compositePassed(constraint string, result RuleEvaluation) bool {
	if constraint == "fair" {
		return result.Fair == nil || *result.Fair
	}
	return result.Enabled
}

func compositePassResult(constraint string) RuleEvaluation {
	if constraint == "fair" {
		return RuleEvaluation{Enabled: true, Fair: boolPtr(true)}
	}
	return RuleEvaluation{Enabled: true}
}

func compositeFailureResult(constraint string, reasons []string) RuleEvaluation {
	if constraint == "fair" {
		eval := RuleEvaluation{Enabled: true, Fair: boolPtr(false)}
		if len(reasons) > 0 {
			eval.Reason = reasons[0]
			eval.Reasons = reasons
		}
		return eval
	}
	eval := RuleEvaluation{Enabled: false}
	if len(reasons) > 0 {
		eval.Reason = reasons[0]
		eval.Reasons = reasons
	}
	return eval
}

func appendCompositeFailureReasons(result RuleEvaluation, reasons *[]string) {
	if len(result.Reasons) > 0 {
		*reasons = append(*reasons, result.Reasons...)
		return
	}
	if result.Reason != "" {
		*reasons = append(*reasons, result.Reason)
	}
}

func combineCompositeResults(constraint, mode string, results []RuleEvaluation) RuleEvaluation {
	passed := mode == "and"

	for _, result := range results {
		currentPassed := compositePassed(constraint, result)
		if mode == "and" {
			if !currentPassed {
				passed = false
				break
			}
			continue
		}
		if currentPassed {
			passed = true
			break
		}
	}

	if passed {
		return compositePassResult(constraint)
	}

	var reasons []string
	for _, result := range results {
		appendCompositeFailureReasons(result, &reasons)
	}
	return compositeFailureResult(constraint, reasons)
}

func boolPtr(b bool) *bool { return &b }

// Programmatic constructors -----------------------------------------------

// EnabledWhen gates a field on a predicate.
func EnabledWhen(field string, when Predicate, reason string) *Rule {
	return &Rule{
		kind:       "enabledWhen",
		targets:    []string{field},
		constraint: "enabled",
		when:       &compiledExpr{pred: when},
		hasReason:  reason != "",
		reason:     reason,
	}
}

// FairPredicateFromPredicate adapts a plain Predicate to a FairPredicate.
func FairPredicateFromPredicate(pred Predicate) FairPredicate {
	return func(_ any, values, conditions map[string]any) (bool, error) {
		return pred(values, conditions)
	}
}

// FairWhen marks a field unfair when its predicate fails.
func FairWhen(field string, pred FairPredicate, reason string) *Rule {
	return &Rule{
		kind:       "fairWhen",
		targets:    []string{field},
		sources:    []string{},
		constraint: "fair",
		fairPred:   pred,
		hasReason:  reason != "",
		reason:     reason,
	}
}

// RequiresDependency makes a field required-until-satisfied against another
// field.
func RequiresDependency(field, dependency, reason string) *Rule {
	return &Rule{
		kind:       "requires",
		targets:    []string{field},
		sources:    []string{dependency},
		constraint: "enabled",
		deps:       []dep{{field: dependency}},
		hasReason:  reason != "",
		reason:     reason,
	}
}

// RequiresPredicate gates a field's requirement on a predicate.
func RequiresPredicate(field string, when Predicate, reason string) *Rule {
	return &Rule{
		kind:       "requires",
		targets:    []string{field},
		sources:    []string{},
		constraint: "enabled",
		deps:       []dep{{expr: &compiledExpr{pred: when}}},
		hasReason:  reason != "",
		reason:     reason,
	}
}

// DisablesField disables targets while a source field holds a satisfied value.
func DisablesField(source string, targets []string, reason string) *Rule {
	return &Rule{
		kind:             "disables",
		targets:          targets,
		sources:          []string{source},
		constraint:       "enabled",
		disableSource:    source,
		hasDisableSource: true,
		hasReason:        reason != "",
		reason:           reason,
	}
}

// DisablesWhen disables targets while a predicate holds.
func DisablesWhen(when Predicate, targets []string, reason string) *Rule {
	return &Rule{
		kind:       "disables",
		targets:    targets,
		sources:    []string{},
		constraint: "enabled",
		when:       &compiledExpr{pred: when},
		hasReason:  reason != "",
		reason:     reason,
	}
}

// Branch is one named oneOf branch.
type Branch struct {
	Name   string
	Fields []string
}

// OneOf allows only one branch of fields to be enabled at a time. The active
// branch is detected from satisfied values, with prev-assisted resolution.
func OneOf(group string, branches []Branch, reason string) *Rule {
	named := make([]namedFields, 0, len(branches))
	var targets []string
	seen := map[string]bool{}
	for _, b := range branches {
		named = append(named, namedFields{name: b.Name, fields: b.Fields})
		for _, f := range b.Fields {
			if seen[f] {
				panic(fmt.Sprintf(
					"[@umpire/core] oneOf(%q) field %q appears in multiple branches", group, f,
				))
			}
			seen[f] = true
			targets = append(targets, f)
		}
	}
	return &Rule{
		kind:       "oneOf",
		targets:    targets,
		sources:    targets,
		constraint: "enabled",
		group:      group,
		branches:   named,
		hasReason:  reason != "",
		reason:     reason,
	}
}

// NamedRules is one named eitherOf branch.
type NamedRules struct {
	Name  string
	Rules []*Rule
}

// AnyOf passes when any inner rule passes.
func AnyOf(rules ...*Rule) *Rule {
	if len(rules) == 0 {
		panic("[@umpire/core] anyOf() requires at least one rule")
	}
	constraint := rules[0].constraint
	targets := rules[0].targets
	for _, r := range rules[1:] {
		if r.constraint != constraint || !sameTargets(r.targets, targets) {
			panic("[@umpire/core] anyOf() rules must target the same fields")
		}
	}
	var sources []string
	seen := map[string]bool{}
	for _, r := range rules {
		for _, s := range r.sources {
			if !seen[s] {
				seen[s] = true
				sources = append(sources, s)
			}
		}
	}
	return &Rule{
		kind:       "anyOf",
		targets:    targets,
		sources:    sources,
		constraint: constraint,
		inner:      rules,
	}
}

// EitherOf passes when any branch's ANDed rules all pass.
func EitherOf(group string, branches []NamedRules) *Rule {
	if len(branches) == 0 {
		panic(fmt.Sprintf(
			"[@umpire/core] eitherOf(%q) must include at least one branch", group,
		))
	}
	var all []*Rule
	for _, b := range branches {
		if len(b.Rules) == 0 {
			panic(fmt.Sprintf(
				"[@umpire/core] eitherOf(%q) branch %q must not be empty", group, b.Name,
			))
		}
		all = append(all, b.Rules...)
	}
	constraint := all[0].constraint
	targets := all[0].targets
	for _, r := range all[1:] {
		if r.constraint != constraint || !sameTargets(r.targets, targets) {
			panic(fmt.Sprintf(
				"[@umpire/core] eitherOf(%q) rules must target the same fields", group,
			))
		}
	}
	return &Rule{
		kind:        "eitherOf",
		targets:     targets,
		constraint:  constraint,
		branchRules: toNamedRules(branches),
	}
}

func toNamedRules(branches []NamedRules) []namedRules {
	out := make([]namedRules, 0, len(branches))
	for _, b := range branches {
		out = append(out, namedRules{name: b.Name, rules: b.Rules})
	}
	return out
}

func sameTargets(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	setA := map[string]bool{}
	for _, f := range a {
		setA[f] = true
	}
	for _, f := range b {
		if !setA[f] {
			return false
		}
	}
	return true
}

// Check builds a field-bound named check as a fairness rule.
func Check(field string, op *CheckOp, reason string) *Rule {
	resolved := reason
	if resolved == "" {
		resolved = op.Reason
	}
	if resolved == "" {
		resolved = "Invalid value"
	}
	return &Rule{
		kind:       "fairWhen",
		targets:    []string{field},
		sources:    []string{field},
		constraint: "fair",
		check:      op,
		reason:     resolved,
		hasReason:  true, // the resolved message is always the reason fallback
		fairPred: func(value any, _ map[string]any, _ map[string]any) (bool, error) {
			if value == nil {
				return false, nil
			}
			return op.Validate(value), nil
		},
	}
}
