package httpapi

import (
	"net/http"
)

type finalizeRequest struct {
	Token string `json:"token"`
}

type finalizeResponse struct {
	Cluster      string    `json:"cluster"`
	ClustersApp  appState  `json:"clustersApp"`
	DelegatedApp *appState `json:"delegatedApp,omitempty"`
	ConnectHost  string    `json:"connectHost"`
	SecretName   string    `json:"secretName"`
	Notes        []string  `json:"notes"`
}

type appState struct {
	Name    string `json:"name"`
	Sync    string `json:"sync"`
	Health  string `json:"health"`
	Message string `json:"message,omitempty"`
}

func (s *Server) handleFinalize(w http.ResponseWriter, r *http.Request) {
	var req finalizeRequest
	if !readJSON(w, r, &req) {
		return
	}
	p, ok := s.verifyToken(req.Token)
	if !ok {
		writeError(w, http.StatusBadRequest, "token de finalisation invalide")
		return
	}
	ctx := r.Context()

	// 1. La PR infra doit être mergée.
	if s.gh == nil {
		writeError(w, http.StatusServiceUnavailable, "GitHub non configuré")
		return
	}
	pr, err := s.gh.GetPR(ctx, p.InfraRepo, p.InfraPR)
	if err != nil {
		writeError(w, http.StatusBadGateway, "état PR: "+err.Error())
		return
	}
	if !pr.Merged {
		writeError(w, http.StatusConflict, "la PR infra n'est pas encore mergée — merger puis réessayer")
		return
	}

	// 2. Sync ArgoCD.
	if s.argo == nil {
		writeError(w, http.StatusServiceUnavailable, "ArgoCD non configuré (token absent)")
		return
	}
	app := s.cfg.ArgoCD.ClustersApp
	// Hard refresh pour prendre en compte le nouveau commit, puis sync.
	if _, err := s.argo.Get(ctx, app, "hard"); err != nil {
		writeError(w, http.StatusBadGateway, "refresh "+app+": "+err.Error())
		return
	}
	st, err := s.argo.Sync(ctx, app)
	if err != nil {
		writeError(w, http.StatusBadGateway, "sync "+app+": "+err.Error())
		return
	}
	resp := finalizeResponse{
		Cluster:     p.Cluster,
		ClustersApp: appState{Name: app, Sync: st.Sync, Health: st.Health, Message: st.Message},
		ConnectHost: p.Cluster + s.cfg.CNPG.HostnameSuffix,
		SecretName:  p.Cluster + "-app",
	}

	// 3. App déléguée (best-effort).
	if p.DelegatedApp != "" {
		_, _ = s.argo.Get(ctx, p.DelegatedApp, "hard")
		dst, derr := s.argo.Sync(ctx, p.DelegatedApp)
		ds := appState{Name: p.DelegatedApp}
		if derr != nil {
			ds.Message = derr.Error()
			resp.Notes = append(resp.Notes, "sync de l'app déléguée à refaire (repo d'équipe pas encore enregistré dans ArgoCD ?) : "+derr.Error())
		} else {
			ds.Sync, ds.Health, ds.Message = dst.Sync, dst.Health, dst.Message
		}
		resp.DelegatedApp = &ds
	}
	resp.Notes = append(resp.Notes,
		"Connexion : psql \"host="+resp.ConnectHost+" port=5432 sslmode=require dbname=... user=...\" (via VPN).",
		"Mot de passe owner dans le Secret "+resp.SecretName+" (ns pg-"+p.Cluster+").")

	writeJSON(w, http.StatusOK, resp)
}
