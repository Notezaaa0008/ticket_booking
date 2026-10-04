// Package db opens the PostgreSQL connection and runs SQL migrations.
package db

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"
	"time"

	"github.com/golang-migrate/migrate/v4"
	_ "github.com/golang-migrate/migrate/v4/database/postgres"
	"github.com/golang-migrate/migrate/v4/source/iofs"
	"gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"

	"ticketbooking/migrations"
)

// Connect opens a GORM connection pool and verifies it with a ping.
func Connect(url string) (*gorm.DB, error) {
	gdb, err := gorm.Open(postgres.Open(url), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Warn),
	})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		return nil, fmt.Errorf("get sql.DB: %w", err)
	}
	sqlDB.SetMaxOpenConns(20)
	sqlDB.SetMaxIdleConns(5)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := sqlDB.PingContext(ctx); err != nil {
		return nil, fmt.Errorf("ping database: %w", err)
	}
	return gdb, nil
}

// CheckCurrent fails when the database has no schema version, is dirty, or is behind
// the highest embedded *.up.sql. It does not apply migrations.
func CheckCurrent(url string) error {
	latest, err := latestEmbeddedVersion()
	if err != nil {
		return err
	}
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, url)
	if err != nil {
		return fmt.Errorf("init migrate: %w", err)
	}
	defer m.Close()

	current, dirty, err := m.Version()
	if errors.Is(err, migrate.ErrNilVersion) {
		return fmt.Errorf("migrations pending: database has no schema version (run server migrate up)")
	}
	if err != nil {
		return fmt.Errorf("read schema version: %w", err)
	}
	if dirty {
		return fmt.Errorf("migrations dirty at version %d", current)
	}
	if current < latest {
		return fmt.Errorf("migrations pending: database is at version %d, embedded migrations go to %d (run server migrate up)", current, latest)
	}
	if current > latest {
		return fmt.Errorf("database schema version %d is newer than embedded migrations (%d)", current, latest)
	}
	return nil
}

func latestEmbeddedVersion() (uint, error) {
	entries, err := fs.ReadDir(migrations.FS, ".")
	if err != nil {
		return 0, fmt.Errorf("read embedded migrations: %w", err)
	}
	var latest uint
	var found bool
	for _, e := range entries {
		name := e.Name()
		if !strings.HasSuffix(name, ".up.sql") {
			continue
		}
		i := strings.IndexByte(name, '_')
		if i <= 0 {
			continue
		}
		n, err := strconv.ParseUint(name[:i], 10, 64)
		if err != nil {
			continue
		}
		if !found || uint(n) > latest {
			latest = uint(n)
			found = true
		}
	}
	if !found {
		return 0, fmt.Errorf("no embedded up migrations")
	}
	return latest, nil
}

// Migrate applies the embedded migrations. direction is "up" or "down".
func Migrate(url, direction string) error {
	src, err := iofs.New(migrations.FS, ".")
	if err != nil {
		return fmt.Errorf("load migrations: %w", err)
	}
	m, err := migrate.NewWithSourceInstance("iofs", src, url)
	if err != nil {
		return fmt.Errorf("init migrate: %w", err)
	}
	defer m.Close()

	switch direction {
	case "up":
		err = m.Up()
	case "down":
		err = m.Down()
	default:
		return fmt.Errorf("unknown direction %q (use up or down)", direction)
	}
	if err != nil && !errors.Is(err, migrate.ErrNoChange) {
		return fmt.Errorf("migrate %s: %w", direction, err)
	}
	return nil
}
