package tools

import (
	"reflect"
	"strings"
)

// generateSchema 通过反射将参数结构体转为 OpenAI function calling 所需的 JSON Schema。
func generateSchema(v interface{}) interface{} {
	properties := map[string]interface{}{}
	required := []string{}

	t := reflect.TypeOf(v)
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return map[string]interface{}{"type": "object", "properties": properties}
	}

	for i := 0; i < t.NumField(); i++ {
		field := t.Field(i)
		name := field.Tag.Get("json")
		if name == "" {
			name = strings.ToLower(field.Name)
		}
		name = strings.Split(name, ",")[0]
		properties[name] = map[string]interface{}{"type": "string"}
		required = append(required, name)
	}

	return map[string]interface{}{
		"type":       "object",
		"properties": properties,
		"required":   required,
	}
}
