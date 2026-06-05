package fireworks

import (
	"bytes"
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
