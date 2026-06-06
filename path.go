package fireworks

import (
	"fmt"
	"regexp"
	"strings"
)

var (
	pathTemplatePlaceholderRe = regexp.MustCompile(`\{(\w+)\}`)
	dotSegmentRe              = regexp.MustCompile(`^(?:\.|%2[eE]){1,2}$`)
)

func pathTemplate(template string, rawValues any) (string, error) {
	values := pathTemplateValues(rawValues)
	rest := template
	var fragmentTemplate *string
	var queryTemplate *string

	if before, after, ok := strings.Cut(rest, "#"); ok {
		rest = before
		fragmentTemplate = &after
	}
	if before, after, ok := strings.Cut(rest, "?"); ok {
		rest = before
		queryTemplate = &after
	}

	pathResult, err := interpolatePathTemplatePart(rest, values, quotePathSegmentPart)
	if err != nil {
		return "", err
	}
	for _, segment := range strings.Split(pathResult, "/") {
		if dotSegmentRe.MatchString(segment) {
			return "", fmt.Errorf("fireworks: constructed path %q contains dot-segment %q which is not allowed", pathResult, segment)
		}
	}

	result := pathResult
	if queryTemplate != nil {
		query, err := interpolatePathTemplatePart(*queryTemplate, values, quoteQueryPart)
		if err != nil {
			return "", err
		}
		result += "?" + query
	}
	if fragmentTemplate != nil {
		fragment, err := interpolatePathTemplatePart(*fragmentTemplate, values, quoteFragmentPart)
		if err != nil {
			return "", err
		}
		result += "#" + fragment
	}
	return result, nil
}

func pathTemplateValues(rawValues any) map[string]any {
	switch v := rawValues.(type) {
	case nil:
		return nil
	case map[string]any:
		return v
	case JSON:
		return map[string]any(v)
	default:
		return nil
	}
}

func interpolatePathTemplatePart(template string, values map[string]any, quoter func(string) string) (string, error) {
	matches := pathTemplatePlaceholderRe.FindAllStringSubmatchIndex(template, -1)
	if len(matches) == 0 {
		return template, nil
	}

	var out strings.Builder
	last := 0
	for _, match := range matches {
		out.WriteString(template[last:match[0]])
		name := template[match[2]:match[3]]
		value, ok := values[name]
		if !ok {
			return "", fmt.Errorf("fireworks: a value for placeholder {%s} was not provided", name)
		}
		switch v := value.(type) {
		case nil:
			out.WriteString("null")
		case bool:
			if v {
				out.WriteString("true")
			} else {
				out.WriteString("false")
			}
		default:
			out.WriteString(quoter(fmt.Sprint(v)))
		}
		last = match[1]
	}
	out.WriteString(template[last:])
	return out.String(), nil
}

func quotePathSegmentPart(value string) string {
	return quoteURIComponent(value, "!$&'()*+,;=:@")
}

func quoteQueryPart(value string) string {
	return quoteURIComponent(value, "!$'()*+,;:@/?")
}

func quoteFragmentPart(value string) string {
	return quoteURIComponent(value, "!$&'()*+,;=:@/?")
}

func quoteURIComponent(value, safe string) string {
	var out strings.Builder
	for i := 0; i < len(value); i++ {
		b := value[i]
		if isURIUnreserved(b) || strings.ContainsRune(safe, rune(b)) {
			out.WriteByte(b)
			continue
		}
		out.WriteByte('%')
		out.WriteByte(upperHex[b>>4])
		out.WriteByte(upperHex[b&0x0f])
	}
	return out.String()
}

func isURIUnreserved(b byte) bool {
	return (b >= 'A' && b <= 'Z') ||
		(b >= 'a' && b <= 'z') ||
		(b >= '0' && b <= '9') ||
		b == '-' || b == '.' || b == '_' || b == '~'
}

const upperHex = "0123456789ABCDEF"
