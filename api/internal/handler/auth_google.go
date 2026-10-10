package handler

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
)

const (
	stateCookie = "athenaeum_oauth_state"
	stateTTL    = 10 * time.Minute
	defaultNext = "/catalog"
)

// safeNext only allows same-site relative paths (open-redirect guard).
func safeNext(next string) string {
	if next == "" || !strings.HasPrefix(next, "/") || strings.HasPrefix(next, "//") || strings.Contains(next, "\\") {
		return defaultNext
	}
	return next
}

var b64url = base64.RawURLEncoding

// signState returns "nonce.next.exp.sig" where sig = HMAC(key, nonce.next.exp).
func (h *AuthHandler) signState(nonce, next string, exp time.Time) string {
	payload := nonce + "." + b64url.EncodeToString([]byte(next)) + "." + strconv.FormatInt(exp.Unix(), 10)
	mac := hmac.New(sha256.New, h.stateKey)
	mac.Write([]byte(payload))
	return payload + "." + b64url.EncodeToString(mac.Sum(nil))
}

var errBadState = errors.New("invalid oauth state")

// verifyState validates the cookie value and returns (nonce, next).
func (h *AuthHandler) verifyState(cookie string, now time.Time) (string, string, error) {
	parts := strings.Split(cookie, ".")
	if len(parts) != 4 {
		return "", "", errBadState
	}
	payload := strings.Join(parts[:3], ".")
	mac := hmac.New(sha256.New, h.stateKey)
	mac.Write([]byte(payload))
	sig, err := b64url.DecodeString(parts[3])
	if err != nil || !hmac.Equal(sig, mac.Sum(nil)) {
		return "", "", errBadState
	}
	expUnix, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || now.Unix() > expUnix {
		return "", "", errBadState
	}
	nextRaw, err := b64url.DecodeString(parts[1])
	if err != nil {
		return "", "", errBadState
	}
	return parts[0], safeNext(string(nextRaw)), nil
}

func (h *AuthHandler) ssoFailure(c *gin.Context) {
	c.Redirect(http.StatusFound, h.webOrigin+"/login?error=sso")
}

// GoogleStart handles GET /api/auth/google.
func (h *AuthHandler) GoogleStart(c *gin.Context) {
	if !h.svc.GoogleEnabled() {
		h.ssoFailure(c)
		return
	}
	nb := make([]byte, 16)
	if _, err := rand.Read(nb); err != nil {
		h.ssoFailure(c)
		return
	}
	nonce := b64url.EncodeToString(nb)
	exp := time.Now().Add(stateTTL)
	url, err := h.svc.GoogleAuthURL(c.Request.Context(), nonce)
	if err != nil {
		h.ssoFailure(c)
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name: stateCookie, Value: h.signState(nonce, safeNext(c.Query("next")), exp), Path: "/api/auth/google",
		MaxAge: int(stateTTL.Seconds()), HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode,
	})
	c.Redirect(http.StatusFound, url)
}

// GoogleCallback handles GET /api/auth/google/callback.
func (h *AuthHandler) GoogleCallback(c *gin.Context) {
	cookie, err := c.Cookie(stateCookie)
	// The state cookie is single-use.
	http.SetCookie(c.Writer, &http.Cookie{Name: stateCookie, Value: "", Path: "/api/auth/google", MaxAge: -1,
		HttpOnly: true, Secure: h.secure, SameSite: http.SameSiteLaxMode})
	if err != nil {
		h.ssoFailure(c)
		return
	}
	nonce, next, err := h.verifyState(cookie, time.Now())
	code := c.Query("code")
	if err != nil || code == "" || !hmac.Equal([]byte(nonce), []byte(c.Query("state"))) {
		h.ssoFailure(c)
		return
	}
	_, sess, err := h.svc.GoogleLogin(c.Request.Context(), code)
	if err != nil {
		h.ssoFailure(c)
		return
	}
	h.setSession(c, sess)
	c.Redirect(http.StatusFound, h.webOrigin+next)
}
