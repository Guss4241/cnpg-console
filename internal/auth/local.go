package auth

import "crypto/subtle"

// Authenticator vérifie un couple identifiant/mot de passe.
type Authenticator interface {
	Authenticate(username, password string) bool
}

// Local : admin unique (identifiant + mot de passe depuis l'env).
type Local struct {
	username string
	password string
}

func NewLocal(username, password string) *Local {
	return &Local{username: username, password: password}
}

// Authenticate compare en temps constant.
func (l *Local) Authenticate(username, password string) bool {
	u := subtle.ConstantTimeCompare([]byte(username), []byte(l.username))
	p := subtle.ConstantTimeCompare([]byte(password), []byte(l.password))
	return u == 1 && p == 1
}
