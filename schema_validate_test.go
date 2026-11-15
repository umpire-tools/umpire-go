package umpire

import "testing"

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
