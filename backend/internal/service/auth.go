package service

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"ticketbooking/internal/domain"
	"ticketbooking/internal/repository"
)

const (
	tokenTTL         = 24 * time.Hour
	minPasswordLen   = 8
	maxPasswordBytes = 72 // bcrypt limit
	maxNameLen       = 100
	maxEmailLen      = 254
	invalidCredsMsg  = "invalid email or password"
	authOpTimeout    = 5 * time.Second
)

// dummyHash is a real bcrypt hash used to equalise timing for unknown emails.
var dummyHash = func() []byte {
	h, err := bcrypt.GenerateFromPassword([]byte("timing-equaliser-not-a-password"), bcrypt.DefaultCost)
	if err != nil {
		panic(err) // cannot happen: fixed short input
	}
	return h
}()

type AuthService struct {
	users  *repository.UserRepository
	secret []byte
}

func NewAuthService(users *repository.UserRepository, jwtSecret string) *AuthService {
	return &AuthService{users: users, secret: []byte(jwtSecret)}
}

type claims struct {
	Role string `json:"role"`
	jwt.RegisteredClaims
}

func (s *AuthService) Register(ctx context.Context, email, password, name string) (*repository.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	if !validEmail(email) {
		return nil, domain.NewError(domain.ReasonValidationFailed, "a valid email is required")
	}
	if utf8.RuneCountInString(password) < minPasswordLen || len(password) > maxPasswordBytes {
		return nil, domain.NewError(domain.ReasonValidationFailed, "password must be 8 to 72 characters")
	}
	if name == "" || utf8.RuneCountInString(name) > maxNameLen {
		return nil, domain.NewError(domain.ReasonValidationFailed, "name is required (max 100 characters)")
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(ctx, authOpTimeout)
	defer cancel()
	u, err := s.users.CreateUser(ctx, email, string(hash), name)
	if errors.Is(err, repository.ErrEmailTaken) {
		return nil, domain.NewError(domain.ReasonEmailTaken, "email is already registered")
	}
	return u, err
}

// Login returns a signed token. Unknown email and wrong password give the same error.
func (s *AuthService) Login(ctx context.Context, email, password string) (string, *repository.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	ctx, cancel := context.WithTimeout(ctx, authOpTimeout)
	defer cancel()
	u, err := s.users.FindByEmail(ctx, email)
	if err != nil {
		return "", nil, err
	}
	if u == nil {
		// Burn comparable time so response timing does not reveal registered emails.
		// The result is irrelevant: the request is rejected either way.
		_ = bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return "", nil, domain.NewError(domain.ReasonInvalidCredentials, invalidCredsMsg)
	}
	if bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(password)) != nil {
		return "", nil, domain.NewError(domain.ReasonInvalidCredentials, invalidCredsMsg)
	}
	tok, err := s.IssueToken(u.ID, u.Role, time.Now())
	if err != nil {
		return "", nil, err
	}
	return tok, u, nil
}

// IssueToken signs an HS256 token (claims: sub = user id, role) valid for 24h from now.
func (s *AuthService) IssueToken(userID, role string, now time.Time) (string, error) {
	c := claims{
		Role: role,
		RegisteredClaims: jwt.RegisteredClaims{
			Subject:   userID,
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(tokenTTL)),
		},
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, c).SignedString(s.secret)
}

// VerifyToken validates signature (HS256 only) and expiry, returning user id and role.
func (s *AuthService) VerifyToken(token string) (userID, role string, err error) {
	var c claims
	_, err = jwt.ParseWithClaims(token, &c, func(*jwt.Token) (any, error) { return s.secret, nil },
		jwt.WithValidMethods([]string{"HS256"}), jwt.WithExpirationRequired())
	if err != nil {
		return "", "", err
	}
	if c.Subject == "" || (c.Role != "user" && c.Role != "admin") {
		return "", "", errors.New("invalid claims")
	}
	return c.Subject, c.Role, nil
}

func (s *AuthService) Me(ctx context.Context, userID string) (*repository.User, error) {
	ctx, cancel := context.WithTimeout(ctx, authOpTimeout)
	defer cancel()
	u, err := s.users.FindByID(ctx, userID)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, domain.NewError(domain.ReasonUnauthenticated, "authentication required")
	}
	return u, nil
}

func validEmail(email string) bool {
	if email == "" || len(email) > maxEmailLen {
		return false
	}
	addr, err := mail.ParseAddress(email)
	return err == nil && addr.Address == email && strings.Contains(email[strings.LastIndex(email, "@"):], ".")
}
