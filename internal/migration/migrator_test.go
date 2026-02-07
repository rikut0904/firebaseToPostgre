package migration

import (
	"testing"

	"firebaseToPostgre/internal/firebase"
)

func TestFlattenMap(t *testing.T) {
	input := map[string]interface{}{
		"a": map[string]interface{}{
			"b": 1,
		},
		"c": 2,
	}
	flat := FlattenMap(input, "_")
	if flat["a_b"].(int) != 1 {
		t.Fatalf("expected a_b=1, got %v", flat["a_b"])
	}
	if flat["c"].(int) != 2 {
		t.Fatalf("expected c=2, got %v", flat["c"])
	}
	if _, ok := flat["a"].(map[string]interface{}); !ok {
		t.Fatalf("expected nested map stored at a")
	}
}

func TestInferPGType(t *testing.T) {
	cases := []struct {
		name string
		val  interface{}
		want string
	}{
		{"bool", true, "BOOLEAN"},
		{"intFloat", float64(10), "BIGINT"},
		{"float", float64(10.5), "DOUBLE PRECISION"},
		{"date", "2024-01-01T00:00:00Z", "TIMESTAMPTZ"},
		{"map", map[string]interface{}{"k": 1}, "JSONB"},
	}
	for _, tc := range cases {
		if got := InferPGType(tc.val); got != tc.want {
			t.Fatalf("%s: expected %s, got %s", tc.name, tc.want, got)
		}
	}
}

func TestPipelineTransform(t *testing.T) {
	p := newPipeline([]ColumnMapping{
		{Source: "name", Target: "name", Type: "TEXT", Nullable: false},
		{Source: "age", Target: "age", Type: "BIGINT", Nullable: true},
		{Source: "_id", Target: "id", Type: "TEXT", PrimaryKey: true, Nullable: false},
	})

	rec := firebase.Record{
		Key: "doc1",
		Data: map[string]interface{}{
			"name": "alice",
			"age":  10.0,
		},
	}

	row, err := p.Transform(rec)
	if err != nil {
		t.Fatalf("transform failed: %v", err)
	}
	if row[0].(string) != "alice" {
		t.Fatalf("expected name alice, got %v", row[0])
	}
	if row[1].(int64) != 10 {
		t.Fatalf("expected age 10, got %v", row[1])
	}
	if row[2].(string) != "doc1" {
		t.Fatalf("expected id doc1, got %v", row[2])
	}
}
