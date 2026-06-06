package fireworks

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"reflect"
	"strings"
	"time"
)

func openapiMarshal(value any) ([]byte, error) {
	value, err := transformOpenAPIJSONValue(value)
	if err != nil {
		return nil, err
	}
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

func transformOpenAPIJSONValue(value any) (any, error) {
	return transformOpenAPIJSONReflect(reflect.ValueOf(value))
}

func transformOpenAPIJSONReflect(value reflect.Value) (any, error) {
	if !value.IsValid() {
		return nil, nil
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return nil, nil
		}
		return transformOpenAPIJSONReflect(value.Elem())
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return nil, nil
		}
		return transformOpenAPIJSONReflect(value.Elem())
	}
	if value.Type() == timeType {
		return formatPythonISOTime(value.Interface().(time.Time)), nil
	}
	if value.CanInterface() && value.Type().Implements(jsonMarshalerType) {
		return value.Interface(), nil
	}
	switch value.Kind() {
	case reflect.Map:
		if value.IsNil() {
			return nil, nil
		}
		base64Source := hasStringMapField(value, "type", "base64")
		out := make(map[string]any, value.Len())
		iter := value.MapRange()
		for iter.Next() {
			key := iter.Key()
			if key.Kind() != reflect.String {
				return value.Interface(), nil
			}
			if base64Source && key.String() == "data" {
				encoded, ok, err := base64FileInput(iter.Value())
				if err != nil {
					return nil, err
				}
				if ok {
					out[key.String()] = encoded
					continue
				}
			}
			item, err := transformOpenAPIJSONReflect(iter.Value())
			if err != nil {
				return nil, err
			}
			out[key.String()] = item
		}
		return out, nil
	case reflect.Slice, reflect.Array:
		if value.Kind() == reflect.Slice && value.IsNil() {
			return nil, nil
		}
		if value.Type().Elem().Kind() == reflect.Uint8 {
			return value.Interface(), nil
		}
		out := make([]any, value.Len())
		for i := 0; i < value.Len(); i++ {
			item, err := transformOpenAPIJSONReflect(value.Index(i))
			if err != nil {
				return nil, err
			}
			out[i] = item
		}
		return out, nil
	case reflect.Struct:
		base64Source := hasStringStructField(value, "type", "base64")
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
			if base64Source && name == "data" {
				encoded, ok, err := base64FileInput(fieldValue)
				if err != nil {
					return nil, err
				}
				if ok {
					out[name] = encoded
					continue
				}
			}
			item, err := transformOpenAPIJSONReflect(fieldValue)
			if err != nil {
				return nil, err
			}
			out[name] = item
		}
		return out, nil
	default:
		return value.Interface(), nil
	}
}

func base64FileInput(value reflect.Value) (string, bool, error) {
	if !value.IsValid() {
		return "", false, nil
	}
	if value.Kind() == reflect.Interface {
		if value.IsNil() {
			return "", false, nil
		}
		return base64FileInput(value.Elem())
	}
	if value.CanInterface() {
		switch v := value.Interface().(type) {
		case File:
			return base64Reader(v.Content)
		case *File:
			if v == nil {
				return "", false, nil
			}
			return base64Reader(v.Content)
		case io.Reader:
			return base64Reader(v)
		}
	}
	if value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return "", false, nil
		}
		return base64FileInput(value.Elem())
	}
	if value.Kind() == reflect.String {
		return "", false, nil
	}
	if value.Kind() == reflect.Slice && value.Type().Elem().Kind() == reflect.Uint8 {
		return base64.StdEncoding.EncodeToString(value.Bytes()), true, nil
	}
	return "", false, nil
}

func base64Reader(reader io.Reader) (string, bool, error) {
	if reader == nil {
		return "", true, fmt.Errorf("fireworks: base64 file input has nil content")
	}
	data, err := io.ReadAll(reader)
	if err != nil {
		return "", true, fmt.Errorf("fireworks: read base64 file input: %w", err)
	}
	return base64.StdEncoding.EncodeToString(data), true, nil
}

func hasStringMapField(value reflect.Value, key, want string) bool {
	if value.Kind() != reflect.Map || value.Type().Key().Kind() != reflect.String {
		return false
	}
	mapKey := reflect.New(value.Type().Key()).Elem()
	mapKey.SetString(key)
	item := value.MapIndex(mapKey)
	return stringReflectValue(item) == want
}

func hasStringStructField(value reflect.Value, key, want string) bool {
	if value.Kind() != reflect.Struct {
		return false
	}
	for i := 0; i < value.NumField(); i++ {
		field := value.Type().Field(i)
		if !field.IsExported() {
			continue
		}
		name, _ := jsonFieldName(field)
		if name == key && stringReflectValue(value.Field(i)) == want {
			return true
		}
	}
	return false
}

func stringReflectValue(value reflect.Value) string {
	if !value.IsValid() {
		return ""
	}
	if value.Kind() == reflect.Interface || value.Kind() == reflect.Pointer {
		if value.IsNil() {
			return ""
		}
		return stringReflectValue(value.Elem())
	}
	if value.Kind() == reflect.String {
		return value.String()
	}
	return ""
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
