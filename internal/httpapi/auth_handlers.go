package httpapi

import "net/http"

// handleAuthConfig indique au front les méthodes d'auth disponibles.
func (s *Server) handleAuthConfig(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"local": true,
		"oidc":  false, // OIDC branché ultérieurement
	})
}

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req loginRequest
	if !readJSON(w, r, &req) {
		return
	}
	if !s.authn.Authenticate(req.Username, req.Password) {
		writeError(w, http.StatusUnauthorized, "identifiants invalides")
		return
	}
	s.sessions.Issue(w, req.Username)
	writeJSON(w, http.StatusOK, map[string]string{
		"actor":     req.Username,
		"csrfToken": s.csrfToken(req.Username),
	})
}

func (s *Server) handleLogout(w http.ResponseWriter, _ *http.Request) {
	s.sessions.Clear(w)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) handleMe(w http.ResponseWriter, r *http.Request) {
	actor := actorOf(r)
	writeJSON(w, http.StatusOK, map[string]string{
		"actor":     actor,
		"csrfToken": s.csrfToken(actor),
	})
}
