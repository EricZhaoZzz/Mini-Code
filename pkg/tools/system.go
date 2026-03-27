package tools

import (
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/go-playground/validator/v10"
	"github.com/invopop/jsonschema"
)

var toolValidator = validator.New()

// generateSchema 通过 jsonschema 反射生成 OpenAI function calling 使用的 JSON Schema。
func generateSchema(v interface{}) interface{} {
	reflector := jsonschema.Reflector{
		Anonymous:                  true,
		DoNotReference:             true,
		AllowAdditionalProperties:  false,
		RequiredFromJSONSchemaTags: true,
	}

	schema := reflector.Reflect(v)
	data, err := json.Marshal(schema)
	if err != nil {
		return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	}

	delete(raw, "$schema")
	delete(raw, "$id")
	return raw
}

func validateToolArguments(v interface{}) error {
	if err := toolValidator.Struct(v); err != nil {
		validationErrors, ok := err.(validator.ValidationErrors)
		if !ok || len(validationErrors) == 0 {
			return err
		}

		validationError := validationErrors[0]
		field := jsonFieldName(validationError.StructField(), reflect.TypeOf(v))
		switch validationError.Tag() {
		case "required":
			return fmt.Errorf("%s 不能为空", field)
		default:
			return fmt.Errorf("%s 参数无效", field)
		}
	}

	return nil
}

func jsonFieldName(structField string, t reflect.Type) string {
	if t.Kind() == reflect.Ptr {
		t = t.Elem()
	}
	if t.Kind() != reflect.Struct {
		return strings.ToLower(structField)
	}

	field, ok := t.FieldByName(structField)
	if !ok {
		return strings.ToLower(structField)
	}

	name := strings.Split(field.Tag.Get("json"), ",")[0]
	if name == "" {
		return strings.ToLower(structField)
	}
	return name
}
