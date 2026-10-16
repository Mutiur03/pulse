// Package domain defines releases, their source map artifacts and the
// upload tokens CI uses to send them.
package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"path"
	"regexp"
	"strings"
	"time"
)

// ErrNotFound is returned for releases, artifacts or tokens outside the project.
var ErrNotFound = errors.New("not found")

// Release is a version of a project's code.
type Release struct {
	ID        int64
	ProjectID int64
	Version   string
	CreatedAt time.Time
	Artifacts int64
	Bytes     int64
}

// Artifact is one uploaded source map.
type Artifact struct {
	ID        int64
	ReleaseID int64
	ProjectID int64
	Name      string
	DebugID   string
	ObjectKey string
	Size      int64
	SHA256    string
	CreatedAt time.Time
}

// Token is an upload token. The secret is only known when it is created.
type Token struct {
	ID         int64
	ProjectID  int64
	Name       string
	Hint       string
	CreatedAt  time.Time
	LastUsedAt *time.Time
	RevokedAt  *time.Time
}

var reVersion = regexp.MustCompile(`^[\w.@+\-]{1,200}$`)

// ValidVersion checks a release name: letters, digits and . _ - + @.
func ValidVersion(v string) bool { return reVersion.MatchString(v) }

// ArtifactName normalizes an uploaded file name: "~/" plus the URL path for
// relative names, kept as is for absolute URLs.
func ArtifactName(name string) (string, error) {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	switch {
	case name == "" || len(name) > 1000 || strings.ContainsAny(name, "\r\n\x00"):
		return "", errors.New("invalid file name")
	case strings.HasPrefix(name, "http://") || strings.HasPrefix(name, "https://"):
		return name, nil
	}
	name = strings.TrimPrefix(name, "~")
	clean := path.Clean("/" + name)
	if strings.Contains(clean, "/../") {
		return "", errors.New("invalid file name")
	}
	return "~" + clean, nil
}

// ObjectKey is where an artifact's bytes live in object storage.
func ObjectKey(project int64, sha string) string {
	return fmt.Sprintf("sourcemaps/%d/%s/%s.map", project, sha[:2], sha)
}

// SHA256 hex-encodes the digest of data.
func SHA256(data []byte) string {
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// TokenPrefix marks upload tokens so they are easy to spot in config and
// secret scanners, and never confused with public ingest keys (pk_).
const TokenPrefix = "pul_"

// Upload is one validated source map from an upload request.
type Upload struct {
	Name    string
	Data    []byte
	DebugID string
}

// UploadError is a problem with the uploaded files that the client can fix.
// TooLarge distinguishes size limits from other bad input.
type UploadError struct {
	Msg      string
	TooLarge bool
}

func (e *UploadError) Error() string { return e.Msg }
