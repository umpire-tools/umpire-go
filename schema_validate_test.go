package umpire

import (
	"fmt"
	"testing"
)

// The conformance fixtures pin the membership/field-definition cross-check for
// the documents they contain; this table covers every membership op against
// each contradicting emptiness strategy without depending on the suite.
func TestMembershipOpsCrossCheckFieldDefinition(t *testing.T) {
	cases := []struct {
		name       string
		fields     string
		conditions string
		rule       string
		wantErr    string
	}{
		{
			name:   "contains on string field",
			fields: `{"note": {"isEmpty": "string"}, "gate": {}}`,
			rule: `{"type": "enabledWhen", "field": "gate", "when":
				{"op": "contains", "field": "note", "value": "Soto"}}`,
			wantErr: `"contains" requires an array field`,
		},
		{
			name:   "contains on object field",
			fields: `{"meta": {"isEmpty": "object"}, "gate": {}}`,
			rule: `{"type": "enabledWhen", "field": "gate", "when":
				{"op": "contains", "field": "meta", "value": "Soto"}}`,
			wantErr: `"contains" requires an array field`,
		},
		{
			name:   "in on array field",
			fields: `{"tags": {"isEmpty": "array"}, "gate": {}}`,
			rule: `{"type": "enabledWhen", "field": "gate", "when":
				{"op": "in", "field": "tags", "values": ["a"]}}`,
			wantErr: `"in" requires a scalar field`,
		},
		{
			name:       "fieldInCond on array field",
			fields:     `{"tags": {"isEmpty": "array"}, "gate": {}}`,
			conditions: `,"conditions": {"allowed": {"type": "string[]"}}`,
			rule: `{"type": "enabledWhen", "field": "gate", "when":
				{"op": "fieldInCond", "field": "tags", "condition": "allowed"}}`,
			wantErr: `"fieldInCond" requires a scalar field`,
		},
		{
			name:   "notIn on array field nested in anyOf",
			fields: `{"tags": {"isEmpty": "array"}, "gate": {}}`,
			rule: `{"type": "anyOf", "rules": [
				{"type": "enabledWhen", "field": "gate", "when":
					{"op": "notIn", "field": "tags", "values": ["a"]}}]}`,
			wantErr: `"notIn" requires a scalar field`,
		},
		{
			name:   "contains on present field",
			fields: `{"bats": {"isEmpty": "present"}, "gate": {}}`,
			rule: `{"type": "enabledWhen", "field": "gate", "when":
				{"op": "contains", "field": "bats", "value": "Soto"}}`,
		},
		{
			name:   "contains on array field",
			fields: `{"bats": {"isEmpty": "array"}, "gate": {}}`,
			rule: `{"type": "enabledWhen", "field": "gate", "when":
				{"op": "contains", "field": "bats", "value": "Soto"}}`,
		},
		{
			name:   "contains on field without isEmpty",
			fields: `{"bats": {}, "gate": {}}`,
			rule: `{"type": "enabledWhen", "field": "gate", "when":
				{"op": "contains", "field": "bats", "value": "Soto"}}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			schemaJSON := fmt.Sprintf(
				`{"version": 1, "fields": %s%s, "rules": [%s]}`,
				tc.fields, tc.conditions, tc.rule,
			)
			_, err := ParseSchema([]byte(schemaJSON))
			if tc.wantErr == "" {
				if err != nil {
					t.Fatalf("expected the schema to parse, got %q", err.Error())
				}
				return
			}
			if err == nil {
				t.Fatalf("expected an error containing %q", tc.wantErr)
			}
			if !contains(err.Error(), tc.wantErr) {
				t.Errorf("error %q does not contain %q", err.Error(), tc.wantErr)
			}
		})
	}
}

// The conformance failures pin the required primitive value and the field
// definition cross-check for contains. The closed member set is only pinned
// by the public meta-schema, so guard the runtime path here.
func TestContainsRejectsUnknownMembers(t *testing.T) {
	schemaJSON := `{
		"version": 1,
		"fields": {"benchBats": {"isEmpty": "array"}, "gate": {}},
		"rules": [
			{
				"type": "enabledWhen",
				"field": "gate",
				"when": {
					"op": "contains",
					"field": "benchBats",
					"value": "Soto",
					"values": ["Soto"]
				}
			}
		]
	}`

	_, err := ParseSchema([]byte(schemaJSON))
	if err == nil {
		t.Fatal("expected schema validation to fail")
	}
	if !contains(err.Error(), `has unknown member "values"`) {
		t.Errorf("error %q does not report the unknown member", err.Error())
	}
}
