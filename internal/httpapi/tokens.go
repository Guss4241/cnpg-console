package httpapi

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"strings"
)

// finalizePayload lie l'action de finalisation aux éléments produits par prepare
// (anti-rejeu / anti-tampering, façon confirmToken s3ctl).
type finalizePayload struct {
	Cluster      string `json:"cluster"`
	InfraRepo    string `json:"infraRepo"`
	InfraPR      int    `json:"infraPR"`
	TeamRepo     string `json:"teamRepo,omitempty"`
	DelegatedApp string `json:"delegatedApp,omitempty"`
}

func (s *Server) signToken(p finalizePayload) string {
	raw, _ := json.Marshal(p)
	mac := hmac.New(sha256.New, s.secret)
	mac.Write(raw)
	sig := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	return base64.RawURLEncoding.EncodeToString(raw) + "." + sig
}

func (s *Server) verifyToken(token string) (finalizePayload, bool) {
	parts := strings.SplitN(token, ".", 2)
	if len(parts) != 2 {
		return finalizePayload{}, false
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[0])
	if err != nil {
		return finalizePayload{}, false
	}
	mac := hmac.New(sha256.New, s.secret)
	mac.Write(raw)
	expected := base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
	if !hmac.Equal([]byte(parts[1]), []byte(expected)) {
		return finalizePayload{}, false
	}
	var p finalizePayload
	if err := json.Unmarshal(raw, &p); err != nil {
		return finalizePayload{}, false
	}
	return p, true
}
