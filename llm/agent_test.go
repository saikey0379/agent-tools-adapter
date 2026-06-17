package llm

import (
	"reflect"
	"testing"

	"agent-tools/tools"
)

func TestBuildToolInputSchemaPreservesEnumAndNormalizesArrayTypes(t *testing.T) {
	schema := buildToolInputSchema(tools.ToolSchema{
		Name: "create_item",
		Params: []tools.ToolParam{
			{Name: "status", Type: "string", Enum: []any{"draft", "active", "archived"}},
			{Name: "tags", Type: "array[string]"},
			{Name: "metadata", Type: "string", Required: true},
		},
	})

	props := schema["properties"].(map[string]any)
	status := props["status"].(map[string]any)
	wantEnum := []any{"draft", "active", "archived"}
	if !reflect.DeepEqual(status["enum"], wantEnum) {
		t.Fatalf("status enum = %#v", status["enum"])
	}

	tags := props["tags"].(map[string]any)
	if tags["type"] != "array" {
		t.Fatalf("tags type = %#v", tags["type"])
	}
	items := tags["items"].(map[string]any)
	if items["type"] != "string" {
		t.Fatalf("tags items type = %#v", items["type"])
	}

	required := schema["required"].([]string)
	if !reflect.DeepEqual(required, []string{"metadata"}) {
		t.Fatalf("required = %#v", required)
	}
}
