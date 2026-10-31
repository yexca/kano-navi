package contentadmin

import (
	"encoding/binary"
	"errors"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/yexca/kano-navi/backend/internal/mediaformat"
)

// Profile candidates and uploads share the 15 MiB profile image limit.
const maxProfileImageBytes = 15 * 1024 * 1024

var errMediaPath = errors.New("media path rejected")

// normalizeMediaMIME keeps only the cacheable image types.
func normalizeMediaMIME(value string) string {
	normalized := strings.ToLower(jsTrim(strings.SplitN(value, ";", 2)[0]))
	if mediaformat.Extension(normalized) == "" {
		return ""
	}
	return normalized
}

// mediaNamespaceForSource maps a source label to its runtime directory.
func mediaNamespaceForSource(source string) string {
	normalized := strings.ToLower(jsTrim(source))
	switch {
	case strings.Contains(normalized, "youtube"):
		return "youtube"
	case normalized == "avatar", normalized == "banner", normalized == "upload", strings.Contains(normalized, "profile"):
		return "avatar"
	}
	return "x"
}

// cleanRelativePath validates a database-controlled cache path. Absolute
// paths, drive or stream prefixes and parent components are never accepted.
func cleanRelativePath(relative string) (string, bool) {
	normalized := strings.ReplaceAll(relative, `\`, "/")
	if strings.TrimSpace(normalized) == "" || strings.ContainsRune(normalized, 0) || strings.Contains(normalized, ":") {
		return "", false
	}
	if strings.HasPrefix(normalized, "/") || filepath.IsAbs(filepath.FromSlash(normalized)) {
		return "", false
	}
	for _, part := range strings.Split(normalized, "/") {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	return normalized, true
}

func (m *Module) namespaceRoot(namespace string) string {
	if namespace == "legacy" {
		return filepath.Join(m.dataRoot, "cache", "media")
	}
	return filepath.Join(m.dataRoot, namespace)
}

func (m *Module) allMediaRoots() []string {
	return []string{m.namespaceRoot("avatar"), m.namespaceRoot("x"), m.namespaceRoot("youtube"), m.namespaceRoot("legacy")}
}

// lexicalMediaPath maps a relative cache path into the runtime tree. Legacy
// `sha256/...` rows live under cache/media; all others name their namespace.
func (m *Module) lexicalMediaPath(relative string) (string, error) {
	normalized, ok := cleanRelativePath(relative)
	if !ok || m.dataRoot == "" {
		return "", errMediaPath
	}
	if strings.HasPrefix(normalized, "sha256/") {
		return filepath.Join(m.namespaceRoot("legacy"), filepath.FromSlash(normalized)), nil
	}
	switch strings.SplitN(normalized, "/", 2)[0] {
	case "x", "youtube", "avatar":
		return filepath.Join(m.dataRoot, filepath.FromSlash(normalized)), nil
	}
	return "", errMediaPath
}

func insideDirectory(candidate, root string) bool {
	return strings.HasPrefix(candidate, root+string(filepath.Separator))
}

// containedMediaFile resolves symlinks and requires the real file to be a
// regular file inside one of the given real roots.
func (m *Module) containedMediaFile(relative string, roots ...string) (string, error) {
	candidate, err := m.lexicalMediaPath(relative)
	if err != nil {
		return "", err
	}
	resolved, err := filepath.EvalSymlinks(candidate)
	if err != nil {
		return "", errMediaPath
	}
	for _, root := range roots {
		resolvedRoot, rootErr := filepath.EvalSymlinks(root)
		if rootErr != nil || !insideDirectory(resolved, resolvedRoot) {
			continue
		}
		info, statErr := os.Stat(resolved)
		if statErr != nil || !info.Mode().IsRegular() {
			return "", errMediaPath
		}
		return resolved, nil
	}
	return "", errMediaPath
}

// readBoundedFile reads a regular, non-empty file no larger than the profile
// limit without trusting the size reported before the read.
func readBoundedFile(filename string) ([]byte, error) {
	file, err := os.Open(filename)
	if err != nil {
		return nil, errMediaPath
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxProfileImageBytes {
		return nil, errMediaPath
	}
	content, err := io.ReadAll(io.LimitReader(file, maxProfileImageBytes+1))
	if err != nil || len(content) == 0 || len(content) > maxProfileImageBytes {
		return nil, errMediaPath
	}
	return content, nil
}

// writeRootFile publishes content at a data-relative path through a temporary
// file in the same directory. os.Root confines every step, including symlink
// races, to the runtime data directory.
func (m *Module) writeRootFile(relative string, content []byte, reuseIdentical bool) error {
	normalized, ok := cleanRelativePath(relative)
	if !ok || m.dataRoot == "" {
		return errMediaPath
	}
	root, err := os.OpenRoot(m.dataRoot)
	if err != nil {
		return errMediaPath
	}
	defer root.Close()
	target := filepath.FromSlash(normalized)
	directory := filepath.Dir(target)
	if err = root.MkdirAll(directory, 0o755); err != nil {
		return errMediaPath
	}
	// The namespace directory itself must be a real directory, matching the
	// public reader, so a planted link cannot redirect published files.
	namespace := strings.SplitN(normalized, "/", 2)[0]
	if info, statErr := root.Lstat(namespace); statErr != nil || !info.IsDir() {
		return errMediaPath
	}
	if reuseIdentical {
		if existing, openErr := root.Open(target); openErr == nil {
			info, statErr := existing.Stat()
			var current []byte
			if statErr == nil && info.Mode().IsRegular() && info.Size() == int64(len(content)) {
				current, _ = io.ReadAll(io.LimitReader(existing, maxProfileImageBytes+1))
			}
			existing.Close()
			if len(current) == len(content) && sha256Hex(current) == sha256Hex(content) {
				return nil
			}
		}
	}
	suffix, err := randomHex(16)
	if err != nil {
		return errMediaPath
	}
	temporary := filepath.Join(directory, ".part-"+suffix)
	file, err := root.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errMediaPath
	}
	defer root.Remove(temporary)
	_, writeErr := file.Write(content)
	syncErr := file.Sync()
	closeErr := file.Close()
	if writeErr != nil || syncErr != nil || closeErr != nil {
		return errMediaPath
	}
	if err = root.Rename(temporary, target); err != nil {
		return errMediaPath
	}
	return nil
}

// contentAddressedPath is the relative cache path for verified image bytes.
func contentAddressedPath(namespace, hash, extension string) string {
	return path.Join(namespace, "sha256", hash[:2], hash+"."+extension)
}

type storedImage struct {
	relativePath string
	mimeType     string
	extension    string
	sha256       string
	byteSize     int64
	width        *int64
	height       *int64
}

// storeProfileImage verifies the magic bytes against the declared type and
// writes the original bytes into the source namespace.
func (m *Module) storeProfileImage(source string, content []byte, declared, invalidMessage string) (*storedImage, error) {
	sniffed := mediaformat.Sniff(content)
	extension := mediaformat.Extension(sniffed)
	if sniffed == "" || extension == "" || (declared != "" && declared != sniffed) {
		return nil, failure(invalidMessage)
	}
	hash := sha256Hex(content)
	relative := contentAddressedPath(mediaNamespaceForSource(source), hash, extension)
	if err := m.writeRootFile(relative, content, true); err != nil {
		return nil, failure("profile media could not be stored")
	}
	width, height := imageDimensions(content, sniffed)
	return &storedImage{relativePath: relative, mimeType: sniffed, extension: extension, sha256: hash, byteSize: int64(len(content)), width: width, height: height}, nil
}

// imageDimensions probes PNG, GIF and baseline/progressive JPEG headers.
func imageDimensions(body []byte, mimeType string) (*int64, *int64) {
	pair := func(width, height uint32) (*int64, *int64) {
		w, h := int64(width), int64(height)
		return &w, &h
	}
	switch {
	case mimeType == "image/png" && len(body) >= 24:
		return pair(binary.BigEndian.Uint32(body[16:20]), binary.BigEndian.Uint32(body[20:24]))
	case mimeType == "image/gif" && len(body) >= 10:
		return pair(uint32(binary.LittleEndian.Uint16(body[6:8])), uint32(binary.LittleEndian.Uint16(body[8:10])))
	case mimeType == "image/jpeg":
		for offset := 2; offset+9 < len(body); {
			if body[offset] != 0xff {
				offset++
				continue
			}
			marker := body[offset+1]
			length := int(binary.BigEndian.Uint16(body[offset+2 : offset+4]))
			if length < 2 || offset+length+2 > len(body) {
				break
			}
			if marker >= 0xc0 && marker <= 0xc3 {
				return pair(uint32(binary.BigEndian.Uint16(body[offset+7:offset+9])), uint32(binary.BigEndian.Uint16(body[offset+5:offset+7])))
			}
			offset += length + 2
		}
	}
	return nil, nil
}

// publicAssetExists resolves a tracked /assets URL without permitting traversal.
func (m *Module) publicAssetExists(assetURL string) bool {
	if m.assetsDir == "" || !strings.HasPrefix(assetURL, "/assets/") {
		return false
	}
	relative, err := url.PathUnescape(strings.TrimPrefix(assetURL, "/assets/"))
	if err != nil || relative == "" || strings.ContainsRune(relative, 0) {
		return false
	}
	normalized, ok := cleanRelativePath(relative)
	if !ok {
		return false
	}
	info, err := os.Stat(filepath.Join(m.assetsDir, filepath.FromSlash(normalized)))
	return err == nil && info.Mode().IsRegular()
}
