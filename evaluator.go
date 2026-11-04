package umpire

// evalContext carries one Check evaluation's shared state.
type evalContext struct {
	fields       map[string]FieldDef
	values       map[string]any
	conditions   map[string]any
	prev         map[string]any
	availability AvailabilityMap
	cache        map[*Rule]map[string]RuleEvaluation
}

// evaluate runs all rules over the fields in topological order, mirroring
// the TS evaluator: gate rules first, fair rules only for still-enabled
// fields, first-failure reason plus full reason collection.
func evaluate(
	fields map[string]FieldDef,
	fieldOrder []string,
	rules []*Rule,
	topoOrder []string,
	values, conditions, prev map[string]any,
) (AvailabilityMap, error) {
	ctx := &evalContext{
		fields:       fields,
		values:       values,
		conditions:   conditions,
		prev:         prev,
		availability: AvailabilityMap{},
		cache:        map[*Rule]map[string]RuleEvaluation{},
	}

	gateRules, fairRules := partitionRulesByPhase(rules)
	rulesByTargetGate := indexByTarget(gateRules)
	rulesByTargetFair := indexByTarget(fairRules)

	availability := ctx.availability

	for _, field := range topoOrder {
		reasons := []string{}
		enabled := true
		fair := true
		reason := ""

		for _, rule := range rulesByTargetGate[field] {
			result, err := evaluateRule(rule, field, ctx)
			if err != nil {
				return nil, err
			}
			if result.Enabled {
				continue
			}
			enabled = false
			if reason == "" {
				reason = result.Reason
			}
			appendCompositeFailureReasons(result, &reasons)
		}

		if enabled {
			for _, rule := range rulesByTargetFair[field] {
				result, err := evaluateRule(rule, field, ctx)
				if err != nil {
					return nil, err
				}
				if result.Fair == nil || *result.Fair {
					continue
				}
				fair = false
				if reason == "" {
					reason = result.Reason
				}
				appendCompositeFailureReasons(result, &reasons)
			}
		}

		def := fields[field]
		availability[field] = FieldAvailability{
			Enabled:   enabled,
			Satisfied: isSatisfied(values[field], def),
			Fair:      fair,
			Required:  enabled && def.Required,
			Reason:    reason,
			Reasons:   reasons,
		}
	}

	return availability, nil
}

func partitionRulesByPhase(rules []*Rule) (gate, fair []*Rule) {
	for _, rule := range rules {
		if rule.constraint == "fair" {
			fair = append(fair, rule)
		} else {
			gate = append(gate, rule)
		}
	}
	return gate, fair
}

func indexByTarget(rules []*Rule) map[string][]*Rule {
	index := map[string][]*Rule{}
	for _, rule := range rules {
		for _, target := range rule.targets {
			index[target] = append(index[target], rule)
		}
	}
	return index
}
