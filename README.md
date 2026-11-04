# umpire-go

Go-native Umpire semantics: a runtime evaluator for Umpire availability
schemas. Where [`umpire-go-gen`](https://github.com/umpire-tools/umpire-go-gen)
compiles a static `.umpire.json` into typed Go ahead of time, this package
evaluates schemas **live at runtime** — rules stay data, so a binary can load
a schema it has never seen without a rebuild.

It is a port of the TypeScript `@umpire/core` + `@umpire/json` semantics,
matching the [`umpire-spec`](https://github.com/umpire-tools/umpire-spec)
conformance suite (same fixtures `umpire-py` and `umpire-dart` run).

## Status

Conforming: the full pinned `umpire-spec` conformance suite passes — all
availability fixtures plus the validate/evaluate failure fixtures.

## Installation

```bash
go get github.com/umpire-tools/umpire-go
```

## Quick Start

### From a JSON schema (the live path)

```go
package main

import (
	"fmt"

	"github.com/umpire-tools/umpire-go"
)

const checkoutSchema = `{
	"version": 1,
	"fields": {
		"email": {"isEmpty": "string"},
		"plan": {"default": "basic", "isEmpty": "string"},
		"company": {"isEmpty": "string"}
	},
	"rules": [
		{"type": "requires", "field": "company", "dependency": "plan"},
		{"type": "check", "field": "email", "op": "email"}
	],
	"validators": {"email": {"op": "email", "error": "Enter a valid email address"}}
}`

func main() {
	schema, err := umpire.ParseSchema([]byte(checkoutSchema))
	if err != nil {
		panic(err)
	}
	engine, err := umpire.NewFromSchema(schema)
	if err != nil {
		panic(err)
	}

	availability, err := engine.Check(map[string]any{
		"email": "nope",
		"plan":  "business",
	}, map[string]any{}, nil)
	if err != nil {
		panic(err)
	}

	fmt.Println(availability["company"]) // locked until plan's dependency resolves
	fmt.Println(availability["email"])   // valid=false, error="Enter a valid email address"
}
```

### From programmatic rules

```go
engine, err := umpire.New(
	[]umpire.FieldEntry{
		{Name: "email", Def: umpire.FieldDef{IsEmpty: umpire.EmptyString}},
		{Name: "company", Def: umpire.FieldDef{IsEmpty: umpire.EmptyString}},
	},
	[]*umpire.Rule{
		umpire.RequiresDependency("company", "email", ""),
		umpire.EnabledWhen("company", func(values, conditions map[string]any) (bool, error) {
			return values["plan"] == "business", nil
		}, "Business accounts only"),
	},
	map[string]umpire.ValidatorEntry{
		"email": {Op: umpire.CheckOps.Email(), Error: "Bad email", HasError: true},
	},
)
```

## API Overview

| Symbol | Purpose |
|--------|---------|
| `ParseSchema([]byte)` / `SchemaFromObject(any)` | Validate + hydrate a JSON schema (strict, closed-member) |
| `NewFromSchema(*Schema)` / `New([]FieldEntry, []*Rule, map[string]ValidatorEntry)` | Build an engine |
| `(*Umpire).Check(values, conditions, prev)` | Evaluate all rules → `AvailabilityMap` (per-field enabled/required/fair/satisfied/reason/reasons/valid/error) |
| `(*Umpire).Play(before, after, values)` | Foul recommendations for a before → after transition |
| `(*Umpire).Init(overrides)` | Starting values from field defaults |
| `EnabledWhen` / `FairWhen` / `RequiresDependency` / `RequiresPredicate` / `DisablesField` / `DisablesWhen` / `OneOf` / `AnyOf` / `EitherOf` / `Check` | Rule constructors |
| `CheckOps` | Named validators: `email`, `url`, `matches`, `minLength`, `maxLength`, `min`, `max`, `range`, `integer` |

Semantic notes:

- `prev` assists `oneOf` branch resolution (newly-satisfied branch wins over
  the ambiguous first-satisfied fallback), matching the TS core.
- Satisfaction is computed from the values you pass — defaults are **not**
  seeded into `Check`.
- Expression predicates throw on missing runtime conditions; `Check` surfaces
  those as errors.
- Schema validation is closed-member: unexpected keys fail at parse time with
  the same messages as `@umpire/json`.

## Conformance

```bash
make test        # syncs pinned spec fixtures, then go test ./...
```

Fixtures sync from the pinned `umpire-spec` tag (SHA-verified in the
`Makefile`). Point `UMPIRE_SPEC_CONFORMANCE_DIR` at a local checkout for
spec development.

## Project Layout

| File | Purpose |
|------|---------|
| `schema.go`, `schema_validate.go`, `schema_rules.go` | Strict schema validation and hydration |
| `rule.go` | Rule model + constructors, oneOf/anyOf/eitherOf evaluation |
| `expr.go` | Expression DSL compiler |
| `checkops.go` | Named check validators |
| `evaluator.go` | Two-phase (gate/fair) topological evaluation |
| `graph.go` | Dependency graph, cycle detection, topo sort |
| `umpire.go` | Engine: `Check`, `Play`, `Init` |
| `ordered.go` | Order-preserving JSON decoding (branch/reason order is contract-visible) |
| `conformance_test.go` | The spec conformance harness |
