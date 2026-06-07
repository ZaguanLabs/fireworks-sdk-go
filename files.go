package fireworks

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"

	fwtypes "github.com/ZaguanLabs/fireworks-sdk-go/types"
)

type File struct {
	Filename    string
	Content     io.Reader
	ContentType string
}

func NewFile(filename string, content io.Reader) File {
	return File{Filename: filename, Content: content}
}

func NewFileFromBytes(filename string, content []byte) File {
	return File{Filename: filename, Content: bytes.NewReader(content)}
}

func NewFileFromPath(path string) (File, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return File{}, err
	}
	return NewFileFromBytes(filepath.Base(path), content), nil
}

func FileFromPath(path string) (File, error) {
	return NewFileFromPath(path)
}

func uploadFileFromBody(body any) (File, bool) {
	switch v := body.(type) {
	case File:
		return v, true
	case *File:
		if v == nil {
			return File{}, false
		}
		return *v, true
	case fwtypes.DatasetUploadParams:
		return fileFromValue(v.File)
	case *fwtypes.DatasetUploadParams:
		if v == nil {
			return File{}, false
		}
		return fileFromValue(v.File)
	case map[string]any:
		return fileFromValue(v["file"])
	case JSON:
		return fileFromValue(v["file"])
	case map[string]File:
		file, ok := v["file"]
		return file, ok
	default:
		return File{}, false
	}
}

func fileFromValue(value any) (File, bool) {
	switch v := value.(type) {
	case File:
		return v, true
	case *File:
		if v == nil {
			return File{}, false
		}
		return *v, true
	case []byte:
		return NewFileFromBytes("file", v), true
	case io.Reader:
		return NewFile("file", v), true
	default:
		return File{}, false
	}
}

type filePart struct {
	Key  string
	File File
}

func copyWithFilePaths(item any, paths [][]string) any {
	return copyWithFilePathsAt(item, paths, 0)
}

func copyWithFilePathsAt(item any, paths [][]string, index int) any {
	if len(paths) == 0 {
		return item
	}
	switch value := item.(type) {
	case map[string]any:
		keyPaths := make(map[string][][]string)
		for _, path := range paths {
			if index < len(path) {
				keyPaths[path[index]] = append(keyPaths[path[index]], path)
			}
		}
		if len(keyPaths) == 0 {
			return item
		}
		out := make(map[string]any, len(value))
		for key, entry := range value {
			if subpaths, ok := keyPaths[key]; ok {
				out[key] = copyWithFilePathsAt(entry, subpaths, index+1)
			} else {
				out[key] = entry
			}
		}
		return out
	case JSON:
		return copyWithFilePathsAt(map[string]any(value), paths, index)
	case []any:
		var arrayPaths [][]string
		for _, path := range paths {
			if index < len(path) && path[index] == "<array>" {
				arrayPaths = append(arrayPaths, path)
			}
		}
		if len(arrayPaths) == 0 {
			return item
		}
		out := make([]any, len(value))
		for i, entry := range value {
			out[i] = copyWithFilePathsAt(entry, arrayPaths, index+1)
		}
		return out
	default:
		return item
	}
}

func extractFiles(query map[string]any, paths [][]string, arrayFormat string) ([]filePart, error) {
	if arrayFormat == "" {
		arrayFormat = "brackets"
	}
	var files []filePart
	for _, path := range paths {
		parts, err := extractFileItems(query, path, 0, "", arrayFormat)
		if err != nil {
			return nil, err
		}
		files = append(files, parts...)
	}
	return files, nil
}

func extractFileItems(obj any, path []string, index int, flattenedKey string, arrayFormat string) ([]filePart, error) {
	if index >= len(path) {
		file, ok := fileFromValue(obj)
		if !ok {
			return nil, fmt.Errorf("fireworks: expected file content for %q", flattenedKey)
		}
		return []filePart{{Key: flattenedKey, File: file}}, nil
	}

	key := path[index]
	switch value := obj.(type) {
	case map[string]any:
		item, ok := value[key]
		if !ok {
			return nil, nil
		}
		if onlyArraySegments(path[index+1:]) {
			delete(value, key)
		}
		nextKey := key
		if flattenedKey != "" {
			nextKey = flattenedKey + "[" + key + "]"
		}
		return extractFileItems(item, path, index+1, nextKey, arrayFormat)
	case JSON:
		return extractFileItems(map[string]any(value), path, index, flattenedKey, arrayFormat)
	case []any:
		if key != "<array>" {
			return nil, nil
		}
		var files []filePart
		for i, item := range value {
			suffix, err := fileArraySuffix(arrayFormat, i)
			if err != nil {
				return nil, err
			}
			parts, err := extractFileItems(item, path, index+1, flattenedKey+suffix, arrayFormat)
			if err != nil {
				return nil, err
			}
			files = append(files, parts...)
		}
		return files, nil
	case []File:
		if key != "<array>" {
			return nil, nil
		}
		files := make([]filePart, 0, len(value))
		for i, item := range value {
			suffix, err := fileArraySuffix(arrayFormat, i)
			if err != nil {
				return nil, err
			}
			files = append(files, filePart{Key: flattenedKey + suffix, File: item})
		}
		return files, nil
	case [][]byte:
		if key != "<array>" {
			return nil, nil
		}
		files := make([]filePart, 0, len(value))
		for i, item := range value {
			suffix, err := fileArraySuffix(arrayFormat, i)
			if err != nil {
				return nil, err
			}
			files = append(files, filePart{Key: flattenedKey + suffix, File: NewFileFromBytes("file", item)})
		}
		return files, nil
	default:
		return nil, nil
	}
}

func onlyArraySegments(path []string) bool {
	for _, part := range path {
		if part != "<array>" {
			return false
		}
	}
	return true
}

func fileArraySuffix(arrayFormat string, index int) (string, error) {
	switch arrayFormat {
	case "brackets":
		return "[]", nil
	case "indices":
		return fmt.Sprintf("[%d]", index), nil
	case "repeat", "comma":
		return "", nil
	default:
		return "", fmt.Errorf("fireworks: unknown array_format value: %s, choose from brackets, comma, indices, repeat", arrayFormat)
	}
}
