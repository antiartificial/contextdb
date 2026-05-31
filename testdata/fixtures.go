package testdata

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"strings"
)

//go:embed fixtures/retry-fatigue-presets.schema.json
var RetryFatiguePresetSchemaJSON []byte

type RetryFatiguePresetSchema struct {
	Items struct {
		AdditionalProperties bool           `json:"additionalProperties"`
		Required             []string       `json:"required"`
		Properties           map[string]any `json:"properties"`
	} `json:"items"`
	StableOrder []string `json:"x-contextdb-stable-order"`
}

func LoadRetryFatiguePresetSchema() (RetryFatiguePresetSchema, error) {
	var schema RetryFatiguePresetSchema
	if err := json.Unmarshal(RetryFatiguePresetSchemaJSON, &schema); err != nil {
		return RetryFatiguePresetSchema{}, err
	}
	return schema, nil
}

func ValidateRetryFatiguePresetPayload(payload []map[string]any) error {
	schema, err := LoadRetryFatiguePresetSchema()
	if err != nil {
		return err
	}
	if schema.Items.AdditionalProperties {
		return fmt.Errorf("retry fatigue preset schema must disallow additional properties")
	}
	if len(payload) != len(schema.StableOrder) {
		return fmt.Errorf("retry fatigue preset count = %d, want %d", len(payload), len(schema.StableOrder))
	}
	for i, preset := range payload {
		name, ok := preset["name"].(string)
		if !ok {
			return fmt.Errorf("preset %d missing string name", i)
		}
		if name != schema.StableOrder[i] {
			return fmt.Errorf("preset %d name = %q, want %q", i, name, schema.StableOrder[i])
		}
		for key, value := range preset {
			if _, ok := schema.Items.Properties[key]; !ok {
				return fmt.Errorf("preset %q has unexpected field %q", name, key)
			}
			if _, ok := value.(string); !ok {
				return fmt.Errorf("preset %q field %q is %T, want string", name, key, value)
			}
		}
		for _, field := range schema.Items.Required {
			value, ok := preset[field].(string)
			if !ok {
				return fmt.Errorf("preset %q missing required string field %q", name, field)
			}
			if strings.TrimSpace(value) == "" {
				return fmt.Errorf("preset %q required field %q is empty", name, field)
			}
		}
	}
	return nil
}
