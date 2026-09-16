package haul

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

func schemaMap(t *testing.T) map[string]any {
	t.Helper()
	b, err := ClaimsSchema()
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("the generated schema is not valid JSON: %v", err)
	}
	return m
}

func schemaProp(t *testing.T, path ...string) map[string]any {
	t.Helper()
	cur := schemaMap(t)
	for _, p := range path {
		next, ok := cur[p].(map[string]any)
		if !ok {
			t.Fatalf("schema path %v: %q is missing", path, p)
		}
		cur = next
	}
	return cur
}

func TestClaimsSchema_IsValidJSON(t *testing.T) {
	m := schemaMap(t)
	if m["type"] != "object" {
		t.Errorf("schema is missing its envelope: %v", m["type"])
	}
	// Claude Code resolves $schema as a reference and refuses a document that
	// declares a dialect. Verified against 2.1.273:
	//   no schema with key or ref "https://json-schema.org/draft/2020-12/schema"
	if _, ok := m["$schema"]; ok {
		t.Error("$schema must be omitted; the vendor rejects a declared dialect")
	}
}

// The API refuses these at the top level, and a refused schema produces no
// output at all, which is worse than a permissive one.
func TestClaimsSchema_AvoidsUnsupportedCombinators(t *testing.T) {
	m := schemaMap(t)
	for _, k := range []string{"oneOf", "allOf", "anyOf"} {
		if _, ok := m[k]; ok {
			t.Errorf("top-level %q is refused: input_schema does not support oneOf, allOf, or anyOf at the top level", k)
		}
	}
}

// Ajv runs in strict mode, which rejects a keyword whose applicable type is not
// declared on the same subschema.
func TestClaimsSchema_DeclaresTypesForStrictMode(t *testing.T) {
	var walk func(m map[string]any, path string)
	walk = func(m map[string]any, path string) {
		for _, kw := range []string{"minItems", "maxItems"} {
			if _, ok := m[kw]; ok && m["type"] != "array" {
				t.Errorf("%s: %q needs an accompanying \"type\": \"array\" under strict mode", path, kw)
			}
		}
		if _, ok := m["maxLength"]; ok && m["type"] != "string" {
			t.Errorf("%s: maxLength needs an accompanying string type", path)
		}
		for k, v := range m {
			if sub, ok := v.(map[string]any); ok {
				walk(sub, path+"/"+k)
			}
		}
	}
	walk(schemaMap(t), "#")
}

// Drift guard: every Claims field must be emittable, or it is dead.
func TestClaimsSchema_CoversEveryClaimsField(t *testing.T) {
	props := schemaProp(t, "properties")
	rt := reflect.TypeOf(Claims{})
	for i := 0; i < rt.NumField(); i++ {
		name := strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]
		if name == "" || name == "-" || name == "ext" {
			continue
		}
		if _, ok := props[name]; !ok {
			t.Errorf("Claims.%s (json %q) is missing from the schema", rt.Field(i).Name, name)
		}
	}
}

// Drift guard: the schema must not invent a field the struct cannot hold, which
// the vendor would accept and the decode would then silently drop.
func TestClaimsSchema_InventsNothing(t *testing.T) {
	props := schemaProp(t, "properties")
	known := map[string]bool{}
	rt := reflect.TypeOf(Claims{})
	for i := 0; i < rt.NumField(); i++ {
		known[strings.Split(rt.Field(i).Tag.Get("json"), ",")[0]] = true
	}
	for name := range props {
		if !known[name] {
			t.Errorf("schema declares %q, which Claims has no field for", name)
		}
	}
}

// The reason enum is generated from the same table Validate uses, so the
// vocabulary cannot drift from the validator.
func TestClaimsSchema_ReasonCodesMatchTheValidator(t *testing.T) {
	rc := schemaProp(t, "properties", "outcome_detail", "properties", "reason_code")
	got := map[string]bool{}
	for _, r := range rc["enum"].([]any) {
		got[r.(string)] = true
	}
	want := map[string]bool{}
	for _, o := range outcomeOrder {
		for _, r := range ReasonsFor(o) {
			want[string(r)] = true
		}
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("schema enum and validator table disagree:\n  schema: %v\n  table:  %v", keys(got), keys(want))
	}
}

// The outcome enum must be exactly what a tentacle may write. The head's own
// values must never be offered.
func TestClaimsSchema_OutcomeEnumIsTheTentacleVocabulary(t *testing.T) {
	oc := schemaProp(t, "properties", "outcome")
	for _, v := range oc["enum"].([]any) {
		if !Outcome(v.(string)).WritableByTentacle() {
			t.Errorf("schema offers %q, which only the head may write", v)
		}
	}
	if len(oc["enum"].([]any)) != len(outcomeOrder) {
		t.Errorf("got %d outcomes, want %d", len(oc["enum"].([]any)), len(outcomeOrder))
	}
}

// What the schema cannot enforce, Validate must. This asserts the seam rather
// than assuming it.
func TestClaimsSchema_CoherenceIsLeftToTheValidator(t *testing.T) {
	rc := schemaProp(t, "properties", "outcome_detail", "properties", "reason_code")
	var inEnum bool
	for _, r := range rc["enum"].([]any) {
		if r.(string) == string(ReasonUnsafe) {
			inEnum = true
		}
	}
	if !inEnum {
		t.Fatal("fixture assumes unsafe is in the flat enum")
	}
	// Shape-valid under the schema, incoherent under the contract.
	h := base()
	h.Claims.Outcome = OutcomeNoOp
	h.Claims.OutcomeDetail = &OutcomeDetail{ReasonCode: ReasonUnsafe, Message: "x"}
	if err := h.Validate(); err == nil {
		t.Error("the validator must reject a pairing the schema cannot prevent")
	}
}

// The scoping the schema cannot enforce is at least stated, so a model reading
// the description knows the rule.
func TestClaimsSchema_StatesTheScopingInProse(t *testing.T) {
	desc := schemaProp(t, "properties", "outcome_detail", "properties", "reason_code")["description"].(string)
	for _, o := range []Outcome{OutcomeRefused, OutcomeBlocked, OutcomeNoOp} {
		if !strings.Contains(desc, string(o)) {
			t.Errorf("reason_code description does not mention %q", o)
		}
		for _, r := range ReasonsFor(o) {
			if !strings.Contains(desc, string(r)) {
				t.Errorf("reason_code description omits %q", r)
			}
		}
	}
}

func TestClaimsSchema_IsDeterministic(t *testing.T) {
	a := string(MustClaimsSchema())
	for i := 0; i < 5; i++ {
		if string(MustClaimsSchema()) != a {
			t.Fatal("the generated schema is not byte-stable across calls")
		}
	}
}

func TestClaimsSchema_ExposesOnlyTheClaimsHalf(t *testing.T) {
	m := schemaMap(t)
	props := m["properties"].(map[string]any)
	for _, forbidden := range []string{"assignment", "agent", "verification", "kraken_haul", "schema_version"} {
		if _, ok := props[forbidden]; ok {
			t.Errorf("%q belongs to the head and must not be in the tentacle's schema", forbidden)
		}
	}
	if m["additionalProperties"] != false {
		t.Error("the schema must be closed, or a tentacle can smuggle fields past it")
	}
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
