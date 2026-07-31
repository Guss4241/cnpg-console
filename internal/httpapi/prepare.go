package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net/http"
	"strings"

	"cnpg-console/internal/clusterspec"
	"cnpg-console/internal/github"
)

type prepareRequest struct {
	Name      string `json:"name"`
	Instances int    `json:"instances"`
	Storage   string `json:"storage"`
	Database  string `json:"database"`
	Owner     string `json:"owner"`
	Delegate  bool   `json:"delegate"`
}

type prepareResponse struct {
	Cluster       clusterspec.Cluster `json:"cluster"`
	InfraPRURL    string              `json:"infraPrUrl"`
	InfraPRNumber int                 `json:"infraPrNumber"`
	TeamRepoURL   string              `json:"teamRepoUrl,omitempty"`
	Hostname      string              `json:"hostname"`
	FinalizeToken string              `json:"finalizeToken"`
	NextSteps     []string            `json:"nextSteps"`
}

func (s *Server) handlePrepare(w http.ResponseWriter, r *http.Request) {
	if s.gh == nil {
		writeError(w, http.StatusServiceUnavailable, "GitHub non configuré (token absent)")
		return
	}
	var req prepareRequest
	if !readJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	g := s.cfg.GitHub

	// 1. État courant.
	content, sha, err := s.gh.GetFile(ctx, g.InfraRepo, g.InfraValuesPath, g.InfraBranch)
	if err != nil {
		writeError(w, http.StatusBadGateway, "lecture values infra: "+err.Error())
		return
	}
	existing, err := clusterspec.ParseExisting(content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	// 2. Construction du cluster + allocation de port.
	c := clusterspec.Cluster{
		Name:      strings.TrimSpace(req.Name),
		Instances: req.Instances,
		Storage:   strings.TrimSpace(req.Storage),
		Database:  strings.TrimSpace(req.Database),
		Owner:     strings.TrimSpace(req.Owner),
		Port:      clusterspec.AllocatePort(existing),
	}

	// 3. Délégation : création (idempotente) du repo d'équipe AVANT le rendu,
	//    pour connaître sa branche par défaut.
	teamRepoURL := ""
	delegatedApp := ""
	if req.Delegate {
		teamName := g.TeamRepoName(c.Name)
		repo, err := s.ensureTeamRepo(ctx, teamName, c.Name)
		if err != nil {
			writeError(w, http.StatusBadGateway, "repo d'équipe: "+err.Error())
			return
		}
		teamRepoURL = repo.HTMLURL
		c.Team = &clusterspec.Team{
			RepoURL:      fmt.Sprintf("git@github.com:%s/%s.git", g.Owner, teamName),
			RepoRevision: repo.DefaultBranch,
			RepoPath:     "manifests",
		}
		delegatedApp = "pg-" + c.Name + "-content"
	}

	// 4. Validation.
	if err := clusterspec.Validate(c, existing); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}

	// 5. Insertion textuelle + PR sur le repo infra.
	newText, err := clusterspec.Insert(string(content), clusterspec.RenderEntry(c))
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	branch := "cnpg-console/add-" + c.Name + "-" + randSuffix()
	baseSHA, err := s.gh.BranchSHA(ctx, g.InfraRepo, g.InfraBranch)
	if err != nil {
		writeError(w, http.StatusBadGateway, "SHA branche infra: "+err.Error())
		return
	}
	if err := s.gh.CreateBranch(ctx, g.InfraRepo, branch, baseSHA); err != nil {
		writeError(w, http.StatusBadGateway, "création branche: "+err.Error())
		return
	}
	msg := "cnpg-console: ajout du cluster " + c.Name
	if err := s.gh.PutFile(ctx, g.InfraRepo, g.InfraValuesPath, branch, []byte(newText), sha, msg); err != nil {
		writeError(w, http.StatusBadGateway, "commit values: "+err.Error())
		return
	}
	pr, err := s.gh.CreatePR(ctx, g.InfraRepo, branch, g.InfraBranch, msg, prBody(c, s.cfg.CNPG.HostnameSuffix))
	if err != nil {
		writeError(w, http.StatusBadGateway, "ouverture PR: "+err.Error())
		return
	}

	apps := []string{s.cfg.ArgoCD.ClustersApp}
	if delegatedApp != "" {
		apps = append(apps, delegatedApp)
	}
	tok := s.signToken(finalizePayload{
		Cluster: c.Name, Action: "create",
		PRs:  []prRef{{Repo: g.InfraRepo, Number: pr.Number}},
		Apps: apps,
	})
	steps := []string{
		fmt.Sprintf("Relire et merger la PR : %s", pr.HTMLURL),
	}
	if req.Delegate {
		steps = append(steps, "Enregistrer le repo d'équipe dans ArgoCD (helm-argocd.tool) — étape différée (manuelle pour l'instant).")
	}
	steps = append(steps, "Une fois la PR mergée, cliquer sur « Finaliser » pour déclencher le sync ArgoCD.")

	writeJSON(w, http.StatusOK, prepareResponse{
		Cluster:       c,
		InfraPRURL:    pr.HTMLURL,
		InfraPRNumber: pr.Number,
		TeamRepoURL:   teamRepoURL,
		Hostname:      c.Name + s.cfg.CNPG.HostnameSuffix,
		FinalizeToken: tok,
		NextSteps:     steps,
	})
}

// ensureTeamRepo crée le repo d'équipe s'il n'existe pas et y dépose le contenu
// de départ (README + manifests/.gitkeep + exemples).
func (s *Server) ensureTeamRepo(ctx context.Context, name, cluster string) (github.Repo, error) {
	exists, err := s.gh.RepoExists(ctx, name)
	if err != nil {
		return github.Repo{}, err
	}
	var repo github.Repo
	if exists {
		// Récupère la branche par défaut réelle du repo existant.
		repo, err = s.gh.GetRepo(ctx, name)
		if err != nil {
			return github.Repo{}, err
		}
	} else {
		repo, err = s.gh.CreateOrgRepo(ctx, name, "Bases & users du cluster PostgreSQL "+cluster+" (délégation GitOps)")
		if err != nil {
			return github.Repo{}, err
		}
	}
	br := repo.DefaultBranch
	if br == "" {
		br = "master"
	}
	// Contenu de départ (best-effort : ignore les conflits si déjà présents).
	exDB := "# EXEMPLE (non synchronisé). Copier dans manifests/ et adapter.\n" + renderDatabase(cluster, "exemple", "exemple_app")
	exRole := "# EXEMPLE (non synchronisé). Copier dans manifests/ et adapter.\n" + renderDatabaseRole(cluster, "exemple_app", true, "exemple-app-pw")
	_ = s.gh.PutFile(ctx, name, "manifests/.gitkeep", br, []byte(""), "", "cnpg-console: dossier des manifests")
	_ = s.gh.PutFile(ctx, name, "examples/database.yaml", br, []byte(exDB), "", "cnpg-console: exemple Database")
	_ = s.gh.PutFile(ctx, name, "examples/databaserole.yaml", br, []byte(exRole), "", "cnpg-console: exemple DatabaseRole")
	return repo, nil
}

func randSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func prBody(c clusterspec.Cluster, suffix string) string {
	var b strings.Builder
	b.WriteString("Ajout automatique d'un cluster PostgreSQL (cnpg-console).\n\n")
	fmt.Fprintf(&b, "- **cluster** : `%s` (namespace `pg-%s`)\n", c.Name, c.Name)
	fmt.Fprintf(&b, "- **base** : `%s` — **owner** : `%s`\n", c.Database, c.Owner)
	fmt.Fprintf(&b, "- **instances** : %d — **storage** : %s\n", c.Instances, c.Storage)
	fmt.Fprintf(&b, "- **port NLB** : %d — **hostname** : `%s%s`\n", c.Port, c.Name, suffix)
	if c.Team != nil {
		fmt.Fprintf(&b, "- **délégation** : repo `%s`\n", c.Team.RepoURL)
	}
	b.WriteString("\n⚠️ Nom de cluster/base **immuables** après création.\n")
	return b.String()
}

