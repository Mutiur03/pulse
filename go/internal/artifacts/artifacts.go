// Package artifacts stores releases, their source maps and the upload tokens
// that CI uses to send them.
package artifacts

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/Mutiur03/pulse/internal/symbolicate"
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

// Store keeps it all in Postgres.
type Store struct{ DB *pgxpool.Pool }

// EnsureRelease returns the release, creating it on first use.
func (s *Store) EnsureRelease(ctx context.Context, project int64, version string) (Release, error) {
	var r Release
	err := s.DB.QueryRow(ctx, `INSERT INTO releases (project_id, version) VALUES ($1, $2)
		ON CONFLICT (project_id, version) DO UPDATE SET version = EXCLUDED.version
		RETURNING id, project_id, version, created_at`, project, version).Scan(&r.ID, &r.ProjectID, &r.Version, &r.CreatedAt)
	return r, err
}

func (s *Store) ListReleases(ctx context.Context, project int64, limit int) ([]Release, error) {
	rows, err := s.DB.Query(ctx, `SELECT r.id, r.project_id, r.version, r.created_at, count(a.id), coalesce(sum(a.size), 0)
		FROM releases r LEFT JOIN release_artifacts a ON a.release_id = r.id
		WHERE r.project_id = $1 GROUP BY r.id ORDER BY r.created_at DESC, r.id DESC LIMIT $2`, project, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Release, error) {
		var r Release
		err := row.Scan(&r.ID, &r.ProjectID, &r.Version, &r.CreatedAt, &r.Artifacts, &r.Bytes)
		return r, err
	})
}

func (s *Store) GetRelease(ctx context.Context, project int64, version string) (Release, []Artifact, error) {
	var r Release
	err := s.DB.QueryRow(ctx, `SELECT id, project_id, version, created_at FROM releases WHERE project_id = $1 AND version = $2`,
		project, version).Scan(&r.ID, &r.ProjectID, &r.Version, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, nil, ErrNotFound
	}
	if err != nil {
		return r, nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT `+artifactCols+` FROM release_artifacts WHERE release_id = $1 ORDER BY name`, r.ID)
	if err != nil {
		return r, nil, err
	}
	arts, err := pgx.CollectRows(rows, scanArtifact)
	for _, a := range arts {
		r.Artifacts++
		r.Bytes += a.Size
	}
	return r, arts, err
}

const artifactCols = `id, release_id, project_id, name, coalesce(debug_id::text, ''), object_key, size, sha256, created_at`

func scanArtifact(row pgx.CollectableRow) (Artifact, error) {
	var a Artifact
	err := row.Scan(&a.ID, &a.ReleaseID, &a.ProjectID, &a.Name, &a.DebugID, &a.ObjectKey, &a.Size, &a.SHA256, &a.CreatedAt)
	return a, err
}

// PutArtifact records an uploaded map, replacing one with the same name.
func (s *Store) PutArtifact(ctx context.Context, a Artifact) (Artifact, error) {
	var debug *string
	if a.DebugID != "" {
		debug = &a.DebugID
	}
	rows, err := s.DB.Query(ctx, `INSERT INTO release_artifacts (release_id, project_id, name, debug_id, object_key, size, sha256)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (release_id, name) DO UPDATE SET debug_id = EXCLUDED.debug_id, object_key = EXCLUDED.object_key,
			size = EXCLUDED.size, sha256 = EXCLUDED.sha256, created_at = now()
		RETURNING `+artifactCols, a.ReleaseID, a.ProjectID, a.Name, debug, a.ObjectKey, a.Size, a.SHA256)
	if err != nil {
		return a, err
	}
	return pgx.CollectExactlyOneRow(rows, scanArtifact)
}

// MapByDebugID finds a map by the debug id of its generated file, in any
// release of the project.
func (s *Store) MapByDebugID(ctx context.Context, project int64, debugID string) (string, error) {
	var key string
	err := s.DB.QueryRow(ctx, `SELECT object_key FROM release_artifacts WHERE project_id = $1 AND debug_id = $2
		ORDER BY created_at DESC LIMIT 1`, project, debugID).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", symbolicate.ErrNoMap
	}
	return key, err
}

// MapByURL finds the map of a generated file by its URL within a release:
// an artifact named "~/<path>.map" (or the full URL plus ".map") matches
// exactly; otherwise the artifact sharing the longest path suffix wins.
func (s *Store) MapByURL(ctx context.Context, project int64, release, fileURL string) (string, error) {
	p := urlPath(fileURL)
	if p == "" {
		return "", symbolicate.ErrNoMap
	}
	base := path.Base(p)
	rows, err := s.DB.Query(ctx, `SELECT a.name, a.object_key FROM release_artifacts a JOIN releases r ON r.id = a.release_id
		WHERE r.project_id = $1 AND r.version = $2 AND (a.name = $3 OR a.name = $4 OR a.name LIKE $5)
		ORDER BY a.created_at DESC LIMIT 200`,
		project, release, "~"+p+".map", strings.SplitN(fileURL, "?", 2)[0]+".map", "%/"+likeEscape(base)+".map")
	if err != nil {
		return "", err
	}
	cands, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) ([2]string, error) {
		var c [2]string
		err := row.Scan(&c[0], &c[1])
		return c, err
	})
	if err != nil {
		return "", err
	}
	return bestMatch(p, fileURL, cands)
}

// bestMatch picks the artifact for a file path: exact names first, then the
// longest shared run of trailing path segments.
func bestMatch(p, fileURL string, cands [][2]string) (string, error) {
	want := strings.Split(strings.Trim(p, "/"), "/")
	best, bestScore, bestLen := "", 0, 0
	for _, c := range cands {
		name := strings.TrimSuffix(c[0], ".map")
		if name == "~"+p || name == strings.SplitN(fileURL, "?", 2)[0] {
			return c[1], nil
		}
		got := strings.Split(strings.Trim(urlPath(strings.TrimPrefix(name, "~")), "/"), "/")
		score := 0
		for i := 1; i <= len(want) && i <= len(got) && want[len(want)-i] == got[len(got)-i]; i++ {
			score = i
		}
		// On a tie, the name with fewer unmatched segments is closer.
		if score > bestScore || (score == bestScore && score > 0 && len(got) < bestLen) {
			best, bestScore, bestLen = c[1], score, len(got)
		}
	}
	if best == "" {
		return "", symbolicate.ErrNoMap
	}
	return best, nil
}

// urlPath returns the path of a URL or of an already relative path.
func urlPath(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if u.Path == "" {
		return ""
	}
	return path.Clean("/" + u.Path)
}

func likeEscape(s string) string {
	return strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`).Replace(s)
}

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

func hashToken(t string) []byte {
	sum := sha256.Sum256([]byte(t))
	return sum[:]
}

// CreateToken makes a token and returns it with its one-time secret.
func (s *Store) CreateToken(ctx context.Context, project int64, name string, by int64) (Token, string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return Token{}, "", err
	}
	secret := TokenPrefix + hex.EncodeToString(b)
	var t Token
	var creator *int64
	if by > 0 {
		creator = &by
	}
	err := s.DB.QueryRow(ctx, `INSERT INTO upload_tokens (project_id, name, token_hash, hint, created_by)
		VALUES ($1, $2, $3, $4, $5) RETURNING id, project_id, name, hint, created_at, last_used_at, revoked_at`,
		project, name, hashToken(secret), secret[len(secret)-4:], creator).
		Scan(&t.ID, &t.ProjectID, &t.Name, &t.Hint, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt)
	return t, secret, err
}

func (s *Store) ListTokens(ctx context.Context, project int64) ([]Token, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, project_id, name, hint, created_at, last_used_at, revoked_at FROM upload_tokens
		WHERE project_id = $1 ORDER BY revoked_at IS NULL DESC, created_at DESC LIMIT 100`, project)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (Token, error) {
		var t Token
		err := row.Scan(&t.ID, &t.ProjectID, &t.Name, &t.Hint, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt)
		return t, err
	})
}

func (s *Store) RevokeToken(ctx context.Context, project, id int64) (Token, error) {
	var t Token
	err := s.DB.QueryRow(ctx, `UPDATE upload_tokens SET revoked_at = coalesce(revoked_at, now())
		WHERE project_id = $1 AND id = $2 RETURNING id, project_id, name, hint, created_at, last_used_at, revoked_at`, project, id).
		Scan(&t.ID, &t.ProjectID, &t.Name, &t.Hint, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, ErrNotFound
	}
	return t, err
}

// Authenticate returns the project of an active upload token.
func (s *Store) Authenticate(ctx context.Context, secret string) (int64, error) {
	if !strings.HasPrefix(secret, TokenPrefix) || len(secret) > 100 {
		return 0, ErrNotFound
	}
	var project int64
	err := s.DB.QueryRow(ctx, `UPDATE upload_tokens SET last_used_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL RETURNING project_id`, hashToken(secret)).Scan(&project)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return project, err
}

// ExpireArtifacts deletes a project's source maps uploaded before cutoff and
// returns the object keys no other artifact still uses, for removal from
// storage, and how many artifacts were deleted.
func (s *Store) ExpireArtifacts(ctx context.Context, project int64, cutoff time.Time) ([]string, int, error) {
	rows, err := s.DB.Query(ctx, `DELETE FROM release_artifacts WHERE project_id = $1 AND created_at < $2 RETURNING object_key`, project, cutoff)
	if err != nil {
		return nil, 0, err
	}
	keys, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil || len(keys) == 0 {
		return nil, 0, err
	}
	rows, err = s.DB.Query(ctx, `SELECT DISTINCT object_key FROM release_artifacts WHERE object_key = ANY($1)`, keys)
	if err != nil {
		return nil, len(keys), err
	}
	used, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return nil, len(keys), err
	}
	inUse := map[string]bool{}
	for _, k := range used {
		inUse[k] = true
	}
	var gone []string
	seen := map[string]bool{}
	for _, k := range keys {
		if !inUse[k] && !seen[k] {
			seen[k] = true
			gone = append(gone, k)
		}
	}
	return gone, len(keys), nil
}
