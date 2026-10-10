package handler

import (
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/cattyman919/athenaeum/api/internal/middleware"
	"github.com/cattyman919/athenaeum/api/internal/model"
	"github.com/cattyman919/athenaeum/api/internal/service/auth"
)

// RefreshCookie is the refresh-token cookie name (docs/04 §5).
const RefreshCookie = "athenaeum_rt"

// AuthHandler serves /api/auth/* (docs/03 §4).
type AuthHandler struct {
	svc       *auth.Service
	secure    bool
	webOrigin string
	stateKey  []byte
}

// NewAuthHandler builds the handler. stateKey signs the OAuth state cookie.
func NewAuthHandler(svc *auth.Service, secure bool, webOrigin string, stateKey []byte) *AuthHandler {
	return &AuthHandler{svc: svc, secure: secure, webOrigin: webOrigin, stateKey: stateKey}
}

type userEnvelope struct {
	User model.User `json:"user"`
}

func (h *AuthHandler) setSession(c *gin.Context, s auth.Session) {
	h.setCookie(c, middleware.AccessCookie, s.AccessToken, s.AccessExpires)
	h.setCookie(c, RefreshCookie, s.RefreshToken, s.RefreshExpires)
}

func (h *AuthHandler) clearSession(c *gin.Context) {
	h.setCookie(c, middleware.AccessCookie, "", time.Unix(0, 0))
	h.setCookie(c, RefreshCookie, "", time.Unix(0, 0))
}

func (h *AuthHandler) setCookie(c *gin.Context, name, value string, exp time.Time) {
	maxAge := int(time.Until(exp).Seconds())
	if value == "" || maxAge < 0 {
		maxAge = -1
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name: name, Value: value, Path: "/", MaxAge: maxAge, Expires: exp,
		HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode,
	})
}

// Register handles POST /api/auth/register.
func (h *AuthHandler) Register(c *gin.Context) {
	var req struct {
		Name     string `json:"name"`
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, model.New(model.CodeValidationFailed, "Invalid request body."))
		return
	}
	u, sess, err := h.svc.Register(c.Request.Context(), req.Name, req.Email, req.Password)
	if err != nil {
		RespondError(c, err)
		return
	}
	h.setSession(c, sess)
	c.JSON(http.StatusCreated, userEnvelope{User: u})
}

// Login handles POST /api/auth/login.
func (h *AuthHandler) Login(c *gin.Context) {
	var req struct {
		Email    string `json:"email"`
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		RespondError(c, model.New(model.CodeValidationFailed, "Invalid request body."))
		return
	}
	u, sess, err := h.svc.Login(c.Request.Context(), req.Email, req.Password)
	if err != nil {
		RespondError(c, err)
		return
	}
	h.setSession(c, sess)
	c.JSON(http.StatusOK, userEnvelope{User: u})
}

// Refresh handles POST /api/auth/refresh.
func (h *AuthHandler) Refresh(c *gin.Context) {
	raw, _ := c.Cookie(RefreshCookie)
	u, sess, err := h.svc.Refresh(c.Request.Context(), raw)
	if err != nil {
		if de, ok := model.AsError(err); ok && (de.Code == model.CodeReusedToken || de.Code == model.CodeUnauthenticated) {
			h.clearSession(c)
		}
		RespondError(c, err)
		return
	}
	h.setSession(c, sess)
	c.JSON(http.StatusOK, userEnvelope{User: u})
}

// Logout handles POST /api/auth/logout.
func (h *AuthHandler) Logout(c *gin.Context) {
	uid, _ := middleware.UserID(c)
	if err := h.svc.Logout(c.Request.Context(), uid); err != nil {
		RespondError(c, err)
		return
	}
	h.clearSession(c)
	c.Status(http.StatusNoContent)
}

// Me handles GET /api/auth/me.
func (h *AuthHandler) Me(c *gin.Context) {
	uid, _ := middleware.UserID(c)
	u, err := h.svc.Me(c.Request.Context(), uid)
	if err != nil {
		RespondError(c, err)
		return
	}
	c.JSON(http.StatusOK, userEnvelope{User: u})
}
