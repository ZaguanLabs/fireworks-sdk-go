package fireworks

import (
	"bytes"
	"encoding/json"
	"reflect"
	"strings"
	"time"
)

func openapiMarshal(value any) ([]byte, error) {
	value = transformOpenAPIJSONValue(value)
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	if len(out) > 0 && out[len(out)-1] == '\n' {
		out = out[:len(out)-1]
	}
	return append([]byte(nil), out...), nil
}

func transformOpenAPIJSONValue(value any) any {
	return transformOpenAPIJSONReflect(reflect.ValueOf(value))
}

func transformOpenAPIJSONReflect(value reflect.Value) any {
	if !value.IsValid() {
		return nil
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil
		}
		return transformOpenAPIJSONReflect(value.Elem())
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil
		}
		return transformOpenAPIJSONReflect(value.Elem())
	}
	if value.Type() == timeType {
		return formatPythonISOTime(value.Interface().(time.Time))
	}
	if value.CanInterface() && value.Type().Implements(jsonMarshalerType) {
		return value.Interface()
	}
	switch value.Kind() {
	case reflect.Map:
		if value.IsNil() {
			return nil
		}
		out := make(map[string]any, value.Len())
		iter := value.MapRange()
		for iter.Next() {
			key := iter.Key()
			if key.Kind() != reflect.String {
				return value.Interface()
			}
			out[key.String()] = transformOpenAPIJSONReflect(iter.Value())
		}
		return out
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return nil
		}
		out := make([]any, value.Len())
		for i := 0; i < value.Len(); i++ {
			out[i] = transformOpenAPIJSONReflect(value.Index(i))
		}
		return out
	case reflect.Struct:
		out := make(map[string]any)
		for i := 0; i < value.NumField(); i++ {
			field := value.Type().Field(i)
			if !field.IsExported() {
				continue
			}
			name, omitEmpty := jsonFieldName(field)
			if name == "" || name == "-" {
				continue
			}
			fieldValue := value.Field(i)
			if omitEmpty && isJSONEmptyValue(fieldValue) {
				continue
			}
			out[name] = transformOpenAPIJSONReflect(fieldValue)
		}
		return out
	default:
		return value.Interface()
	}
}

func jsonFieldName(field reflect.StructField) (string, bool) {
	tag := field.Tag.Get("json")
	if tag == "" {
		return field.Name, false
	}
	name := tag
	omitEmpty := false
	if comma := strings.IndexByte(tag, ','); comma >= 0 {
		name = tag[:comma]
		for _, option := range splitJSONTagOptions(tag[comma+1:]) {
			if option == "omitempty" {
				omitEmpty = true
				break
			}
		}
	}
	if name == "" {
		name = field.Name
	}
	return name, omitEmpty
}

func splitJSONTagOptions(options string) []string {
	if options == "" {
		return nil
	}
	var out []string
	start := 0
	for i := 0; i <= len(options); i++ {
		if i == len(options) || options[i] == ',' {
			out = append(out, options[start:i])
			start = i + 1
		}
	}
	return out
}

func isJSONEmptyValue(value reflect.Value) bool {
	switch value.Kind() {
	case reflect.Array, reflect.Map, reflect.Slice, reflect.String:
		return value.Len() == 0
	case reflect.Bool:
		return !value.Bool()
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return value.Int() == 0
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64, reflect.Uintptr:
		return value.Uint() == 0
	case reflect.Float32, reflect.Float64:
		return value.Float() == 0
	case reflect.Interface, reflect.Pointer:
		return value.IsNil()
	default:
		return false
	}
}

func formatPythonISOTime(value time.Time) string {
	format := "2006-01-02T15:04:05"
	if value.Nanosecond() != 0 {
		format += ".999999999"
	}
	return value.Format(format + "-07:00")
}

var timeType = reflect.TypeOf(time.Time{})

var jsonMarshalerType = reflect.TypeOf((*json.Marshaler)(nil)).Elem()
