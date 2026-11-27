// Kimi K2 TypeScript tool declarations, adapted from Moonshot AI's encoder.
// See licenses/Kimi-K2.txt.
package sdk

import (
	"encoding/json"
	"fmt"
	"slices"
	"sort"
	"strings"
)

func k2Keys(m map[string]any) []string {
	result := make([]string, 0, len(m))
	for key := range m {
		result = append(result, key)
	}
	sort.Strings(result)
	return result
}
func k2Description(desc, indent string) string {
	lines := strings.Split(desc, "\n")
	for i, line := range lines {
		if line != "" {
			lines[i] = indent + "// " + line
		}
	}
	return strings.Join(lines, "\n")
}
func k2Repr(v any, nested bool) string {
	switch x := v.(type) {
	case string:
		if nested {
			quote := "'"
			if strings.Contains(x, "'") && !strings.Contains(x, "\"") {
				quote = "\""
			}
			return quote + strings.NewReplacer("\\", "\\\\", quote, "\\"+quote, "\n", "\\n", "\r", "\\r", "\t", "\\t").Replace(x) + quote
		}
		return x
	case nil:
		return "None"
	case bool:
		if x {
			return "True"
		}
		return "False"
	case []any:
		var values []string
		for _, value := range x {
			values = append(values, k2Repr(value, true))
		}
		return "[" + strings.Join(values, ", ") + "]"
	case map[string]any:
		var pairs []string
		for _, k := range k2Keys(x) {
			pairs = append(pairs, k2Repr(k, true)+": "+k2Repr(x[k], true))
		}
		return "{" + strings.Join(pairs, ", ") + "}"
	default:
		return fmt.Sprint(v)
	}
}

type k2Registry struct {
	defs map[string]any
	self bool
}

func k2Docs(schema map[string]any, indent string) string {
	var b strings.Builder
	if desc := kimiMapString(schema, "description"); desc != "" {
		b.WriteString(k2Description(desc, indent) + "\n")
	}
	var constraints []string
	switch schema["type"] {
	case "string":
		constraints = []string{"maxLength", "minLength", "pattern"}
	case "number", "integer":
		constraints = []string{"maximum", "minimum"}
	case "array":
		constraints = []string{"maxItems", "minItems"}
	}
	var fields []string
	for _, name := range constraints {
		if value, ok := schema[name]; ok {
			fields = append(fields, name+": "+k2Repr(value, false))
		}
	}
	if len(fields) > 0 {
		b.WriteString(indent + "// " + strings.Join(fields, ", ") + "\n")
	}
	return b.String()
}

func (r *k2Registry) object(m map[string]any, indent string, root bool) (string, error) {
	if defs, ok := m["$defs"].(map[string]any); ok {
		for k, v := range defs {
			r.defs[k] = v
		}
	}
	req := map[string]bool{}
	for _, k := range asK2List(m["required"]) {
		if name, ok := k.(string); ok {
			req[name] = true
		}
	}
	child := indent + "  "
	props, _ := m["properties"].(map[string]any)
	keys := k2Keys(props)
	if !root {
		sort.SliceStable(keys, func(i, j int) bool { return req[keys[i]] && !req[keys[j]] })
	}
	fields := make([]string, 0, len(keys))
	for _, key := range keys {
		prop := props[key]
		schema, _ := prop.(map[string]any)
		typ, err := r.format(prop, child)
		if err != nil {
			return "", err
		}
		field := k2Docs(schema, child)
		if value := schema["default"]; value != nil {
			repr := k2Repr(value, false)
			switch value.(type) {
			case string, map[string]any, []any:
				repr, err = k2JSON(value, false)
				if err != nil {
					return "", err
				}
			}
			field += child + "// Default: " + repr + "\n"
		}
		name := key
		if !req[key] {
			name += "?"
		}
		fields = append(fields, field+child+name+": "+typ)
	}
	if ap := m["additionalProperties"]; ap != nil && !root {
		typ, err := r.format(ap, child)
		if err != nil {
			return "", err
		}
		if flag, ok := ap.(bool); ok && !flag {
			typ = "never"
		}
		fields = append(fields, child+"[k: string]: "+typ)
	}
	if len(fields) == 0 {
		return "{}", nil
	}
	return "{\n" + strings.Join(fields, ",\n") + "\n" + indent + "}", nil
}

func asK2List(val any) []any {
	switch v := val.(type) {
	case []any:
		return v
	case []string:
		items := make([]any, len(v))
		for i, s := range v {
			items[i] = s
		}
		return items
	}
	return nil
}

func (r *k2Registry) format(v any, indent string) (string, error) {
	if b, ok := v.(bool); ok {
		if b {
			return "any", nil
		}
		return "null", nil
	}
	m, ok := v.(map[string]any)
	if !ok {
		return "", fmt.Errorf("invalid schema")
	}
	if ref, ok := m["$ref"].(string); ok {
		if ref == "#" {
			r.self = true
			return "parameters", nil
		}
		key, ok := strings.CutPrefix(ref, "#/$defs/")
		if !ok || r.defs[key] == nil {
			return "", fmt.Errorf("unsupported schema reference %q", ref)
		}
		return key, nil
	}
	if options := asK2List(m["anyOf"]); options != nil {
		types := make([]string, 0, len(options))
		for _, opt := range options {
			t, e := r.format(opt, indent)
			if e != nil {
				return "", e
			}
			types = append(types, t)
		}
		return strings.Join(types, " | "), nil
	}
	if options := asK2List(m["enum"]); options != nil {
		typ, _ := m["type"].(string)
		if types := asK2List(m["type"]); types != nil {
			if len(types) > 2 || (len(types) == 2 && !slices.Contains(types, any("null"))) {
				return "", fmt.Errorf("unsupported enum types")
			}
			for _, value := range types {
				if value != "null" {
					typ, _ = value.(string)
				}
			}
		}
		values := make([]string, 0, len(options))
		for _, opt := range options {
			if opt == nil {
				values = append(values, "None")
				continue
			}
			match := true
			switch typ {
			case "string":
				_, match = opt.(string)
			case "boolean":
				_, match = opt.(bool)
			case "number", "integer":
				n, ok := opt.(json.Number)
				match = ok
				if match && typ == "integer" {
					_, err := n.Int64()
					match = err == nil
				}
				if _, b := opt.(bool); b {
					match = true
				}
			}
			if !match {
				return "", fmt.Errorf("invalid enum value for %s", typ)
			}
			if s, ok := opt.(string); ok {
				values = append(values, "\""+s+"\"")
			} else {
				values = append(values, k2Repr(opt, false))
			}
		}
		return strings.Join(values, " | "), nil
	}
	typ := m["type"]
	if types := asK2List(typ); types != nil {
		names := make([]string, 0, len(types))
		for _, entry := range types {
			s, _ := entry.(string)
			switch s {
			case "integer":
				s = "number"
			case "object":
				s = "{}"
			case "array":
				s = "Array<any>"
			case "string", "number", "boolean", "null":
			default:
				return "", fmt.Errorf("invalid union type")
			}
			names = append(names, s)
		}
		return strings.Join(names, " | "), nil
	}
	if s, ok := typ.(string); ok {
		switch s {
		case "integer":
			return "number", nil
		case "object":
			return r.object(m, indent, false)
		case "array":
			items := m["items"]
			if items == nil || items == false {
				return "Array<any>", nil
			}
			schema, _ := items.(map[string]any)
			doc := k2Docs(schema, indent+"  ")
			at := indent
			if doc != "" {
				at += "  "
			}
			t, err := r.format(items, at)
			if err != nil {
				return "", err
			}
			if doc != "" {
				return "Array<\n" + doc + at + t + "\n" + indent + ">", nil
			}
			return "Array<" + t + ">", nil
		default:
			return s, nil
		}
	}
	if len(m) == 0 {
		return "any", nil
	}
	return "", fmt.Errorf("missing schema type")
}

func k2ToolDeclarations(tools []map[string]any) (string, error) {
	// Canonicalize Go slice/map representations to the native JSON shape.
	data, err := json.Marshal(tools)
	if err != nil {
		return "", err
	}
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.UseNumber()
	var canonical []map[string]any
	if err := d.Decode(&canonical); err != nil {
		return "", err
	}
	var result []string
	for _, tool := range canonical {
		if tool["type"] != "function" {
			continue
		}
		fn, _ := tool["function"].(map[string]any)
		if len(fn) == 0 {
			continue
		}
		registry := k2Registry{defs: map[string]any{}}
		parameters, _ := fn["parameters"].(map[string]any)
		signature, err := registry.object(parameters, "", false)
		if err != nil {
			return "", err
		}
		var lines []string
		if registry.self {
			root, err := registry.object(parameters, "", true)
			if err != nil {
				return "", err
			}
			lines = append(lines, "interface parameters "+root)
			signature = "parameters"
		}
		for _, key := range k2Keys(registry.defs) {
			v := registry.defs[key]
			s, err := registry.format(v, "")
			if err != nil {
				return "", err
			}
			schema, _ := v.(map[string]any)
			if desc := kimiMapString(schema, "description"); desc != "" {
				lines = append(lines, k2Description(desc, ""))
			}
			lines = append(lines, "interface "+key+" "+s)
		}
		if desc := kimiMapString(fn, "description"); desc != "" {
			lines = append(lines, k2Description(desc, ""))
		}
		name, ok := fn["name"].(string)
		if !ok {
			name = "function"
		}
		lines = append(lines, "type "+name+" = (_: "+signature+") => any;")
		result = append(result, strings.Join(lines, "\n"))
	}
	if len(result) == 0 {
		return "", nil
	}
	return "# Tools\n\n## functions\nnamespace functions {\n" + strings.Join(result, "\n") + "\n}\n", nil
}
