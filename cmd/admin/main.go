// Command admin creates an admin account or grants the admin role to an
// existing one. It talks to the same SQLite database as the API (DATABASE_URL)
// so the admin role has no self-service path.
package main

import (
	"context"
	"database/sql"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/joho/godotenv"

	"github.com/pinolrent/pinolrent-api/internal/auth"
	"github.com/pinolrent/pinolrent-api/internal/db"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "admin:", err)
		os.Exit(1)
	}
}

func run() error {
	email := flag.String("email", "", "email of the account to create or promote (required)")
	password := flag.String("password", "", "password for a new account; ignored when the account already exists")
	flag.Parse()

	// Same .env handling as the server: a malformed file is a hard error.
	if err := godotenv.Load(); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("invalid .env file: %w", err)
	}

	emailAddr := strings.ToLower(strings.TrimSpace(*email))
	if emailAddr == "" {
		return errors.New("-email is required")
	}
	if len(emailAddr) > 254 || !strings.Contains(emailAddr, "@") {
		return fmt.Errorf("invalid email %q", emailAddr)
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		dbURL = "pinolrent.db"
	}
	d, err := db.Open(dbURL)
	if err != nil {
		return fmt.Errorf("open db: %w", err)
	}
	defer func() { _ = d.Close() }()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	var id int64
	err = d.QueryRowContext(ctx, `SELECT id FROM users WHERE lower(email) = ?`, emailAddr).Scan(&id)
	switch {
	case errors.Is(err, sql.ErrNoRows):
		if *password == "" {
			return fmt.Errorf("no account for %s: pass -password to create it", emailAddr)
		}
		if len(*password) < 8 || len(*password) > 72 {
			return errors.New("password must be 8-72 characters")
		}
		hash, err := auth.HashPassword(*password)
		if err != nil {
			return fmt.Errorf("hash password: %w", err)
		}
		if err := createAdmin(ctx, d, emailAddr, hash); err != nil {
			return err
		}
	case err != nil:
		return fmt.Errorf("lookup user: %w", err)
	default:
		if *password != "" {
			fmt.Fprintf(os.Stderr, "admin: %s already exists; password unchanged\n", emailAddr)
		}
		if _, err := d.ExecContext(ctx,
			`INSERT OR IGNORE INTO user_roles (user_id, role) VALUES (?, 'admin')`, id); err != nil {
			return fmt.Errorf("grant admin role: %w", err)
		}
	}

	roles, err := rolesOf(ctx, d, emailAddr)
	if err != nil {
		return fmt.Errorf("read roles: %w", err)
	}
	fmt.Printf("%s roles: %s\n", emailAddr, strings.Join(roles, ","))
	return nil
}

// createAdmin inserts an admin-only account. It is deliberately not a buyer:
// it is an operator account, not a marketplace one.
func createAdmin(ctx context.Context, d *sql.DB, email, hash string) error {
	tx, err := d.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()

	res, err := tx.ExecContext(ctx,
		`INSERT INTO users (email, password_hash) VALUES (?, ?)`, email, hash)
	if err != nil {
		return fmt.Errorf("insert user: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx,
		`INSERT INTO user_roles (user_id, role) VALUES (?, 'admin')`, id); err != nil {
		return fmt.Errorf("grant admin role: %w", err)
	}
	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

func rolesOf(ctx context.Context, d *sql.DB, email string) ([]string, error) {
	rows, err := d.QueryContext(ctx, `
		SELECT r.role FROM user_roles r
		JOIN users u ON u.id = r.user_id
		WHERE lower(u.email) = ?
		ORDER BY r.role`, email)
	if err != nil {
		return nil, err
	}
	defer func() { _ = rows.Close() }()

	var roles []string
	for rows.Next() {
		var role string
		if err := rows.Scan(&role); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	return roles, rows.Err()
}
