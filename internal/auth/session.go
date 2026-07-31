// Package auth : authentification locale + session signée (HMAC), stateless.
// Calqué sur le pattern s3ctl. L'OIDC/PKCE générique pourra être ajouté ensuite.
package auth

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// SessionManager émet et vérifie des sessions via un cookie signé HMAC-SHA256.
type SessionManager struct {
	secret     []byte
	cookieName string
	ttl        time.Duration
	secure     bool
	now        func() time.Time
}

func NewSessionManager(secret string, ttl time.Duration, secure bool) *SessionManager {
	return &SessionManager{
		secret:     []byte(secret),
		cookieName: "cnpg_console_session",
		ttl:        ttl,
		secure:     secure,
		now:        time.Now,
	}
}

func (s *SessionManager) sign(payload string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte(payload))
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString([]byte(payload)) + "." + sig
}

func (s *SessionManager) verifyToken(token string) (actor string, ok bool) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return "", false
	}
	payloadBytes, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return "", false
	}
	expected := s.sign(string(payloadBytes))
	expectedSig := strings.SplitN(expected, ".", 2)[1]
	if !hmac.Equal([]byte(parts[1]), []byte(expectedSig)) {
		return "", false
	}
	payload := string(payloadBytes)
	sep := strings.LastIndex(payload, "|")
	if sep < 0 {
		return "", false
	}
	actor = payload[:sep]
	expUnix, err := strconv.ParseInt(payload[sep+1:], 10, 64)
	if err != nil {
		return "", false
	}
	if s.now().Unix() > expUnix {
		return "", false
	}
	return actor, true
}

func (s *SessionManager) Issue(w http.ResponseWriter, actor string) {
	exp := s.now().Add(s.ttl)
	payload := fmt.Sprintf("%s|%d", actor, exp.Unix())
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    s.sign(payload),
		Path:     "/",
		Expires:  exp,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *SessionManager) Verify(r *http.Request) (actor string, ok bool) {
	c, err := r.Cookie(s.cookieName)
	if err != nil {
		return "", false
	}
	return s.verifyToken(c.Value)
}

func (s *SessionManager) Clear(w http.ResponseWriter) {
	http.SetCookie(w, &http.Cookie{
		Name:     s.cookieName,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	})
}
