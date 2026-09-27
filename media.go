package main

import (
	"errors"
	"fmt"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// maxPhotoBytes caps an upload. The frontend downscales before sending, so this
// is a backstop against a client that doesn't.
const maxPhotoBytes = 20 << 20 // 20 MiB

// imageExtByContentType maps the types http.DetectContentType recognises to the
// extension the file is stored under. Phone cameras hand over generic names
// like "image.jpg" or "blob", so the bytes decide, not the filename.
var imageExtByContentType = map[string]string{
	"image/jpeg": ".jpg",
	"image/png":  ".png",
	"image/gif":  ".gif",
	"image/webp": ".webp",
}

// errUnsupportedPhoto is returned for an upload that isn't a supported image.
var errUnsupportedPhoto = errors.New("unsupported image type")

// photoType returns an upload's content type and the extension it should be
// stored under, or errUnsupportedPhoto if the bytes aren't a supported image.
func photoType(data []byte) (contentType string, ext string, err error) {
	contentType = strings.TrimSpace(strings.SplitN(http.DetectContentType(data), ";", 2)[0])
	ext, ok := imageExtByContentType[contentType]
	if !ok {
		return "", "", fmt.Errorf("%w %q", errUnsupportedPhoto, contentType)
	}
	return contentType, ext, nil
}

// slugify reduces a topic to a filename-safe fragment.
func slugify(s string) string {
	var b strings.Builder
	lastDash := true
	for _, r := range strings.ToLower(s) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		default:
			if !lastDash {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}

	slug := strings.Trim(b.String(), "-")
	if len(slug) > 40 {
		slug = strings.Trim(slug[:40], "-")
	}
	if slug == "" {
		slug = "photo"
	}
	return slug
}

// photoDirFor is the storage-relative directory a day's photos live in.
func photoDirFor(at time.Time) string {
	return path.Join(imagesDir, at.Format("2006-01-02"))
}

// photoFileName names a photo taken at a given time for a topic. attempt tells
// apart photos that would otherwise share a name, like two taken in the same
// second.
func photoFileName(at time.Time, topic string, ext string, attempt int) string {
	base := fmt.Sprintf("%s-%s", at.Format("150405"), slugify(topic))
	if attempt > 0 {
		return fmt.Sprintf("%s-%d%s", base, attempt, ext)
	}
	return base + ext
}

// photoReference renders the link that points an entry at a stored photo.
func photoReference(relPath string, caption string) string {
	if journalFormat == "org" {
		return fmt.Sprintf("[[file:%s][%s]]", relPath, caption)
	}
	return fmt.Sprintf("![%s](%s)", caption, relPath)
}

// SavePhoto writes an uploaded image into the storage directory and returns the
// storage-relative path it was written to, using forward slashes so the path can
// go straight into a Markdown or Org link.
func SavePhoto(at time.Time, topic string, data []byte) (string, error) {
	_, ext, err := photoType(data)
	if err != nil {
		return "", err
	}

	storageMutex.Lock()
	defer storageMutex.Unlock()

	if err := ensureStorage(); err != nil {
		return "", err
	}

	relDir := photoDirFor(at)
	absDir := filepath.Join(storageRoot(), filepath.FromSlash(relDir))
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return "", err
	}

	for attempt := 0; ; attempt++ {
		name := photoFileName(at, topic, ext, attempt)
		absPath := filepath.Join(absDir, name)
		// O_EXCL so two photos taken in the same second can't overwrite
		// each other.
		f, err := os.OpenFile(absPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
		if os.IsExist(err) {
			continue
		}
		if err != nil {
			return "", err
		}
		if _, err := f.Write(data); err != nil {
			_ = f.Close()
			return "", err
		}
		if err := f.Close(); err != nil {
			return "", err
		}
		return path.Join(relDir, name), nil
	}
}

// cleanPhotoPath normalises the storage-relative path of a photo, as served
// under /api/media/, or returns ok=false for anything outside the images
// directory.
func cleanPhotoPath(relPath string) (string, bool) {
	rel := path.Clean("/" + relPath)
	if !strings.HasPrefix(rel, "/"+imagesDir+"/") {
		return "", false
	}
	return strings.TrimPrefix(rel, "/"), true
}

// photoFilePath resolves the storage-relative path of a photo to a file on
// disk, or returns ok=false for anything outside the images directory.
func photoFilePath(relPath string) (string, bool) {
	rel, ok := cleanPhotoPath(relPath)
	if !ok {
		return "", false
	}
	return filepath.Join(storageRoot(), filepath.FromSlash(rel)), true
}
