package umpire

import "testing"

func testFields() []FieldEntry {
	return []FieldEntry{
		{Name: "email", Def: FieldDef{IsEmpty: EmptyString}},
		{Name: "plan", Def: FieldDef{Default: "basic", IsEmpty: EmptyString}},
		{Name: "discount", Def: FieldDef{IsEmpty: EmptyNumber}},
		{Name: "company", Def: FieldDef{IsEmpty: EmptyString}},
	}
}

func TestCheckProgrammaticRules(t *testing.T) {
	engine, err := New(
		testFields(),
		[]*Rule{
			EnabledWhen("discount", func(values, _ map[string]any) (bool, error) {
				return values["plan"] == "pro", nil
			}, ""), // reason: condition not met
			RequiresDependency("company", "email", ""),
		},
		map[string]ValidatorEntry{
			"email": {Op: CheckOps.Email(), Error: "Bad email", HasError: true},
		},
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	availability, err := engine.Check(map[string]any{
		"email": "not-an-email", "plan": "pro", "discount": 10.0,
	}, nil, nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}

	discount := availability["discount"]
	if !discount.Enabled || !discount.Satisfied {
		t.Errorf("discount should be enabled and satisfied: %+v", discount)
	}
	company := availability["company"]
	if !company.Enabled || company.Required {
		t.Errorf("company should be enabled and optional: %+v", company)
	}
	if company.Satisfied {
		t.Errorf("company is absent, should not be satisfied: %+v", company)
	}
	email := availability["email"]
	if email.Valid == nil || *email.Valid {
		t.Errorf("email value should report invalid: %+v", email)
	}
	if email.Error != "Bad email" {
		t.Errorf("email error = %q", email.Error)
	}
	if !email.Enabled {
		t.Errorf("email has no gate rules; expected enabled")
	}
}

func TestCheckMissingConditionFails(t *testing.T) {
	engine, err := New(
		testFields(),
		[]*Rule{
			EnabledWhen("discount", func(values, conditions map[string]any) (bool, error) {
				v, ok := conditions["isPro"]
				if !ok {
					return false, &MissingConditionError{Name: "isPro"}
				}
				return v == true, nil
			}, ""),
		},
		nil,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := engine.Check(map[string]any{"plan": "basic"}, nil, nil); err == nil {
		t.Fatal("expected missing-condition error")
	}
}

// MissingConditionError mirrors the runtime's missing-condition failure.
type MissingConditionError struct{ Name string }

func (e *MissingConditionError) Error() string {
	return "Missing runtime condition " + e.Name
}

func TestOneOfPrevResolution(t *testing.T) {
	branches := []Branch{
		{Name: "phone", Fields: []string{"bullpenPhone"}},
		{Name: "signal", Fields: []string{"outfieldSignal"}},
	}
	engine, err := New(
		[]FieldEntry{
			{Name: "bullpenPhone", Def: FieldDef{IsEmpty: EmptyString}},
			{Name: "outfieldSignal", Def: FieldDef{IsEmpty: EmptyString}},
		},
		[]*Rule{OneOf("relay", branches, "")},
		nil,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	// Both branches satisfied; prev had phone satisfied → signal is newly
	// satisfied and wins.
	values := map[string]any{"bullpenPhone": "ring", "outfieldSignal": "wave"}
	prev := map[string]any{"bullpenPhone": "ring"}
	availability, err := engine.Check(values, nil, prev)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	if !availability["outfieldSignal"].Enabled {
		t.Errorf("signal branch should win via prev: %+v", availability["outfieldSignal"])
	}
	if availability["bullpenPhone"].Enabled {
		t.Errorf("phone branch should be locked out: %+v", availability["bullpenPhone"])
	}
	if availability["bullpenPhone"].Reason != "conflicts with signal strategy" {
		t.Errorf("phone reason = %q", availability["bullpenPhone"].Reason)
	}
}

func TestPlayFouls(t *testing.T) {
	engine, err := New(
		testFields(),
		[]*Rule{
			EnabledWhen("discount", func(values, _ map[string]any) (bool, error) {
				return values["plan"] == "pro", nil
			}, "Pro plan only"),
		},
		nil,
	)
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	before, err := engine.Check(map[string]any{"plan": "pro", "discount": 10.0}, nil, nil)
	if err != nil {
		t.Fatalf("before check: %v", err)
	}
	after, err := engine.Check(map[string]any{"plan": "basic", "discount": 10.0}, nil, nil)
	if err != nil {
		t.Fatalf("after check: %v", err)
	}

	fouls := engine.Play(before, after, map[string]any{"plan": "basic", "discount": 10.0})
	if len(fouls) != 1 {
		t.Fatalf("expected 1 foul, got %d: %+v", len(fouls), fouls)
	}
	foul := fouls[0]
	if foul.Field != "discount" {
		t.Errorf("foul field = %q", foul.Field)
	}
	if foul.Reason != "Pro plan only" {
		t.Errorf("foul reason = %q", foul.Reason)
	}
	if foul.SuggestedValue != nil {
		t.Errorf("foul suggestedValue = %v", foul.SuggestedValue)
	}
}

func TestInitDefaults(t *testing.T) {
	engine, err := New(testFields(), nil, nil)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	values := engine.Init(map[string]any{"email": "a@b.com"})
	if values["plan"] != "basic" {
		t.Errorf("plan default = %v", values["plan"])
	}
	if values["email"] != "a@b.com" {
		t.Errorf("email override = %v", values["email"])
	}
	if _, has := values["discount"]; !has {
		t.Error("discount should be initialized (nil default)")
	}
}

func TestCycleDetection(t *testing.T) {
	_, err := New(
		testFields(),
		[]*Rule{
			RequiresDependency("company", "email", ""),
			RequiresDependency("email", "company", ""),
		},
		nil,
	)
	if err == nil {
		t.Fatal("expected cycle detection error")
	}
}

func TestSchemaRoundTripWarnings(t *testing.T) {
	schemaJSON := `{
		"version": 1,
		"fields": {"email": {"isEmpty": "string"}},
		"rules": [],
		"validators": {"email": {"op": "email", "error": "Enter a valid email address"}},
		"excluded": [
			{"type": "custom", "field": "email", "description": "handle natively"}
		]
	}`
	schema, err := ParseSchema([]byte(schemaJSON))
	if err != nil {
		t.Fatalf("ParseSchema: %v", err)
	}
	if len(schema.Warnings) != 1 {
		t.Fatalf("expected 1 warning, got %v", schema.Warnings)
	}
	engine, err := NewFromSchema(schema)
	if err != nil {
		t.Fatalf("NewFromSchema: %v", err)
	}
	availability, err := engine.Check(map[string]any{"email": "nope"}, nil, nil)
	if err != nil {
		t.Fatalf("Check: %v", err)
	}
	email := availability["email"]
	if email.Valid == nil || *email.Valid {
		t.Errorf("email should be invalid: %+v", email)
	}
	if email.Error != "Enter a valid email address" {
		t.Errorf("email error = %q", email.Error)
	}
}
