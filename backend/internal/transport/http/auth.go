package http

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/jackc/pgx/v5/pgconn"
	"golang.org/x/crypto/bcrypt"
)

const (
	accessTokenTTL  = 15 * time.Minute
	refreshTokenTTL = 30 * 24 * time.Hour
)

type Auth struct {
	db        *sql.DB
	jwtSecret []byte
}

type user struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	Name      string    `json:"name"`
	Role      string    `json:"role"`
	CreatedAt time.Time `json:"created_at"`
}

type tokenClaims struct {
	UserID int64
	Email  string
	Role   string
	Expiry time.Time
}

func NewAuth(db *sql.DB, secret string) *Auth {
	return &Auth{db: db, jwtSecret: []byte(secret)}
}

// EnsureSchema creates the auth tables. These statements are idempotent so existing
// development databases can be upgraded when the API starts.
func (a *Auth) EnsureSchema() error {
	statements := []string{`
		CREATE TABLE IF NOT EXISTS users (
			id BIGSERIAL PRIMARY KEY,
			email TEXT NOT NULL UNIQUE,
			name TEXT NOT NULL,
			password_hash TEXT NOT NULL,
			role TEXT NOT NULL DEFAULT 'user' CHECK (role IN ('user', 'admin')),
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);`, `CREATE TABLE IF NOT EXISTS refresh_tokens (
			token_hash CHAR(64) PRIMARY KEY,
			user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
			expires_at TIMESTAMPTZ NOT NULL,
			created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
		);`, `CREATE INDEX IF NOT EXISTS refresh_tokens_user_id_idx ON refresh_tokens(user_id);`}
	for _, statement := range statements {
		if _, err := a.db.Exec(statement); err != nil {
			return err
		}
	}
	return nil
}

func (a *Auth) RegisterRoutes(api *gin.RouterGroup) {
	auth := api.Group("/auth")
	auth.POST("/register", a.register)
	auth.POST("/login", a.login)
	auth.POST("/refresh", a.refresh)
	auth.POST("/logout", a.logout)
	auth.GET("/me", a.RequireAuth(), a.me)

	api.GET("/admin/ping", a.RequireAuth(), a.RequireRole("admin"), func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"message": "admin access granted"})
	})
}

type credentials struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

func (a *Auth) register(c *gin.Context) {
	var input struct {
		Email    string `json:"email"`
		Name     string `json:"name"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	input.Name = strings.TrimSpace(input.Name)
	if len(input.Email) < 3 || len(input.Email) > 254 || !strings.Contains(input.Email, "@") || input.Name == "" || len(input.Name) > 100 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "A valid email and name are required"})
		return
	}
	if len(input.Password) < 8 || len(input.Password) > 72 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Password must be between 8 and 72 bytes"})
		return
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(input.Password), bcrypt.DefaultCost)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create account"})
		return
	}
	var u user
	err = a.db.QueryRowContext(c.Request.Context(), `
		INSERT INTO users (email, name, password_hash) VALUES ($1, $2, $3)
		RETURNING id, email, name, role, created_at`, input.Email, input.Name, string(hash)).Scan(
		&u.ID, &u.Email, &u.Name, &u.Role, &u.CreatedAt)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			c.JSON(http.StatusConflict, gin.H{"error": "An account with this email already exists"})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create account"})
		return
	}
	a.issueSession(c, u, http.StatusCreated)
}

func (a *Auth) login(c *gin.Context) {
	var input credentials
	if err := c.ShouldBindJSON(&input); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "Invalid request body"})
		return
	}
	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	var u user
	var passwordHash string
	err := a.db.QueryRowContext(c.Request.Context(), `
		SELECT id, email, name, role, created_at, password_hash FROM users WHERE email = $1`, input.Email).Scan(
		&u.ID, &u.Email, &u.Name, &u.Role, &u.CreatedAt, &passwordHash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(passwordHash), []byte(input.Password)) != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Email or password is incorrect"})
		return
	}
	a.issueSession(c, u, http.StatusOK)
}

func (a *Auth) refresh(c *gin.Context) {
	var input struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.RefreshToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "refresh_token is required"})
		return
	}
	oldHash := hashToken(input.RefreshToken)
	tx, err := a.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not refresh session"})
		return
	}
	defer tx.Rollback()
	var u user
	err = tx.QueryRowContext(c.Request.Context(), `
		DELETE FROM refresh_tokens WHERE token_hash = $1 AND expires_at > NOW()
		RETURNING user_id`, oldHash).Scan(&u.ID)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Refresh token is invalid or expired"})
		return
	}
	err = tx.QueryRowContext(c.Request.Context(), `
		SELECT email, name, role, created_at FROM users WHERE id = $1`, u.ID).Scan(
		&u.Email, &u.Name, &u.Role, &u.CreatedAt)
	if err != nil {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Account no longer exists"})
		return
	}
	access, err := a.createAccessToken(u)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create session"})
		return
	}
	refreshToken, err := randomToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create session"})
		return
	}
	if _, err = tx.ExecContext(c.Request.Context(), `INSERT INTO refresh_tokens (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`, hashToken(refreshToken), u.ID, time.Now().Add(refreshTokenTTL)); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create session"})
		return
	}
	if err = tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create session"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": u, "access_token": access, "token_type": "Bearer", "expires_in": int(accessTokenTTL.Seconds()), "refresh_token": refreshToken})
}

func (a *Auth) logout(c *gin.Context) {
	var input struct {
		RefreshToken string `json:"refresh_token"`
	}
	if err := c.ShouldBindJSON(&input); err != nil || input.RefreshToken == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "refresh_token is required"})
		return
	}
	_, _ = a.db.ExecContext(c.Request.Context(), `DELETE FROM refresh_tokens WHERE token_hash = $1`, hashToken(input.RefreshToken))
	c.Status(http.StatusNoContent)
}

func (a *Auth) me(c *gin.Context) {
	u, ok := c.Get("auth_user")
	if !ok {
		c.JSON(http.StatusUnauthorized, gin.H{"error": "Unauthorized"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"user": u})
}

func (a *Auth) issueSession(c *gin.Context, u user, status int) {
	access, err := a.createAccessToken(u)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create session"})
		return
	}
	refresh, err := randomToken()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create session"})
		return
	}
	_, err = a.db.ExecContext(c.Request.Context(), `INSERT INTO refresh_tokens (token_hash, user_id, expires_at) VALUES ($1, $2, $3)`, hashToken(refresh), u.ID, time.Now().Add(refreshTokenTTL))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "Could not create session"})
		return
	}
	c.JSON(status, gin.H{"user": u, "access_token": access, "token_type": "Bearer", "expires_in": int(accessTokenTTL.Seconds()), "refresh_token": refresh})
}

func (a *Auth) RequireAuth() gin.HandlerFunc {
	return func(c *gin.Context) {
		header := strings.Fields(c.GetHeader("Authorization"))
		if len(header) != 2 || !strings.EqualFold(header[0], "Bearer") {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Bearer token is required"})
			return
		}
		claims, err := a.parseAccessToken(header[1])
		if err != nil {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Access token is invalid or expired"})
			return
		}
		c.Set("auth_user", gin.H{"id": claims.UserID, "email": claims.Email, "role": claims.Role})
		c.Set("auth_role", claims.Role)
		c.Next()
	}
}

func (a *Auth) RequireRole(role string) gin.HandlerFunc {
	return func(c *gin.Context) {
		if current, _ := c.Get("auth_role"); current != role {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "Insufficient permissions"})
			return
		}
		c.Next()
	}
}

func (a *Auth) createAccessToken(u user) (string, error) {
	now := time.Now()
	header := base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`))
	claims := struct {
		Subject string `json:"sub"`
		Email   string `json:"email"`
		Role    string `json:"role"`
		Issued  int64  `json:"iat"`
		Expiry  int64  `json:"exp"`
	}{strconv.FormatInt(u.ID, 10), u.Email, u.Role, now.Unix(), now.Add(accessTokenTTL).Unix()}
	encodedClaims, err := json.Marshal(claims)
	if err != nil {
		return "", err
	}
	payload := base64.RawURLEncoding.EncodeToString(encodedClaims)
	unsigned := header + "." + payload
	mac := sign(a.jwtSecret, unsigned)
	return unsigned + "." + base64.RawURLEncoding.EncodeToString(mac), nil
}

func (a *Auth) parseAccessToken(token string) (tokenClaims, error) {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return tokenClaims{}, errors.New("malformed token")
	}
	if parts[0] != base64.RawURLEncoding.EncodeToString([]byte(`{"alg":"HS256","typ":"JWT"}`)) {
		return tokenClaims{}, errors.New("unsupported token header")
	}
	provided, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil || !hmac.Equal(provided, sign(a.jwtSecret, parts[0]+"."+parts[1])) {
		return tokenClaims{}, errors.New("invalid signature")
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		return tokenClaims{}, err
	}
	var raw struct {
		Subject string `json:"sub"`
		Email   string `json:"email"`
		Role    string `json:"role"`
		Expiry  int64  `json:"exp"`
	}
	if err := json.Unmarshal(payload, &raw); err != nil {
		return tokenClaims{}, err
	}
	id, err := strconv.ParseInt(raw.Subject, 10, 64)
	if err != nil || id < 1 || (raw.Role != "user" && raw.Role != "admin") || time.Now().Unix() >= raw.Expiry {
		return tokenClaims{}, errors.New("invalid claims")
	}
	return tokenClaims{UserID: id, Email: raw.Email, Role: raw.Role, Expiry: time.Unix(raw.Expiry, 0)}, nil
}

func randomToken() (string, error) {
	bytes := make([]byte, 32)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(bytes), nil
}

func hashToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func sign(secret []byte, message string) []byte {
	mac := hmac.New(sha256.New, secret)
	_, _ = mac.Write([]byte(message))
	return mac.Sum(nil)
}
