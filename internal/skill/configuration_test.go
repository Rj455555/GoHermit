package skill

import (
	"encoding/json"
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
