package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/crypto/bcrypt"

	"newsanalyzer/internal/repo"
)

type Auth struct {
	Repo       *repo.Repo
	Secret     []byte
	AccessTTL  time.Duration
	RefreshTTL time.Duration
}

type Claims struct {
	UserID string `json:"uid"`
	jwt.RegisteredClaims
}

func New(r *repo.Repo, secret string, accessMin, refreshHours int) *Auth {
	return &Auth{
		Repo:       r,
		Secret:     []byte(secret),
		AccessTTL:  time.Duration(accessMin) * time.Minute,
		RefreshTTL: time.Duration(refreshHours) * time.Hour,
	}
}

func HashPassword(p string) (string, error) {
	b, err := bcrypt.GenerateFromPassword([]byte(p), bcrypt.DefaultCost)
	return string(b), err
}

func CheckPassword(hash, p string) bool {
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(p)) == nil
}

func (a *Auth) SignAccess(userID string) (string, error) {
	c := Claims{UserID: userID, RegisteredClaims: jwt.RegisteredClaims{
		ExpiresAt: jwt.NewNumericDate(time.Now().Add(a.AccessTTL)),
		IssuedAt:  jwt.NewNumericDate(time.Now()),
	}}
	t := jwt.NewWithClaims(jwt.SigningMethodHS256, c)
	return t.SignedString(a.Secret)
}

func (a *Auth) ParseAccess(tokStr string) (*Claims, error) {
	t, err := jwt.ParseWithClaims(tokStr, &Claims{}, func(t *jwt.Token) (any, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("bad alg")
		}
		return a.Secret, nil
	})
	if err != nil || !t.Valid {
		return nil, errors.New("invalid token")
	}
	c, ok := t.Claims.(*Claims)
	if !ok {
		return nil, errors.New("bad claims")
	}
	return c, nil
}

func (a *Auth) NewRefresh(ctx context.Context, userID string) (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	tok := hex.EncodeToString(b)
	h := HashToken(tok)
	if err := a.Repo.StoreRefresh(ctx, userID, h, time.Now().Add(a.RefreshTTL)); err != nil {
		return "", err
	}
	return tok, nil
}

func (a *Auth) UseRefresh(ctx context.Context, tok string) (string, string, string, error) {
	h := HashToken(tok)
	uid, exp, err := a.Repo.FindRefresh(ctx, h)
	if err != nil {
		return "", "", "", errors.New("invalid refresh")
	}
	if time.Now().After(exp) {
		_ = a.Repo.DeleteRefresh(ctx, h)
		return "", "", "", errors.New("expired")
	}
	_ = a.Repo.DeleteRefresh(ctx, h)
	access, err := a.SignAccess(uid)
	if err != nil {
		return "", "", "", err
	}
	newRefresh, err := a.NewRefresh(ctx, uid)
	if err != nil {
		return "", "", "", err
	}
	return access, newRefresh, uid, nil
}

func HashToken(t string) string {
	s := sha256.Sum256([]byte(t))
	return hex.EncodeToString(s[:])
}

type ctxKey struct{}

func UserID(r *http.Request) string {
	if v, ok := r.Context().Value(ctxKey{}).(string); ok {
		return v
	}
	return ""
}

func (a *Auth) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if !strings.HasPrefix(h, "Bearer ") {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		tok := strings.TrimPrefix(h, "Bearer ")
		c, err := a.ParseAccess(tok)
		if err != nil {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, c.UserID)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
