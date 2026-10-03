package repository

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"gorm.io/gorm"
)

// ErrEmailTaken is returned by CreateUser when the email is already registered.
var ErrEmailTaken = errors.New("email already registered")

type User struct {
	ID           string    `gorm:"column:id" json:"id"`
	Email        string    `gorm:"column:email" json:"email"`
	PasswordHash string    `gorm:"column:password_hash" json:"-"`
	Name         string    `gorm:"column:name" json:"name"`
	Role         string    `gorm:"column:role" json:"role"`
	CreatedAt    time.Time `gorm:"column:created_at" json:"created_at"`
}

type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}

// CreateUser always inserts role 'user'; roles are never taken from callers.
func (r *UserRepository) CreateUser(ctx context.Context, email, passwordHash, name string) (*User, error) {
	var u User
	err := r.db.WithContext(ctx).Raw(
		`INSERT INTO users (email, password_hash, name, role) VALUES (?, ?, ?, 'user')
		 RETURNING id, email, password_hash, name, role, created_at`,
		email, passwordHash, name,
	).Scan(&u).Error
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrEmailTaken
		}
		return nil, err
	}
	return &u, nil
}

// FindByEmail returns nil, nil when no user has this email.
func (r *UserRepository) FindByEmail(ctx context.Context, email string) (*User, error) {
	return r.find(ctx, `SELECT id, email, password_hash, name, role, created_at FROM users WHERE email = ?`, email)
}

// FindByID returns nil, nil when the user does not exist.
func (r *UserRepository) FindByID(ctx context.Context, id string) (*User, error) {
	return r.find(ctx, `SELECT id, email, password_hash, name, role, created_at FROM users WHERE id = ?::uuid`, id)
}

func (r *UserRepository) find(ctx context.Context, sql string, arg any) (*User, error) {
	var u User
	tx := r.db.WithContext(ctx).Raw(sql, arg).Scan(&u)
	if tx.Error != nil {
		return nil, tx.Error
	}
	if tx.RowsAffected == 0 {
		return nil, nil
	}
	return &u, nil
}
