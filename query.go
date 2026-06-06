package fireworks

import (
	"fmt"
	"net/url"
	"reflect"
	"sort"
	"strings"
)

type queryPair struct {
	Key   string
	Value string
}

func stringifyQuery(params any, arrayFormat, nestedFormat string) (string, error) {
	if arrayFormat == "" {
		arrayFormat = "repeat"
	}
	if nestedFormat == "" {
		nestedFormat = "brackets"
	}
	items, err := stringifyQueryItems("", params, arrayFormat, nestedFormat)
	if err != nil {
		return "", err
	}
	values := make(url.Values)
	for _, item := range items {
		values.Add(item.Key, item.Value)
	}
	return values.Encode(), nil
}

func stringifyQueryItems(key string, value any, arrayFormat, nestedFormat string) ([]queryPair, error) {
	switch v := value.(type) {
	case nil:
		return nil, nil
	case map[string]any:
		return stringifyQueryMap(key, v, arrayFormat, nestedFormat)
	case JSON:
		return stringifyQueryMap(key, map[string]any(v), arrayFormat, nestedFormat)
	case []any:
		return stringifyQuerySlice(key, v, arrayFormat, nestedFormat)
	case string:
		if v == "" || key == "" {
			return nil, nil
		}
		return []queryPair{{Key: key, Value: v}}, nil
	case bool:
		if key == "" {
			return nil, nil
		}
		return []queryPair{{Key: key, Value: queryPrimitiveString(v)}}, nil
	default:
		reflected := reflect.ValueOf(value)
		if reflected.IsValid() && reflected.Kind() == reflect.Map {
			return stringifyQueryReflectMap(key, reflected, arrayFormat, nestedFormat)
		}
		if reflected.IsValid() && (reflected.Kind() == reflect.Slice || reflected.Kind() == reflect.Array) {
			items := make([]any, 0, reflected.Len())
			for i := 0; i < reflected.Len(); i++ {
				items = append(items, reflected.Index(i).Interface())
			}
			return stringifyQuerySlice(key, items, arrayFormat, nestedFormat)
		}
		if key == "" {
			return nil, nil
		}
		return []queryPair{{Key: key, Value: queryPrimitiveString(v)}}, nil
	}
}

func stringifyQueryMap(prefix string, values map[string]any, arrayFormat, nestedFormat string) ([]queryPair, error) {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	var pairs []queryPair
	for _, key := range keys {
		nextKey := key
		if prefix != "" {
			if nestedFormat == "dots" {
				nextKey = prefix + "." + key
			} else {
				nextKey = prefix + "[" + key + "]"
			}
		}
		items, err := stringifyQueryItems(nextKey, values[key], arrayFormat, nestedFormat)
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, items...)
	}
	return pairs, nil
}

func stringifyQueryReflectMap(prefix string, values reflect.Value, arrayFormat, nestedFormat string) ([]queryPair, error) {
	keys := values.MapKeys()
	sort.Slice(keys, func(i, j int) bool {
		return fmt.Sprint(keys[i].Interface()) < fmt.Sprint(keys[j].Interface())
	})

	var pairs []queryPair
	for _, keyValue := range keys {
		key := fmt.Sprint(keyValue.Interface())
		nextKey := key
		if prefix != "" {
			if nestedFormat == "dots" {
				nextKey = prefix + "." + key
			} else {
				nextKey = prefix + "[" + key + "]"
			}
		}
		items, err := stringifyQueryItems(nextKey, values.MapIndex(keyValue).Interface(), arrayFormat, nestedFormat)
		if err != nil {
			return nil, err
		}
		pairs = append(pairs, items...)
	}
	return pairs, nil
}

func stringifyQuerySlice(key string, values []any, arrayFormat, nestedFormat string) ([]queryPair, error) {
	switch arrayFormat {
	case "comma":
		parts := make([]string, 0, len(values))
		for _, item := range values {
			if item == nil {
				continue
			}
			parts = append(parts, queryPrimitiveString(item))
		}
		if key == "" {
			return nil, nil
		}
		return []queryPair{{Key: key, Value: strings.Join(parts, ",")}}, nil
	case "repeat":
		var pairs []queryPair
		for _, item := range values {
			items, err := stringifyQueryItems(key, item, arrayFormat, nestedFormat)
			if err != nil {
				return nil, err
			}
			pairs = append(pairs, items...)
		}
		return pairs, nil
	case "indices":
		var pairs []queryPair
		for i, item := range values {
			items, err := stringifyQueryItems(fmt.Sprintf("%s[%d]", key, i), item, arrayFormat, nestedFormat)
			if err != nil {
				return nil, err
			}
			pairs = append(pairs, items...)
		}
		return pairs, nil
	case "brackets":
		var pairs []queryPair
		for _, item := range values {
			items, err := stringifyQueryItems(key+"[]", item, arrayFormat, nestedFormat)
			if err != nil {
				return nil, err
			}
			pairs = append(pairs, items...)
		}
		return pairs, nil
	default:
		return nil, fmt.Errorf("fireworks: unknown array_format value: %s, choose from brackets, comma, indices, repeat", arrayFormat)
	}
}
