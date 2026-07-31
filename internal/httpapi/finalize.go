package httpapi

import (
	"fmt"
	"net/http"
)

type finalizeRequest struct {
	Token string `json:"token"`
}

type finalizeResponse struct {
	Cluster string     `json:"cluster"`
	Action  string     `json:"action"`
	Apps    []appState `json:"apps"`
	Notes   []string   `json:"notes"`
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

	// 1. Toutes les PR liées doivent être mergées.
	if s.gh == nil {
		writeError(w, http.StatusServiceUnavailable, "GitHub non configuré")
		return
	}
	for _, ref := range p.PRs {
		pr, err := s.gh.GetPR(ctx, ref.Repo, ref.Number)
		if err != nil {
			writeError(w, http.StatusBadGateway, "état PR "+ref.Repo+"#"+itoa(ref.Number)+": "+err.Error())
			return
		}
		if !pr.Merged {
			writeError(w, http.StatusConflict, "la PR "+ref.Repo+"#"+itoa(ref.Number)+" n'est pas encore mergée — merger puis réessayer")
			return
		}
	}

	// 2. Sync ArgoCD des apps concernées (la 1re est bloquante).
	if s.argo == nil {
		writeError(w, http.StatusServiceUnavailable, "ArgoCD non configuré (token absent)")
		return
	}
	resp := finalizeResponse{Cluster: p.Cluster, Action: p.Action}
	for i, app := range p.Apps {
		// Hard refresh pour prendre en compte le nouveau commit, puis sync.
		_, gerr := s.argo.Get(ctx, app, "hard")
		var st struct {
			Sync, Health, Message string
		}
		var serr error
		if gerr == nil {
			r, e := s.argo.Sync(ctx, app, p.Prune)
			serr = e
			if e == nil {
				st.Sync, st.Health, st.Message = r.Sync, r.Health, r.Message
			}
		} else {
			serr = gerr
		}
		if serr != nil {
			if i == 0 {
				writeError(w, http.StatusBadGateway, "sync "+app+": "+serr.Error())
				return
			}
			resp.Apps = append(resp.Apps, appState{Name: app, Message: serr.Error()})
			resp.Notes = append(resp.Notes, "sync de l'app "+app+" à refaire (repo d'équipe pas encore enregistré dans ArgoCD ?) : "+serr.Error())
			continue
		}
		resp.Apps = append(resp.Apps, appState{Name: app, Sync: st.Sync, Health: st.Health, Message: st.Message})
	}

	// 3. Notes selon l'action.
	host := p.Cluster + s.cfg.CNPG.HostnameSuffix
	ns := "pg-" + p.Cluster
	switch p.Action {
	case "scale":
		resp.Notes = append(resp.Notes, "Volume redimensionné (expansion en ligne du PVC). Vérifier la capacité une fois le sync appliqué.")
	case "add-db":
		resp.Notes = append(resp.Notes, "Nouvelle base synchronisée dans le namespace "+ns+".")
	case "add-role":
		resp.Notes = append(resp.Notes, "Nouveau rôle synchronisé dans "+ns+". S'assurer que son Secret de mot de passe existe (voir l'étape de préparation).")
	case "del-db", "del-role":
		resp.Notes = append(resp.Notes,
			"Objet retiré de GitOps et pruné dans "+ns+".",
			"reclaimPolicy=delete : la base/le rôle PostgreSQL a été RÉELLEMENT supprimé côté serveur par CNPG (opération irréversible).")
	default:
		resp.Notes = append(resp.Notes,
			"Connexion : psql \"host="+host+" port=5432 sslmode=require dbname=... user=...\" (via VPN).",
			"Mot de passe owner dans le Secret "+p.Cluster+"-app (ns "+ns+").")
	}

	writeJSON(w, http.StatusOK, resp)
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }
