package storage

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"habr-tg-bot/internal/domain"

	_ "modernc.org/sqlite"
)

const migrationSQL = `
CREATE TABLE IF NOT EXISTS users (
    telegram_user_id INTEGER PRIMARY KEY,
    auto_send_enabled INTEGER NOT NULL DEFAULT 0,
    created_at TEXT NOT NULL,
    updated_at TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS user_categories (
    telegram_user_id INTEGER NOT NULL,
    category TEXT NOT NULL,
    created_at TEXT NOT NULL,
    PRIMARY KEY (telegram_user_id, category),
    FOREIGN KEY (telegram_user_id) REFERENCES users(telegram_user_id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_users_auto_send_enabled ON users(auto_send_enabled);
CREATE INDEX IF NOT EXISTS idx_user_categories_user ON user_categories(telegram_user_id);
`

type SQLite struct {
	db     *sql.DB
	logger *slog.Logger
}

func NewSQLite(ctx context.Context, path string, logger *slog.Logger) (*SQLite, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, fmt.Errorf("create sqlite directory: %w", err)
	}
	db, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("open sqlite: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)

	s := &SQLite{db: db, logger: logger}
	if err := s.applyPragmas(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := s.applyMigrations(ctx); err != nil {
		_ = db.Close()
		return nil, err
	}
	return s, nil
}

func (s *SQLite) Close() error {
	return s.db.Close()
}

func (s *SQLite) applyPragmas(ctx context.Context) error {
	statements := []string{
		"PRAGMA journal_mode=WAL;",
		"PRAGMA synchronous=NORMAL;",
		"PRAGMA foreign_keys=ON;",
		"PRAGMA busy_timeout=5000;",
	}
	for _, stmt := range statements {
		if _, err := s.db.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("apply sqlite pragma %q: %w", stmt, err)
		}
	}
	return nil
}

func (s *SQLite) applyMigrations(ctx context.Context) error {
	if _, err := s.db.ExecContext(ctx, migrationSQL); err != nil {
		return fmt.Errorf("apply migrations: %w", err)
	}
	return nil
}

func (s *SQLite) EnsureUser(ctx context.Context, telegramUserID int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO users (telegram_user_id, auto_send_enabled, created_at, updated_at)
VALUES (?, 0, ?, ?)
ON CONFLICT(telegram_user_id) DO NOTHING;
`, telegramUserID, now, now)
	if err != nil {
		return fmt.Errorf("ensure user: %w", err)
	}
	return nil
}

func (s *SQLite) GetUserSettings(ctx context.Context, telegramUserID int64) (domain.UserSettings, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT telegram_user_id, auto_send_enabled, created_at, updated_at
FROM users
WHERE telegram_user_id = ?;
`, telegramUserID)

	var settings domain.UserSettings
	var autoSend int
	var createdRaw, updatedRaw string
	err := row.Scan(&settings.TelegramUserID, &autoSend, &createdRaw, &updatedRaw)
	if errors.Is(err, sql.ErrNoRows) {
		if err := s.EnsureUser(ctx, telegramUserID); err != nil {
			return domain.UserSettings{}, err
		}
		return s.GetUserSettings(ctx, telegramUserID)
	}
	if err != nil {
		return domain.UserSettings{}, fmt.Errorf("get user settings: %w", err)
	}
	settings.AutoSendEnabled = autoSend == 1
	settings.CreatedAt, _ = time.Parse(time.RFC3339, createdRaw)
	settings.UpdatedAt, _ = time.Parse(time.RFC3339, updatedRaw)

	categories, err := s.GetUserCategories(ctx, telegramUserID)
	if err != nil {
		return domain.UserSettings{}, err
	}
	settings.Categories = categories
	return settings, nil
}

func (s *SQLite) GetUserCategories(ctx context.Context, telegramUserID int64) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT category
FROM user_categories
WHERE telegram_user_id = ?
ORDER BY category;
`, telegramUserID)
	if err != nil {
		return nil, fmt.Errorf("get user categories: %w", err)
	}
	defer rows.Close()

	categories := make([]string, 0)
	for rows.Next() {
		var category string
		if err := rows.Scan(&category); err != nil {
			return nil, fmt.Errorf("scan user category: %w", err)
		}
		categories = append(categories, category)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate user categories: %w", err)
	}
	return categories, nil
}

func (s *SQLite) ToggleUserCategory(ctx context.Context, telegramUserID int64, category string) (bool, error) {
	if err := s.EnsureUser(ctx, telegramUserID); err != nil {
		return false, err
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, fmt.Errorf("begin toggle category tx: %w", err)
	}
	defer func() { _ = tx.Rollback() }()

	var exists int
	err = tx.QueryRowContext(ctx, `
SELECT 1 FROM user_categories WHERE telegram_user_id = ? AND category = ?;
`, telegramUserID, category).Scan(&exists)
	if err == nil {
		if _, err := tx.ExecContext(ctx, `
DELETE FROM user_categories WHERE telegram_user_id = ? AND category = ?;
`, telegramUserID, category); err != nil {
			return false, fmt.Errorf("delete user category: %w", err)
		}
		if err := touchUser(ctx, tx, telegramUserID); err != nil {
			return false, err
		}
		if err := tx.Commit(); err != nil {
			return false, fmt.Errorf("commit remove category: %w", err)
		}
		return false, nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return false, fmt.Errorf("check user category: %w", err)
	}

	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, `
INSERT INTO user_categories (telegram_user_id, category, created_at)
VALUES (?, ?, ?);
`, telegramUserID, category, now); err != nil {
		return false, fmt.Errorf("insert user category: %w", err)
	}
	if err := touchUser(ctx, tx, telegramUserID); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, fmt.Errorf("commit add category: %w", err)
	}
	return true, nil
}

func (s *SQLite) SetAutoSend(ctx context.Context, telegramUserID int64, enabled bool) error {
	if err := s.EnsureUser(ctx, telegramUserID); err != nil {
		return err
	}
	value := 0
	if enabled {
		value = 1
	}
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
UPDATE users
SET auto_send_enabled = ?, updated_at = ?
WHERE telegram_user_id = ?;
`, value, now, telegramUserID)
	if err != nil {
		return fmt.Errorf("set auto send: %w", err)
	}
	return nil
}

func (s *SQLite) ListAutoSendUsers(ctx context.Context) ([]domain.UserSettings, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT telegram_user_id
FROM users
WHERE auto_send_enabled = 1
ORDER BY telegram_user_id;
`)
	if err != nil {
		return nil, fmt.Errorf("list auto send users: %w", err)
	}
	defer rows.Close()

	ids := make([]int64, 0)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, fmt.Errorf("scan auto send user: %w", err)
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate auto send users: %w", err)
	}

	users := make([]domain.UserSettings, 0, len(ids))
	for _, id := range ids {
		settings, err := s.GetUserSettings(ctx, id)
		if err != nil {
			s.logger.Error("failed to load auto-send user settings", "telegram_user_id", id, "error", err)
			continue
		}
		users = append(users, settings)
	}
	return users, nil
}

func touchUser(ctx context.Context, tx *sql.Tx, telegramUserID int64) error {
	now := time.Now().UTC().Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, `
UPDATE users SET updated_at = ? WHERE telegram_user_id = ?;
`, now, telegramUserID); err != nil {
		return fmt.Errorf("touch user: %w", err)
	}
	return nil
}
