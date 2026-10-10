// Package pgtest provides throwaway migrated Postgres databases for repository
// tests (docs/09 §8): one pgvector container per test binary, one fresh
// database per test.
package pgtest

import (
	"context"
	"fmt"
	"io/fs"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/testcontainers/testcontainers-go"
	tcpostgres "github.com/testcontainers/testcontainers-go/modules/postgres"
	"github.com/testcontainers/testcontainers-go/wait"

	"github.com/cattyman919/athenaeum/api/migrations"
)

const image = "pgvector/pgvector:pg16"

var (
	once    sync.Once
	baseURL string // connection string to the "postgres" admin database
	initErr error
	dbSeq   atomic.Int64
)

func start() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	ctr, err := tcpostgres.Run(ctx, image,
		tcpostgres.WithDatabase("postgres"),
		tcpostgres.WithUsername("athenaeum"),
		tcpostgres.WithPassword("athenaeum"),
		testcontainers.WithWaitStrategy(
			wait.ForLog("database system is ready to accept connections").
				WithOccurrence(2).WithStartupTimeout(2*time.Minute)),
	)
	if err != nil {
		initErr = err
		return
	}
	baseURL, initErr = ctr.ConnectionString(ctx, "sslmode=disable")
}

// New returns a pool connected to a fresh database with every migration applied.
// The test is skipped when Docker is unavailable.
func New(t testing.TB) *pgxpool.Pool {
	t.Helper()
	once.Do(start)
	if initErr != nil {
		t.Skipf("postgres test container unavailable: %v", initErr)
	}
	ctx := context.Background()
	admin, err := pgxpool.New(ctx, baseURL)
	if err != nil {
		t.Fatal(err)
	}
	defer admin.Close()

	name := fmt.Sprintf("test_%d_%d", time.Now().UnixNano(), dbSeq.Add(1))
	if _, err := admin.Exec(ctx, "CREATE DATABASE "+name); err != nil {
		t.Fatal(err)
	}
	url := strings.Replace(baseURL, "/postgres?", "/"+name+"?", 1)
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := Migrate(ctx, pool, "up"); err != nil {
		t.Fatal(err)
	}
	return pool
}

// Migrate applies every embedded "up" or "down" migration in order
// (down runs in reverse).
func Migrate(ctx context.Context, pool *pgxpool.Pool, direction string) error {
	files, err := fs.Glob(migrations.FS, "*."+direction+".sql")
	if err != nil {
		return err
	}
	sort.Strings(files)
	if direction == "down" {
		for i, j := 0, len(files)-1; i < j; i, j = i+1, j-1 {
			files[i], files[j] = files[j], files[i]
		}
	}
	for _, f := range files {
		sqlBytes, err := fs.ReadFile(migrations.FS, f)
		if err != nil {
			return err
		}
		if _, err := pool.Exec(ctx, string(sqlBytes)); err != nil {
			return fmt.Errorf("migration %s: %w", f, err)
		}
	}
	return nil
}
