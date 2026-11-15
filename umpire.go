package umpire

import "fmt"

// Umpire is the evaluation engine: fields plus rules plus validators,
// mirroring the TS core's createUmpire surface.
type Umpire struct {
	fields     map[string]FieldDef
	fieldOrder []string
	rules      []*Rule
	validators map[string]ValidatorEntry
	topoOrder  []string
}

// New builds an engine from field entries (in declaration order), rules, and
// validators. It rejects unknown field references and dependency cycles.
func New(fields []FieldEntry, rules []*Rule, validators map[string]ValidatorEntry) (*Umpire, error) {
	fieldMap := map[string]FieldDef{}
	var fieldOrder []string
	for _, entry := range fields {
		if _, exists := fieldMap[entry.Name]; exists {
			return nil, fmt.Errorf(
				"[@umpire/core] Field %q is defined more than once", entry.Name,
			)
		}
		fieldMap[entry.Name] = entry.Def
		fieldOrder = append(fieldOrder, entry.Name)
	}

	for _, rule := range rules {
		for _, target := range rule.targets {
			if _, ok := fieldMap[target]; !ok {
				return nil, fmt.Errorf(
					"[@umpire/core] Unknown field %q referenced by %s rule", target, rule.kind,
				)
			}
		}
		ordering, _ := rule.graphSources()
		for _, source := range ordering {
			if _, ok := fieldMap[source]; !ok {
				return nil, fmt.Errorf(
					"[@umpire/core] Unknown field %q referenced by %s rule", source, rule.kind,
				)
			}
		}
	}
	for field := range validators {
		if _, ok := fieldMap[field]; !ok {
			return nil, fmt.Errorf(
				"[@umpire/core] Unknown field %q referenced by validators", field,
			)
		}
	}

	graph, err := buildGraph(fieldOrder, rules)
	if err != nil {
		return nil, err
	}
	if err := graph.detectCycles(); err != nil {
		return nil, err
	}
	topoOrder, err := graph.topologicalSort()
	if err != nil {
		return nil, err
	}

	return &Umpire{
		fields:     fieldMap,
		fieldOrder: fieldOrder,
		rules:      rules,
		validators: validators,
		topoOrder:  topoOrder,
	}, nil
}

// NewFromSchema builds an engine from a hydrated schema.
func NewFromSchema(schema *Schema) (*Umpire, error) {
	entries := make([]FieldEntry, 0, len(schema.FieldOrder))
	for _, name := range schema.FieldOrder {
		entries = append(entries, FieldEntry{Name: name, Def: schema.Fields[name]})
	}
	return New(entries, schema.Rules, schema.Validators)
}

// Check evaluates every rule and returns per-field availability. prev, when
// supplied, assists oneOf branch resolution.
func (u *Umpire) Check(values, conditions, prev map[string]any) (AvailabilityMap, error) {
	if conditions == nil {
		conditions = map[string]any{}
	}

	availability, err := evaluate(
		u.fields, u.fieldOrder, u.rules, u.topoOrder, values, conditions, prev,
	)
	if err != nil {
		return nil, err
	}

	for _, field := range u.fieldOrder {
		entry, hasValidator := u.validators[field]
		status := availability[field]
		if !hasValidator || !status.Enabled || !status.Satisfied {
			continue
		}
		valid := entry.Op.Validate(values[field])
		status.Valid = &valid
		if !valid && entry.HasError {
			status.Error = entry.Error
		}
		availability[field] = status
	}

	return availability, nil
}

// Play recommends fouls for a before → after availability transition.
func (u *Umpire) Play(before, after AvailabilityMap, values map[string]any) []Foul {
	var fouls []Foul
	for _, field := range u.fieldOrder {
		beforeStatus, ok := before[field]
		if !ok {
			continue
		}
		afterStatus, ok := after[field]
		if !ok {
			continue
		}

		disabledTransition := beforeStatus.Enabled && !afterStatus.Enabled
		foulTransition := beforeStatus.Fair && !afterStatus.Fair
		if !disabledTransition && !foulTransition {
			continue
		}
		if !afterStatus.Satisfied {
			continue
		}

		current := values[field]
		suggested := u.fields[field].Default
		if jsonEqual(current, suggested) {
			continue
		}

		reason := afterStatus.Reason
		if reason == "" {
			if disabledTransition {
				reason = "field disabled"
			} else {
				reason = "field fouled"
			}
		}
		fouls = append(fouls, Foul{
			Field:          field,
			Reason:         reason,
			SuggestedValue: suggested,
		})
	}
	return fouls
}

// Init produces starting values from field defaults, with overrides applied.
func (u *Umpire) Init(overrides map[string]any) map[string]any {
	values := map[string]any{}
	for _, field := range u.fieldOrder {
		if override, ok := overrides[field]; ok {
			values[field] = override
		} else {
			values[field] = u.fields[field].Default
		}
	}
	return values
}

// TopoOrder returns the evaluation order of fields.
func (u *Umpire) TopoOrder() []string {
	out := make([]string, len(u.topoOrder))
	copy(out, u.topoOrder)
	return out
}
