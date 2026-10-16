// Package pgtest gives tests a fresh, migrated Postgres database.
//
// Tests that call New are skipped unless PULSE_TEST_POSTGRES holds a URL to a
// server where the user may create databases, for example
// postgres://test:test@localhost:5432/postgres?sslmode=disable.
package pgtest

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// New creates a throwaway database, applies schemas/postgres/*.sql in order
// and returns a pool to it. The database is dropped when the test ends.
func New(t testing.TB) *pgxpool.Pool {
	t.Helper()
	base := os.Getenv("PULSE_TEST_POSTGRES")
	if base == "" {
		t.Skip("PULSE_TEST_POSTGRES not set")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	admin, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer func() { _ = admin.Close(ctx) }()
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	name := "pulse_test_" + hex.EncodeToString(b)
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatalf("create database: %v", err)
	}

	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("parse url: %v", err)
	}
	u.Path = "/" + name
	pool, err := pgxpool.New(ctx, u.String())
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(func() {
		pool.Close()
		c, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if a, err := pgx.Connect(c, base); err == nil {
			_, _ = a.Exec(c, "DROP DATABASE IF EXISTS "+name+" WITH (FORCE)")
			_ = a.Close(c)
		}
	})

	files, err := filepath.Glob(filepath.Join(schemaDir(), "*.sql"))
	if err != nil || len(files) == 0 {
		t.Fatalf("no migrations found in %s: %v", schemaDir(), err)
	}
	sort.Strings(files)
	for _, f := range files {
		sql, err := os.ReadFile(f) //nolint:gosec // migration files from the repository
		if err != nil {
			t.Fatalf("read %s: %v", f, err)
		}
		if _, err := pool.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("apply %s: %v", filepath.Base(f), err)
		}
	}
	return pool
}

func schemaDir() string {
	_, file, _, _ := runtime.Caller(0)
	return filepath.Join(filepath.Dir(file), "..", "..", "..", "schemas", "postgres")
}

// User inserts a user and returns its id.
func User(t testing.TB, db *pgxpool.Pool, email string) int64 {
	t.Helper()
	var id int64
	err := db.QueryRow(context.Background(),
		`INSERT INTO users (email, password_hash, name) VALUES ($1, 'x', $1) RETURNING id`, email).Scan(&id)
	if err != nil {
		t.Fatalf("insert user: %v", err)
	}
	return id
}

// Org inserts an organization with the given owner and returns its id.
func Org(t testing.TB, db *pgxpool.Pool, name string, owner int64) int64 {
	t.Helper()
	ctx := context.Background()
	var id int64
	if err := db.QueryRow(ctx, `INSERT INTO organizations (name) VALUES ($1) RETURNING id`, name).Scan(&id); err != nil {
		t.Fatalf("insert org: %v", err)
	}
	if _, err := db.Exec(ctx, `INSERT INTO org_members (org_id, user_id, role) VALUES ($1, $2, 'owner')`, id, owner); err != nil {
		t.Fatalf("insert owner: %v", err)
	}
	return id
}
