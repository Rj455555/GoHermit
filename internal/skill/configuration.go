package skill

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"strings"

	"github.com/Rj455555/GoHermit/internal/owner"
)

const (
	maxConfigurationBytes = 32 << 10
	maxSchemaDepth        = 8
	maxSchemaProperties   = 64
	maxArrayItems         = 256
)

type configurationSchema struct {
	Type                 string                         `json:"type"`
	Properties           map[string]configurationSchema `json:"properties,omitempty"`
	Required             []string                       `json:"required,omitempty"`
	AdditionalProperties *bool                          `json:"additionalProperties,omitempty"`
	Items                *configurationSchema           `json:"items,omitempty"`
	Enum                 []json.RawMessage              `json:"enum,omitempty"`
	MinLength            *int                           `json:"minLength,omitempty"`
	MaxLength            *int                           `json:"maxLength,omitempty"`
	Minimum              *float64                       `json:"minimum,omitempty"`
	Maximum              *float64                       `json:"maximum,omitempty"`
}

// ValidateConfigurationSchema validates the deliberately small, non-executable
// JSON Schema subset accepted by native Skills.
func ValidateConfigurationSchema(raw json.RawMessage) error {
	if len(raw) == 0 || len(raw) > maxConfigurationBytes {
		return errors.New("configuration schema is missing or oversized")
	}
	var schema configurationSchema
	if err := decodeStrict(raw, &schema); err != nil {
		return fmt.Errorf("invalid configuration schema: %w", err)
	}
	return validateSchemaNode(schema, 0, true)
}

// ValidateConfiguration validates one binding configuration against a pinned
// Skill schema. It never resolves references, applies defaults, or executes
// schema-provided content.
func ValidateConfiguration(schemaRaw, configuration json.RawMessage) error {
	if err := ValidateConfigurationSchema(schemaRaw); err != nil {
		return err
	}
	if len(configuration) == 0 {
		configuration = json.RawMessage(`{}`)
	}
	if len(configuration) > maxConfigurationBytes {
		return errors.New("Skill configuration exceeds 32 KiB")
	}
	var schema configurationSchema
	if err := decodeStrict(schemaRaw, &schema); err != nil {
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(configuration))
	decoder.UseNumber()
	var value any
	if err := decoder.Decode(&value); err != nil {
		return fmt.Errorf("invalid Skill configuration JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return errors.New("Skill configuration must contain exactly one JSON value")
	}
	return validateConfiguredValue(schema, value, "$", 0)
}

func validateSchemaNode(schema configurationSchema, depth int, root bool) error {
	if depth > maxSchemaDepth {
		return errors.New("configuration schema nesting limit exceeded")
	}
	switch schema.Type {
	case "object":
		if len(schema.Properties) > maxSchemaProperties {
			return errors.New("configuration schema property limit exceeded")
		}
		if schema.AdditionalProperties == nil || *schema.AdditionalProperties {
			return errors.New("object schemas must set additionalProperties to false")
		}
		required := make(map[string]struct{}, len(schema.Required))
		for _, name := range schema.Required {
			if err := validateConfigurationName(name); err != nil {
				return err
			}
			if _, duplicate := required[name]; duplicate {
				return fmt.Errorf("duplicate required property %q", name)
			}
			if _, exists := schema.Properties[name]; !exists {
				return fmt.Errorf("required property %q is not declared", name)
			}
			required[name] = struct{}{}
		}
		for name, child := range schema.Properties {
			if err := validateConfigurationName(name); err != nil {
				return err
			}
			if err := validateSchemaNode(child, depth+1, false); err != nil {
				return fmt.Errorf("property %q: %w", name, err)
			}
		}
		if schema.Items != nil || schema.MinLength != nil || schema.MaxLength != nil ||
			schema.Minimum != nil || schema.Maximum != nil {
			return errors.New("object schema contains incompatible constraints")
		}
	case "array":
		if schema.Items == nil {
			return errors.New("array schema requires items")
		}
		if err := validateSchemaNode(*schema.Items, depth+1, false); err != nil {
			return err
		}
		if len(schema.Properties) != 0 || len(schema.Required) != 0 || schema.AdditionalProperties != nil {
			return errors.New("array schema contains object constraints")
		}
	case "string":
		if schema.MinLength != nil && (*schema.MinLength < 0 || *schema.MinLength > maxConfigurationBytes) {
			return errors.New("invalid minLength")
		}
		if schema.MaxLength != nil && (*schema.MaxLength < 0 || *schema.MaxLength > maxConfigurationBytes) {
			return errors.New("invalid maxLength")
		}
		if schema.MinLength != nil && schema.MaxLength != nil && *schema.MinLength > *schema.MaxLength {
			return errors.New("minLength exceeds maxLength")
		}
	case "integer", "number":
		if schema.Minimum != nil && schema.Maximum != nil && *schema.Minimum > *schema.Maximum {
			return errors.New("minimum exceeds maximum")
		}
	case "boolean":
	default:
		return fmt.Errorf("unsupported configuration schema type %q", schema.Type)
	}
	if root && schema.Type != "object" {
		return errors.New("configuration schema root must be an object")
	}
	if len(schema.Enum) > 64 {
		return errors.New("configuration enum limit exceeded")
	}
	for _, value := range schema.Enum {
		if len(value) > maxConfigurationBytes || !json.Valid(value) {
			return errors.New("invalid configuration enum value")
		}
	}
	return nil
}

func validateConfiguredValue(schema configurationSchema, value any, path string, depth int) error {
	if depth > maxSchemaDepth {
		return errors.New("Skill configuration nesting limit exceeded")
	}
	if len(schema.Enum) != 0 {
		raw, _ := json.Marshal(value)
		matched := false
		for _, candidate := range schema.Enum {
			if bytes.Equal(raw, candidate) {
				matched = true
				break
			}
		}
		if !matched {
			return fmt.Errorf("%s is not an allowed enum value", path)
		}
	}
	switch schema.Type {
	case "object":
		object, ok := value.(map[string]any)
		if !ok {
			return fmt.Errorf("%s must be an object", path)
		}
		for _, required := range schema.Required {
			if _, exists := object[required]; !exists {
				return fmt.Errorf("%s.%s is required", path, required)
			}
		}
		for name, childValue := range object {
			if err := validateConfigurationName(name); err != nil {
				return err
			}
			child, exists := schema.Properties[name]
			if !exists {
				return fmt.Errorf("%s.%s is not allowed", path, name)
			}
			if err := validateConfiguredValue(child, childValue, path+"."+name, depth+1); err != nil {
				return err
			}
		}
	case "array":
		array, ok := value.([]any)
		if !ok {
			return fmt.Errorf("%s must be an array", path)
		}
		if len(array) > maxArrayItems {
			return fmt.Errorf("%s exceeds the array item limit", path)
		}
		for index, item := range array {
			if err := validateConfiguredValue(*schema.Items, item, fmt.Sprintf("%s[%d]", path, index), depth+1); err != nil {
				return err
			}
		}
	case "string":
		text, ok := value.(string)
		if !ok {
			return fmt.Errorf("%s must be a string", path)
		}
		if schema.MinLength != nil && len(text) < *schema.MinLength {
			return fmt.Errorf("%s is shorter than minLength", path)
		}
		if schema.MaxLength != nil && len(text) > *schema.MaxLength {
			return fmt.Errorf("%s exceeds maxLength", path)
		}
		if owner.LooksSecret(text) || hasCredentialMarker(text) {
			return fmt.Errorf("%s contains secret-like content", path)
		}
	case "boolean":
		if _, ok := value.(bool); !ok {
			return fmt.Errorf("%s must be a boolean", path)
		}
	case "integer":
		number, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("%s must be an integer", path)
		}
		integer, err := number.Int64()
		if err != nil {
			return fmt.Errorf("%s must be an integer", path)
		}
		asFloat := float64(integer)
		if !withinNumberBounds(schema, asFloat) {
			return fmt.Errorf("%s is outside numeric bounds", path)
		}
	case "number":
		number, ok := value.(json.Number)
		if !ok {
			return fmt.Errorf("%s must be a number", path)
		}
		parsed, err := number.Float64()
		if err != nil || math.IsInf(parsed, 0) || math.IsNaN(parsed) {
			return fmt.Errorf("%s must be a finite number", path)
		}
		if !withinNumberBounds(schema, parsed) {
			return fmt.Errorf("%s is outside numeric bounds", path)
		}
	}
	return nil
}

func validateConfigurationName(name string) error {
	if name == "" || len(name) > maxIdentifierBytes {
		return errors.New("configuration property name is empty or oversized")
	}
	if hasCredentialMarker(name) || owner.LooksSecret(name) {
		return fmt.Errorf("configuration property %q is credential-like", name)
	}
	for _, character := range name {
		if !(character == '-' || character == '_' ||
			character >= 'a' && character <= 'z' ||
			character >= 'A' && character <= 'Z' ||
			character >= '0' && character <= '9') {
			return fmt.Errorf("configuration property %q contains unsupported characters", name)
		}
	}
	return nil
}

func hasCredentialMarker(value string) bool {
	lower := strings.ToLower(value)
	for _, prefix := range []string{"sk-", "ghp_", "github_pat_", "xoxb-", "xoxp-"} {
		if strings.HasPrefix(lower, prefix) {
			return true
		}
	}
	for _, marker := range []string{"credential", "password", "passwd", "secret", "api_key", "apikey", "access_token", "private_key", "id_rsa", ".env"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func withinNumberBounds(schema configurationSchema, value float64) bool {
	return (schema.Minimum == nil || value >= *schema.Minimum) &&
		(schema.Maximum == nil || value <= *schema.Maximum)
}
