package skill

import (
	"encoding/json"
	"strconv"
	"strings"
	"testing"
)

func TestValidateConfigurationSubset(t *testing.T) {
	schema := json.RawMessage(`{
		"type":"object",
		"properties":{
			"mode":{"type":"string","enum":["safe","strict"]},
			"retries":{"type":"integer","minimum":0,"maximum":3},
			"enabled":{"type":"boolean"}
		},
		"required":["mode"],
		"additionalProperties":false
	}`)
	for _, configuration := range []string{
		`{"mode":"safe"}`,
		`{"mode":"strict","retries":3,"enabled":true}`,
	} {
		if err := ValidateConfiguration(schema, json.RawMessage(configuration)); err != nil {
			t.Fatalf("valid configuration %s: %v", configuration, err)
		}
	}
	for name, configuration := range map[string]string{
		"missing required": `{}`,
		"unknown property": `{"mode":"safe","extra":true}`,
		"wrong type":       `{"mode":"safe","retries":"3"}`,
		"out of range":     `{"mode":"safe","retries":4}`,
		"invalid enum":     `{"mode":"unsafe"}`,
		"multiple values":  `{"mode":"safe"} {}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateConfiguration(schema, json.RawMessage(configuration)); err == nil {
				t.Fatal("invalid configuration must fail closed")
			}
		})
	}
}

func TestConfigurationRejectsUnknownSchemaAndSecrets(t *testing.T) {
	for name, schema := range map[string]string{
		"unknown keyword":  `{"type":"object","properties":{},"additionalProperties":false,"$ref":"x"}`,
		"open object":      `{"type":"object","properties":{}}`,
		"credential key":   `{"type":"object","properties":{"api_key":{"type":"string"}},"additionalProperties":false}`,
		"unsupported type": `{"type":"object","properties":{"x":{"type":"null"}},"additionalProperties":false}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateConfigurationSchema(json.RawMessage(schema)); err == nil {
				t.Fatal("unsafe schema must fail closed")
			}
		})
	}
	schema := json.RawMessage(`{"type":"object","properties":{"label":{"type":"string"}},"additionalProperties":false}`)
	for _, value := range []string{
		`{"label":"` + "sk-" + "live-12345678901234567890" + `"}`,
		`{"label":"password=not-allowed"}`,
	} {
		if err := ValidateConfiguration(schema, json.RawMessage(value)); err == nil {
			t.Fatal("secret-like configuration must fail closed")
		}
	}
	oversized := json.RawMessage(`{"label":"` + strings.Repeat("x", maxConfigurationBytes) + `"}`)
	if err := ValidateConfiguration(schema, oversized); err == nil {
		t.Fatal("oversized configuration must fail closed")
	}
}

func TestValidateConfigurationCompositeTypes(t *testing.T) {
	schema := json.RawMessage(`{
		"type":"object",
		"properties":{
			"tags":{"type":"array","items":{"type":"string","minLength":1,"maxLength":5}},
			"score":{"type":"number","minimum":0,"maximum":1},
			"nested":{"type":"object","properties":{"enabled":{"type":"boolean"}},"required":["enabled"],"additionalProperties":false}
		},
		"required":["tags","score","nested"],
		"additionalProperties":false
	}`)
	if err := ValidateConfiguration(schema, json.RawMessage(`{"tags":["safe"],"score":0.5,"nested":{"enabled":true}}`)); err != nil {
		t.Fatal(err)
	}
	for name, configuration := range map[string]string{
		"short string": `{"tags":[""],"score":0.5,"nested":{"enabled":true}}`,
		"long string":  `{"tags":["toolong"],"score":0.5,"nested":{"enabled":true}}`,
		"number bound": `{"tags":["safe"],"score":2,"nested":{"enabled":true}}`,
		"boolean type": `{"tags":["safe"],"score":0.5,"nested":{"enabled":"yes"}}`,
		"array type":   `{"tags":"safe","score":0.5,"nested":{"enabled":true}}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateConfiguration(schema, json.RawMessage(configuration)); err == nil {
				t.Fatal("invalid composite configuration must fail closed")
			}
		})
	}
}

func TestConfigurationSchemaStructuralLimits(t *testing.T) {
	properties := make([]string, 0, maxSchemaProperties+1)
	for index := 0; index < maxSchemaProperties+1; index++ {
		properties = append(properties, `"p`+strconv.Itoa(index)+`":{"type":"string"}`)
	}
	for name, schema := range map[string]string{
		"root type":           `{"type":"string"}`,
		"array missing items": `{"type":"object","properties":{"x":{"type":"array"}},"additionalProperties":false}`,
		"undeclared required": `{"type":"object","properties":{},"required":["x"],"additionalProperties":false}`,
		"duplicate required":  `{"type":"object","properties":{"x":{"type":"string"}},"required":["x","x"],"additionalProperties":false}`,
		"property limit":      `{"type":"object","properties":{` + strings.Join(properties, ",") + `},"additionalProperties":false}`,
		"string bounds":       `{"type":"object","properties":{"x":{"type":"string","minLength":2,"maxLength":1}},"additionalProperties":false}`,
		"number bounds":       `{"type":"object","properties":{"x":{"type":"number","minimum":2,"maximum":1}},"additionalProperties":false}`,
	} {
		t.Run(name, func(t *testing.T) {
			if err := ValidateConfigurationSchema(json.RawMessage(schema)); err == nil {
				t.Fatal("structurally invalid schema must fail closed")
			}
		})
	}
}
