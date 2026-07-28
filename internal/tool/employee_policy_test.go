package tool

import (
	"context"
	"testing"
)

type policyTestTool struct{ definition Definition }

func (t policyTestTool) Definition() Definition { return t.definition }
func (policyTestTool) Execute(context.Context, Call) (Result, error) {
	return Result{}, nil
}

func TestRegistryEmployeePolicyCanOnlyNarrowExistingTools(t *testing.T) {
	registry := NewRegistry()
	for _, definition := range []Definition{
		{Name: "filesystem.read", Permission: PermissionRead},
		{Name: "filesystem.write", Permission: PermissionWrite, MutatesWorkspace: true},
		{Name: "test.run", Permission: PermissionExecute},
	} {
		if err := registry.Register(policyTestTool{definition: definition}); err != nil {
			t.Fatal(err)
		}
	}
	registry.RestrictCapabilities([]string{"read", "test.run", "nonexistent.tool"})
	definitions := registry.Definitions()
	if len(definitions) != 2 || definitions[0].Name != "filesystem.read" || definitions[1].Name != "test.run" {
		t.Fatalf("narrowed registry = %#v", definitions)
	}
	if _, exists := registry.Get("filesystem.write"); exists {
		t.Fatal("Employee policy retained a disallowed mutation tool")
	}
	if _, exists := registry.Get("nonexistent.tool"); exists {
		t.Fatal("Employee policy added a tool")
	}
}
