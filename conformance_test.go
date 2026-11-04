package umpire

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
)

func resolveConformanceDir(t *testing.T) string {
	t.Helper()
	if dir := os.Getenv("UMPIRE_SPEC_CONFORMANCE_DIR"); dir != "" {
		if _, err := os.Stat(filepath.Join(dir, "index.json")); err == nil {
			return dir
		}
		t.Fatalf(
			"UMPIRE_SPEC_CONFORMANCE_DIR is set to %q but index.json was not found there", dir,
		)
	}
	dir := filepath.Join("spec", "conformance")
	if _, err := os.Stat(filepath.Join(dir, "index.json")); err != nil {
		t.Fatalf(
			"conformance fixtures not found at %s — run `make spec-sync` first",
			dir,
		)
	}
	return dir
}

func loadFixtureIndex(t *testing.T, conformanceDir string) ([]any, []any) {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(conformanceDir, "index.json"))
	if err != nil {
		t.Fatalf("read index.json: %v", err)
	}
	index, err := decodeOrdered(raw)
	if err != nil {
		t.Fatalf("decode index.json: %v", err)
	}
	indexObj, ok := asObject(index)
	if !ok {
		t.Fatalf("index.json must be an object")
	}
	fixtures, _ := indexObj.val("fixtures").([]any)
	failures, _ := indexObj.val("failures").([]any)
	if len(fixtures) == 0 || len(failures) == 0 {
		t.Fatalf("index.json must list fixtures and failures")
	}
	return fixtures, failures
}

func loadFixtureFile(t *testing.T, conformanceDir, relPath string) *object {
	t.Helper()
	raw, err := os.ReadFile(filepath.Join(conformanceDir, relPath))
	if err != nil {
		t.Fatalf("read %s: %v", relPath, err)
	}
	decoded, err := decodeOrdered(raw)
	if err != nil {
		t.Fatalf("decode %s: %v", relPath, err)
	}
	obj, ok := asObject(decoded)
	if !ok {
		t.Fatalf("%s must be an object", relPath)
	}
	return obj
}

func availabilityToMap(fa FieldAvailability) map[string]any {
	var reason any
	if fa.Reason != "" {
		reason = fa.Reason
	}
	reasons := fa.Reasons
	if reasons == nil {
		reasons = []string{}
	}
	m := map[string]any{
		"enabled":   fa.Enabled,
		"satisfied": fa.Satisfied,
		"fair":      fa.Fair,
		"required":  fa.Required,
		"reason":    reason,
		"reasons":   reasons,
	}
	if fa.Valid != nil {
		m["valid"] = *fa.Valid
	}
	if fa.Error != "" {
		m["error"] = fa.Error
	}
	return m
}

func TestConformanceFixtures(t *testing.T) {
	conformanceDir := resolveConformanceDir(t)
	fixtures, _ := loadFixtureIndex(t, conformanceDir)

	for _, entry := range fixtures {
		entryObj, ok := asObject(entry)
		if !ok {
			t.Fatalf("fixture entry must be an object")
		}
		fixtureID, _ := entryObj.string("id")
		path, _ := entryObj.string("path")
		fixture := loadFixtureFile(t, conformanceDir, path)

		t.Run(fixtureID, func(t *testing.T) {
			schemaRaw, _ := fixture.get("schema")
			schema, err := SchemaFromObject(schemaRaw)
			if err != nil {
				t.Fatalf("schema parse failed: %v", err)
			}
			engine, err := NewFromSchema(schema)
			if err != nil {
				t.Fatalf("engine build failed: %v", err)
			}

			cases, _ := fixture.val("cases").([]any)
			for _, rawCase := range cases {
				caseObj, _ := asObject(rawCase)
				caseID, _ := caseObj.string("id")

				values := valuesMap(caseObj, "values")
				conditions := valuesMap(caseObj, "conditions")
				prev := valuesMap(caseObj, "prev")

				availability, err := engine.Check(values, conditions, prev)
				if err != nil {
					t.Fatalf("%s: check failed: %v", caseID, err)
				}

				expectedRaw, _ := caseObj.get("expectedAvailability")
				expected, _ := asObject(expectedRaw)

				for _, field := range expected.Keys() {
					expectedStatus, _ := asObject(expected.val(field))
					expectedMap := map[string]any{}
					for _, key := range expectedStatus.Keys() {
						expectedMap[key] = expectedStatus.val(key)
					}

					actual, ok := availability[field]
					if !ok {
						t.Fatalf("%s: field %q missing from availability", caseID, field)
					}
					actualMap := availabilityToMap(actual)
					// Normalize through JSON so container types ([]string vs []any)
					// match the fixture's decoded shape.
					normalized, err := json.Marshal(actualMap)
					if err != nil {
						t.Fatalf("marshal availability: %v", err)
					}
					var comparableActual map[string]any
					if err := json.Unmarshal(normalized, &comparableActual); err != nil {
						t.Fatalf("unmarshal availability: %v", err)
					}

					if !reflect.DeepEqual(expectedMap, comparableActual) {
						expectedJSON, _ := json.Marshal(expectedMap)
						actualJSON, _ := json.Marshal(comparableActual)
						t.Errorf(
							"%s field %q:\n  expected: %s\n  actual:   %s",
							caseID, field, expectedJSON, actualJSON,
						)
					}
				}
			}
		})
	}
}

func valuesMap(obj *object, key string) map[string]any {
	raw, ok := obj.get(key)
	if !ok {
		return nil
	}
	rawObj, ok := asObject(raw)
	if !ok {
		return nil
	}
	out := map[string]any{}
	for _, k := range rawObj.Keys() {
		out[k] = rawObj.val(k)
	}
	return out
}

func TestConformanceFailureFixtures(t *testing.T) {
	conformanceDir := resolveConformanceDir(t)
	_, failures := loadFixtureIndex(t, conformanceDir)

	for _, entry := range failures {
		entryObj, _ := asObject(entry)
		fixtureID, _ := entryObj.string("id")
		path, _ := entryObj.string("path")
		fixture := loadFixtureFile(t, conformanceDir, path)

		t.Run(fixtureID, func(t *testing.T) {
			failureList, _ := fixture.val("failures").([]any)
			for i, rawFailure := range failureList {
				failure, _ := asObject(rawFailure)
				label := fmt.Sprintf("failure[%d]", i)
				if id, ok := failure.string("id"); ok {
					label = id
				}
				phase, _ := failure.string("phase")
				errorIncludes, _ := failure.string("errorIncludes")
				schemaRaw, _ := failure.get("schema")

				schema, err := SchemaFromObject(schemaRaw)
				if phase == "validate" {
					if err == nil {
						t.Errorf("%s: expected schema validation to fail", label)
						continue
					}
					if !contains(err.Error(), errorIncludes) {
						t.Errorf(
							"%s: error %q does not include %q", label, err.Error(), errorIncludes,
						)
					}
					continue
				}

				if err != nil {
					t.Fatalf("%s: unexpected schema error: %v", label, err)
				}
				engine, err := NewFromSchema(schema)
				if err != nil {
					t.Fatalf("%s: unexpected engine error: %v", label, err)
				}
				values := valuesMap(failure, "values")
				conditions := valuesMap(failure, "conditions")
				_, err = engine.Check(values, conditions, nil)
				if err == nil {
					t.Errorf("%s: expected check to fail", label)
					continue
				}
				if !contains(err.Error(), errorIncludes) {
					t.Errorf(
						"%s: error %q does not include %q", label, err.Error(), errorIncludes,
					)
				}
			}
		})
	}
}

func contains(haystack, needle string) bool {
	return len(needle) == 0 || indexOf(haystack, needle) >= 0
}

func indexOf(haystack, needle string) int {
	for i := 0; i+len(needle) <= len(haystack); i++ {
		if haystack[i:i+len(needle)] == needle {
			return i
		}
	}
	return -1
}
