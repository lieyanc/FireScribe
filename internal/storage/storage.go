package storage

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

type Storage struct {
	Root string
}

type StoredFile struct {
	OriginalName string
	MimeType     string
	ByteSize     int64
	SHA256       string
	RelativePath string
	AbsPath      string
}

func New(root string) (*Storage, error) {
	dirs := []string{
		root,
		filepath.Join(root, "originals"),
		filepath.Join(root, "pages"),
		filepath.Join(root, "derived"),
		filepath.Join(root, "thumbs"),
		filepath.Join(root, "exports"),
		filepath.Join(root, "tmp"),
	}
	for _, dir := range dirs {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return nil, err
		}
	}
	return &Storage{Root: root}, nil
}

func (s *Storage) StoreOriginal(originalName string, r io.Reader) (StoredFile, error) {
	temp, err := os.CreateTemp(filepath.Join(s.Root, "tmp"), "upload-*")
	if err != nil {
		return StoredFile{}, err
	}
	tempPath := temp.Name()
	defer os.Remove(tempPath)

	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(temp, hash), r)
	if closeErr := temp.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return StoredFile{}, err
	}

	sum := hex.EncodeToString(hash.Sum(nil))
	ext := normalizedExt(originalName)
	rel := filepath.Join("originals", sum[0:2], sum[2:4], sum+ext)
	abs := filepath.Join(s.Root, rel)
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return StoredFile{}, err
	}
	if _, err := os.Stat(abs); os.IsNotExist(err) {
		if err := os.Rename(tempPath, abs); err != nil {
			return StoredFile{}, err
		}
	}

	mimeType := detectMime(abs, ext)
	return StoredFile{
		OriginalName: originalName,
		MimeType:     mimeType,
		ByteSize:     size,
		SHA256:       sum,
		RelativePath: filepath.ToSlash(rel),
		AbsPath:      abs,
	}, nil
}

func (s *Storage) CopyTo(relativePath string, sourcePath string, originalName string) (StoredFile, error) {
	abs := filepath.Join(s.Root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return StoredFile{}, err
	}

	in, err := os.Open(sourcePath)
	if err != nil {
		return StoredFile{}, err
	}
	defer in.Close()

	out, err := os.Create(abs)
	if err != nil {
		return StoredFile{}, err
	}
	hash := sha256.New()
	size, err := io.Copy(io.MultiWriter(out, hash), in)
	if closeErr := out.Close(); closeErr != nil && err == nil {
		err = closeErr
	}
	if err != nil {
		return StoredFile{}, err
	}

	ext := normalizedExt(originalName)
	return StoredFile{
		OriginalName: originalName,
		MimeType:     detectMime(abs, ext),
		ByteSize:     size,
		SHA256:       hex.EncodeToString(hash.Sum(nil)),
		RelativePath: filepath.ToSlash(relativePath),
		AbsPath:      abs,
	}, nil
}

func (s *Storage) Inspect(relativePath string, originalName string) (StoredFile, error) {
	abs := filepath.Join(s.Root, filepath.FromSlash(relativePath))
	f, err := os.Open(abs)
	if err != nil {
		return StoredFile{}, err
	}
	defer f.Close()

	hash := sha256.New()
	size, err := io.Copy(hash, f)
	if err != nil {
		return StoredFile{}, err
	}
	ext := normalizedExt(originalName)
	return StoredFile{
		OriginalName: originalName,
		MimeType:     detectMime(abs, ext),
		ByteSize:     size,
		SHA256:       hex.EncodeToString(hash.Sum(nil)),
		RelativePath: filepath.ToSlash(relativePath),
		AbsPath:      abs,
	}, nil
}

func (s *Storage) Abs(relativePath string) string {
	return filepath.Join(s.Root, filepath.FromSlash(relativePath))
}

func normalizedExt(name string) string {
	ext := strings.ToLower(filepath.Ext(name))
	if ext == ".jpeg" {
		return ".jpg"
	}
	if ext == "" {
		return ""
	}
	clean := make([]rune, 0, len(ext))
	for _, r := range ext {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' {
			clean = append(clean, r)
		}
	}
	return string(clean)
}

func detectMime(path, ext string) string {
	if byExt := mime.TypeByExtension(ext); byExt != "" {
		return byExt
	}
	f, err := os.Open(path)
	if err != nil {
		return "application/octet-stream"
	}
	defer f.Close()

	var buf [512]byte
	n, _ := f.Read(buf[:])
	if n == 0 {
		return "application/octet-stream"
	}
	return http.DetectContentType(buf[:n])
}

func PageImageRel(documentID string, pageNo int, ext string) string {
	return filepath.ToSlash(filepath.Join("pages", documentID, fmt.Sprintf("page-%04d%s", pageNo, normalizedExt(ext))))
}

func ThumbnailRel(documentID string, pageNo int) string {
	return filepath.ToSlash(filepath.Join("thumbs", documentID, fmt.Sprintf("page-%04d.jpg", pageNo)))
}

func EnhancedPageRel(documentID string, pageNo int, resultID string) string {
	return filepath.ToSlash(filepath.Join("derived", documentID, fmt.Sprintf("page-%04d", pageNo), resultID+".png"))
}

func ExportRel(exportID, format string) string {
	return filepath.ToSlash(filepath.Join("exports", exportID, "output."+format))
}
