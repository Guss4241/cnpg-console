package httpapi

import (
	"net/http"

	"cnpg-console/internal/clusterspec"
)

// handleClusters lit l'état courant des clusters depuis le repo infra et renvoie
// la liste + le prochain port NLB libre + le suffixe DNS (pour le formulaire).
func (s *Server) handleClusters(w http.ResponseWriter, r *http.Request) {
	if s.gh == nil {
		writeError(w, http.StatusServiceUnavailable, "GitHub non configuré (token absent)")
		return
	}
	data, _, err := s.gh.GetFile(r.Context(), s.cfg.GitHub.InfraRepo, s.cfg.GitHub.InfraValuesPath, s.cfg.GitHub.InfraBranch)
	if err != nil {
		writeError(w, http.StatusBadGateway, "lecture du values infra: "+err.Error())
		return
	}
	existing, err := clusterspec.ParseExisting(data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"clusters":       existing,
		"nextPort":       clusterspec.AllocatePort(existing),
		"hostnameSuffix": s.cfg.CNPG.HostnameSuffix,
		"infraRepo":      s.cfg.GitHub.InfraRepo,
	})
}
