package middleware

import (
	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/pkg/jwt"
)

// AccessCookie is the access-token cookie name (docs/04 §5).
const AccessCookie = "athenaeum_at"

const (
	ctxUserID = "user_id"
	ctxRole   = "role"
	ctxEmail  = "email"
)

// ErrorResponder renders an error envelope (handler.RespondError); injected to
// keep middleware from importing handler (docs/01 §2).
type ErrorResponder func(c *gin.Context, err error)

// Identify parses the access cookie when present and stores the identity in the
// context; it never rejects (used by rate limiting and RequireAuth).
func Identify(jm *jwt.Manager) gin.HandlerFunc {
	return func(c *gin.Context) {
		if tok, err := c.Cookie(AccessCookie); err == nil && tok != "" {
			if cl, err := jm.Parse(tok); err == nil {
				if id, err := uuid.Parse(cl.Subject); err == nil {
					c.Set(ctxUserID, id)
					c.Set(ctxRole, cl.Role)
					c.Set(ctxEmail, cl.Email)
				}
			}
		}
		c.Next()
	}
}

// RequireAuth rejects requests without a valid access token (401 UNAUTHENTICATED).
func RequireAuth(respond ErrorResponder) gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, ok := UserID(c); !ok {
			respond(c, model.ErrUnauthenticated)
			return
		}
		c.Next()
	}
}

// RequireAdmin requires role=admin (403 FORBIDDEN); it must follow RequireAuth.
func RequireAdmin(respond ErrorResponder) gin.HandlerFunc {
	return func(c *gin.Context) {
		if Role(c) != model.RoleAdmin {
			respond(c, model.ErrForbidden)
			return
		}
		c.Next()
	}
}

// UserID returns the authenticated user id.
func UserID(c *gin.Context) (uuid.UUID, bool) {
	v, ok := c.Get(ctxUserID)
	if !ok {
		return uuid.Nil, false
	}
	id, ok := v.(uuid.UUID)
	return id, ok
}

// Role returns the authenticated role ("" when anonymous).
func Role(c *gin.Context) string { return c.GetString(ctxRole) }
