package httpapi

import (
	"net/http"
	"sort"
)

type bootstrapResponse struct {
	Repo      string   `json:"repo"`
	RepoURL   string   `json:"repoUrl"`
	Branch    string   `json:"branch"`
	Files     []string `json:"files"`
	NextSteps []string `json:"nextSteps"`
}

// handleBootstrap crée le repo d'infra (s'il n'existe pas) et y pose un umbrella
// chart helm-cnpg générique + l'app-of-apps ArgoCD + l'app d'installation de
// l'opérateur. Refuse si le repo existe déjà (ne jamais écraser).
func (s *Server) handleBootstrap(w http.ResponseWriter, r *http.Request) {
	if s.gh == nil {
		writeError(w, http.StatusServiceUnavailable, "GitHub non configuré (token absent)")
		return
	}
	ctx := r.Context()
	g := s.cfg.GitHub

	exists, err := s.gh.RepoExists(ctx, g.InfraRepo)
	if err != nil {
		writeError(w, http.StatusBadGateway, "vérification du repo: "+err.Error())
		return
	}
	if exists {
		writeError(w, http.StatusConflict, "le repo "+g.Owner+"/"+g.InfraRepo+" existe déjà — initialisation refusée (rien n'est écrasé)")
		return
	}

	repo, err := s.gh.CreateOrgRepo(ctx, g.InfraRepo, "PostgreSQL platform (CloudNativePG) managed by cnpg-console")
	if err != nil {
		writeError(w, http.StatusBadGateway, "création du repo: "+err.Error())
		return
	}

	// Seed sur la branche configurée (InfraBranch). auto_init n'a créé que la
	// branche par défaut ; on crée InfraBranch à partir d'elle si nécessaire.
	base := repo.DefaultBranch
	if base == "" {
		base = "master"
	}
	target := g.InfraBranch
	if target != base {
		sha, err := s.gh.BranchSHA(ctx, g.InfraRepo, base)
		if err != nil {
			writeError(w, http.StatusBadGateway, "SHA branche "+base+": "+err.Error())
			return
		}
		if err := s.gh.CreateBranch(ctx, g.InfraRepo, target, sha); err != nil {
			writeError(w, http.StatusBadGateway, "création branche "+target+": "+err.Error())
			return
		}
	}

	files := s.scaffoldFiles()
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	for _, p := range paths {
		if err := s.gh.PutFile(ctx, g.InfraRepo, p, target, []byte(files[p]), "", "cnpg-console: scaffold helm-cnpg platform chart ("+p+")"); err != nil {
			writeError(w, http.StatusBadGateway, "écriture "+p+": "+err.Error())
			return
		}
	}

	steps := []string{
		"Relire les values (storageClass, placement des pods, edge/DNS, cloud) dans le repo.",
		"Bootstrap ArgoCD (une seule fois) : kubectl apply -f argocd/root-app.yaml",
		"ArgoCD synchronise l'opérateur CNPG en premier, puis les clusters + pg-edge.",
		"Ensuite, créer des clusters directement depuis cnpg-console (les PR iront sur ce repo).",
	}
	if target != base {
		steps = append([]string{"Note : la branche par défaut du repo est « " + base + " » ; le chart a été posé sur « " + target + " » (branche configurée). Pense à définir « " + target + " » comme branche par défaut si souhaité."}, steps...)
	}

	writeJSON(w, http.StatusOK, bootstrapResponse{
		Repo:      g.Owner + "/" + g.InfraRepo,
		RepoURL:   repo.HTMLURL,
		Branch:    target,
		Files:     paths,
		NextSteps: steps,
	})
}
