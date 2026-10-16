// Package postgres stores releases, artifacts and upload tokens.
package postgres

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/Mutiur03/pulse/internal/modules/sourcemaps/domain"
	"github.com/Mutiur03/pulse/internal/symbolicate"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store keeps it all in Postgres.
type Store struct{ DB *pgxpool.Pool }

// EnsureRelease returns the release, creating it on first use.
func (s *Store) EnsureRelease(ctx context.Context, project int64, version string) (domain.Release, error) {
	var r domain.Release
	err := s.DB.QueryRow(ctx, `INSERT INTO releases (project_id, version) VALUES ($1, $2)
		ON CONFLICT (project_id, version) DO UPDATE SET version = EXCLUDED.version
		RETURNING id, project_id, version, created_at`, project, version).Scan(&r.ID, &r.ProjectID, &r.Version, &r.CreatedAt)
	return r, err
}

func (s *Store) ListReleases(ctx context.Context, project int64, limit int) ([]domain.Release, error) {
	rows, err := s.DB.Query(ctx, `SELECT r.id, r.project_id, r.version, r.created_at, count(a.id), coalesce(sum(a.size), 0)
		FROM releases r LEFT JOIN release_artifacts a ON a.release_id = r.id
		WHERE r.project_id = $1 GROUP BY r.id ORDER BY r.created_at DESC, r.id DESC LIMIT $2`, project, limit)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Release, error) {
		var r domain.Release
		err := row.Scan(&r.ID, &r.ProjectID, &r.Version, &r.CreatedAt, &r.Artifacts, &r.Bytes)
		return r, err
	})
}

func (s *Store) GetRelease(ctx context.Context, project int64, version string) (domain.Release, []domain.Artifact, error) {
	var r domain.Release
	err := s.DB.QueryRow(ctx, `SELECT id, project_id, version, created_at FROM releases WHERE project_id = $1 AND version = $2`,
		project, version).Scan(&r.ID, &r.ProjectID, &r.Version, &r.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return r, nil, domain.ErrNotFound
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

func scanArtifact(row pgx.CollectableRow) (domain.Artifact, error) {
	var a domain.Artifact
	err := row.Scan(&a.ID, &a.ReleaseID, &a.ProjectID, &a.Name, &a.DebugID, &a.ObjectKey, &a.Size, &a.SHA256, &a.CreatedAt)
	return a, err
}

// PutArtifact records an uploaded map, replacing one with the same name.
func (s *Store) PutArtifact(ctx context.Context, a domain.Artifact) (domain.Artifact, error) {
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

func hashToken(t string) []byte {
	sum := sha256.Sum256([]byte(t))
	return sum[:]
}

// CreateToken makes a token and returns it with its one-time secret.
func (s *Store) CreateToken(ctx context.Context, project int64, name string, by int64) (domain.Token, string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return domain.Token{}, "", err
	}
	secret := domain.TokenPrefix + hex.EncodeToString(b)
	var t domain.Token
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

func (s *Store) ListTokens(ctx context.Context, project int64) ([]domain.Token, error) {
	rows, err := s.DB.Query(ctx, `SELECT id, project_id, name, hint, created_at, last_used_at, revoked_at FROM upload_tokens
		WHERE project_id = $1 ORDER BY revoked_at IS NULL DESC, created_at DESC LIMIT 100`, project)
	if err != nil {
		return nil, err
	}
	return pgx.CollectRows(rows, func(row pgx.CollectableRow) (domain.Token, error) {
		var t domain.Token
		err := row.Scan(&t.ID, &t.ProjectID, &t.Name, &t.Hint, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt)
		return t, err
	})
}

func (s *Store) RevokeToken(ctx context.Context, project, id int64) (domain.Token, error) {
	var t domain.Token
	err := s.DB.QueryRow(ctx, `UPDATE upload_tokens SET revoked_at = coalesce(revoked_at, now())
		WHERE project_id = $1 AND id = $2 RETURNING id, project_id, name, hint, created_at, last_used_at, revoked_at`, project, id).
		Scan(&t.ID, &t.ProjectID, &t.Name, &t.Hint, &t.CreatedAt, &t.LastUsedAt, &t.RevokedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return t, domain.ErrNotFound
	}
	return t, err
}

// Authenticate returns the project of an active upload token.
func (s *Store) Authenticate(ctx context.Context, secret string) (int64, error) {
	if !strings.HasPrefix(secret, domain.TokenPrefix) || len(secret) > 100 {
		return 0, domain.ErrNotFound
	}
	var project int64
	err := s.DB.QueryRow(ctx, `UPDATE upload_tokens SET last_used_at = now()
		WHERE token_hash = $1 AND revoked_at IS NULL RETURNING project_id`, hashToken(secret)).Scan(&project)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, domain.ErrNotFound
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
