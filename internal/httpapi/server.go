// Package httpapi expose l'API REST de cnpg-console : auth locale, liste des
// clusters, préparation (PR) et finalisation (sync ArgoCD) d'un nouveau cluster.
// Routage via le mux standard (Go 1.22+).
package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"log"
	"net/http"
	"time"

	"cnpg-console/internal/argocd"
	"cnpg-console/internal/auth"
	"cnpg-console/internal/config"
	"cnpg-console/internal/github"
)

// Server porte les dépendances et construit le handler HTTP.
type Server struct {
	cfg      *config.Config
	gh       *github.Client // nil si pas de token GitHub
	argo     *argocd.Client // nil si ArgoCD non configuré
	authn    auth.Authenticator
	sessions *auth.SessionManager
	secret   []byte
	version  string
	web      http.Handler
	now      func() time.Time
}

// Deps regroupe les dépendances d'un Server.
type Deps struct {
	Config   *config.Config
	GitHub   *github.Client
	ArgoCD   *argocd.Client
	Authn    auth.Authenticator
	Sessions *auth.SessionManager
	Secret   string
	Version  string
	Web      http.Handler
}

func NewServer(d Deps) *Server {
	return &Server{
		cfg:      d.Config,
		gh:       d.GitHub,
		argo:     d.ArgoCD,
		authn:    d.Authn,
		sessions: d.Sessions,
		secret:   []byte(d.Secret),
		version:  d.Version,
		web:      d.Web,
		now:      time.Now,
	}
}

type ctxKey string

const actorKey ctxKey = "actor"

// Handler construit le routeur complet avec middleware.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) { writeJSON(w, 200, map[string]string{"status": "ok"}) })
	mux.HandleFunc("GET /version", func(w http.ResponseWriter, _ *http.Request) {
		writeJSON(w, 200, map[string]string{"version": s.version})
	})
	mux.HandleFunc("GET /api/auth/config", s.handleAuthConfig)
	mux.HandleFunc("POST /api/auth/login", s.handleLogin)
	mux.HandleFunc("POST /api/auth/logout", s.handleLogout)

	prot := func(pattern string, h http.HandlerFunc) { mux.Handle(pattern, s.requireAuth(h)) }
	prot("GET /api/auth/me", s.handleMe)
	prot("GET /api/clusters", s.handleClusters)
	prot("POST /api/prepare", s.handlePrepare)
	prot("POST /api/finalize", s.handleFinalize)

	if s.web != nil {
		mux.Handle("/", s.web)
	} else {
		mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
			writeJSON(w, http.StatusOK, map[string]string{"service": "cnpg-console", "ui": "non embarquée"})
		})
	}
	return s.recoverMW(s.logMW(mux))
}

func (s *Server) recoverMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				log.Printf("httpapi: panic: %v", rec)
				writeError(w, http.StatusInternalServerError, "erreur interne")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *Server) logMW(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := s.now()
		next.ServeHTTP(w, r)
		log.Printf("httpapi: %s %s (%s)", r.Method, r.URL.Path, s.now().Sub(start))
	})
}

// requireAuth vérifie la session et, sur les mutations, le token CSRF.
func (s *Server) requireAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		actor, ok := s.sessions.Verify(r)
		if !ok {
			writeError(w, http.StatusUnauthorized, "non authentifié")
			return
		}
		if isMutating(r.Method) {
			if !hmac.Equal([]byte(r.Header.Get("X-CSRF-Token")), []byte(s.csrfToken(actor))) {
				writeError(w, http.StatusForbidden, "token CSRF invalide ou absent")
				return
			}
		}
		ctx := context.WithValue(r.Context(), actorKey, actor)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func isMutating(method string) bool {
	switch method {
	case http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete:
		return true
	default:
		return false
	}
}

func actorOf(r *http.Request) string {
	if v, ok := r.Context().Value(actorKey).(string); ok {
		return v
	}
	return ""
}

func (s *Server) csrfToken(actor string) string {
	mac := hmac.New(sha256.New, s.secret)
	mac.Write([]byte("csrf|" + actor))
	return hex.EncodeToString(mac.Sum(nil))
}
