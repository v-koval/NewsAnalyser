package db

import (
	"context"
	"embed"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationsFS embed.FS

type DB struct {
	Pool *pgxpool.Pool
}

func Open(ctx context.Context, url string) (*DB, error) {
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		return nil, err
	}
	if err := pool.Ping(ctx); err != nil {
		return nil, err
	}
	return &DB{Pool: pool}, nil
}

func (d *DB) Migrate(ctx context.Context) error {
	entries, err := migrationsFS.ReadDir("migrations")
	if err != nil {
		return err
	}
	names := make([]string, 0, len(entries))
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, n := range names {
		b, err := migrationsFS.ReadFile("migrations/" + n)
		if err != nil {
			return err
		}
		if _, err := d.Pool.Exec(ctx, string(b)); err != nil {
			return fmt.Errorf("migrate %s: %w", n, err)
		}
	}
	return nil
}

func (d *DB) Close() { d.Pool.Close() }
