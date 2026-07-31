package httpapi

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"cnpg-console/internal/clusterspec"
	"cnpg-console/internal/github"
)

// ─── GET /api/clusters/{name} : détail d'un cluster ───────────────────────────

type dbInfo struct {
	Name   string `json:"name"`
	Owner  string `json:"owner,omitempty"`
	Source string `json:"source"` // bootstrap | manifest
	File   string `json:"file,omitempty"`
}

type roleInfo struct {
	Name   string `json:"name"`
	Login  bool   `json:"login"`
	Source string `json:"source"` // bootstrap | manifest
	File   string `json:"file,omitempty"`
}

type clusterDetail struct {
	Cluster        clusterspec.Cluster `json:"cluster"`
	Delegated      bool                `json:"delegated"`
	TeamRepoName   string              `json:"teamRepoName,omitempty"`
	TeamRepoURL    string              `json:"teamRepoUrl,omitempty"`
	Namespace      string              `json:"namespace"`
	Hostname       string              `json:"hostname"`
	Databases      []dbInfo            `json:"databases"`
	Roles          []roleInfo          `json:"roles"`
	ManifestsError string              `json:"manifestsError,omitempty"`
}

func (s *Server) handleClusterDetail(w http.ResponseWriter, r *http.Request) {
	if s.gh == nil {
		writeError(w, http.StatusServiceUnavailable, "GitHub non configuré (token absent)")
		return
	}
	name := r.PathValue("name")
	ctx := r.Context()
	g := s.cfg.GitHub

	data, _, err := s.gh.GetFile(ctx, g.InfraRepo, g.InfraValuesPath, g.InfraBranch)
	if err != nil {
		writeError(w, http.StatusBadGateway, "lecture values infra: "+err.Error())
		return
	}
	existing, err := clusterspec.ParseExisting(data)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	c, ok := clusterspec.FindByName(existing, name)
	if !ok {
		writeError(w, http.StatusNotFound, "cluster "+name+" introuvable")
		return
	}

	d := clusterDetail{
		Cluster:   c,
		Delegated: c.Team != nil,
		Namespace: "pg-" + name,
		Hostname:  name + s.cfg.CNPG.HostnameSuffix,
	}
	// Base + owner applicatifs créés au bootstrap (toujours présents).
	d.Databases = []dbInfo{{Name: c.Database, Owner: c.Owner, Source: "bootstrap"}}
	d.Roles = []roleInfo{{Name: c.Owner, Login: true, Source: "bootstrap"}}

	// Extras gérés dans le repo d'équipe (si délégué et accessible).
	if c.Team != nil {
		teamName := g.TeamRepoName(name)
		d.TeamRepoName = teamName
		rev := c.Team.RepoRevision
		if repo, rerr := s.gh.GetRepo(ctx, teamName); rerr == nil {
			d.TeamRepoURL = repo.HTMLURL
			if rev == "" {
				rev = repo.DefaultBranch
			}
		}
		if rev == "" {
			rev = "master"
		}
		path := c.Team.RepoPath
		if path == "" {
			path = "manifests"
		}
		entries, lerr := s.gh.ListDir(ctx, teamName, path, rev)
		if lerr != nil {
			d.ManifestsError = "repo d'équipe inaccessible (" + teamName + ") : " + lerr.Error()
		} else {
			for _, e := range entries {
				if e.Type != "file" || !(strings.HasSuffix(e.Name, ".yaml") || strings.HasSuffix(e.Name, ".yml")) {
					continue
				}
				content, _, gerr := s.gh.GetFile(ctx, teamName, e.Path, rev)
				if gerr != nil {
					continue
				}
				for _, m := range clusterspec.ParseManifests(content) {
					if m.Cluster != "" && m.Cluster != name {
						continue
					}
					switch m.Kind {
					case "Database":
						d.Databases = append(d.Databases, dbInfo{Name: firstNonEmpty(m.Name, m.MetaName), Owner: m.Owner, Source: "manifest", File: e.Name})
					case "DatabaseRole":
						d.Roles = append(d.Roles, roleInfo{Name: firstNonEmpty(m.Name, m.MetaName), Login: m.Login, Source: "manifest", File: e.Name})
					}
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, d)
}

func firstNonEmpty(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// ─── réponse commune des mutations (prepare) ─────────────────────────────────

type mutationResponse struct {
	Action        string        `json:"action"`
	Cluster       string        `json:"cluster"`
	PRs           []prLink      `json:"prs"`
	TeamRepoURL   string        `json:"teamRepoUrl,omitempty"`
	Promoted      bool          `json:"promoted,omitempty"`
	Secret        *secretResult `json:"secret,omitempty"`
	FinalizeToken string        `json:"finalizeToken"`
	NextSteps     []string      `json:"nextSteps"`
}

type prLink struct {
	Repo string `json:"repo"`
	URL  string `json:"url"`
}

type secretResult struct {
	Name     string `json:"name"`      // nom du Secret k8s
	Username string `json:"username"`  // = nom du rôle
	Password string `json:"password"`  // affiché UNE fois, jamais committé
	Kubectl  string `json:"kubectl"`   // commande de création hors-git
}

// openFilePR crée une branche, committe un fichier (sha vide = création) et ouvre
// une PR head->base. Renvoie la PR créée.
func (s *Server) openFilePR(ctx context.Context, repo, path, baseBranch string, content []byte, sha, branchPrefix, message, body string) (github.PR, error) {
	baseSHA, err := s.gh.BranchSHA(ctx, repo, baseBranch)
	if err != nil {
		return github.PR{}, fmt.Errorf("SHA branche %s: %w", baseBranch, err)
	}
	branch := branchPrefix + randSuffix()
	if err := s.gh.CreateBranch(ctx, repo, branch, baseSHA); err != nil {
		return github.PR{}, fmt.Errorf("création branche: %w", err)
	}
	if err := s.gh.PutFile(ctx, repo, path, branch, content, sha, message); err != nil {
		return github.PR{}, fmt.Errorf("commit %s: %w", path, err)
	}
	pr, err := s.gh.CreatePR(ctx, repo, branch, baseBranch, message, body)
	if err != nil {
		return github.PR{}, fmt.Errorf("ouverture PR: %w", err)
	}
	return pr, nil
}

// ─── POST /api/clusters/{name}/scale : scale-up du volume ────────────────────

type scaleRequest struct {
	Storage string `json:"storage"`
}

func (s *Server) handleScale(w http.ResponseWriter, r *http.Request) {
	if s.gh == nil {
		writeError(w, http.StatusServiceUnavailable, "GitHub non configuré (token absent)")
		return
	}
	name := r.PathValue("name")
	var req scaleRequest
	if !readJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	g := s.cfg.GitHub
	newStorage := strings.TrimSpace(req.Storage)

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
	c, ok := clusterspec.FindByName(existing, name)
	if !ok {
		writeError(w, http.StatusNotFound, "cluster "+name+" introuvable")
		return
	}
	// Scale-UP uniquement (un PVC ne peut pas rétrécir).
	cmp, err := clusterspec.CompareStorage(newStorage, c.Storage)
	if err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if cmp <= 0 {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("scale-down interdit : le volume ne peut que croître (actuel %s, demandé %s)", c.Storage, newStorage))
		return
	}
	newText, err := clusterspec.EditStorage(string(content), name, newStorage)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	msg := fmt.Sprintf("cnpg-console: scale-up du volume %s (%s → %s)", name, c.Storage, newStorage)
	body := fmt.Sprintf("Expansion en ligne du volume du cluster `%s`.\n\n- **avant** : %s\n- **après** : %s\n\n⚠️ Scale-up uniquement (un PVC ne peut pas rétrécir).\n", name, c.Storage, newStorage)
	pr, err := s.openFilePR(ctx, g.InfraRepo, g.InfraValuesPath, g.InfraBranch, []byte(newText), sha, "cnpg-console/scale-"+name+"-", msg, body)
	if err != nil {
		writeError(w, http.StatusBadGateway, err.Error())
		return
	}
	tok := s.signToken(finalizePayload{
		Cluster: name, Action: "scale",
		PRs:  []prRef{{Repo: g.InfraRepo, Number: pr.Number}},
		Apps: []string{s.cfg.ArgoCD.ClustersApp},
	})
	writeJSON(w, http.StatusOK, mutationResponse{
		Action: "scale", Cluster: name,
		PRs:           []prLink{{Repo: g.InfraRepo, URL: pr.HTMLURL}},
		FinalizeToken: tok,
		NextSteps: []string{
			"Relire et merger la PR : " + pr.HTMLURL,
			"Une fois mergée, cliquer sur « Finaliser » pour déclencher le sync ArgoCD (expansion du PVC).",
		},
	})
}

// ─── POST /api/clusters/{name}/databases et /roles : ajout d'extras ──────────

type addDatabaseRequest struct {
	Database string `json:"database"`
	Owner    string `json:"owner"`
}

func (s *Server) handleAddDatabase(w http.ResponseWriter, r *http.Request) {
	if s.gh == nil {
		writeError(w, http.StatusServiceUnavailable, "GitHub non configuré (token absent)")
		return
	}
	name := r.PathValue("name")
	var req addDatabaseRequest
	if !readJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	db := strings.TrimSpace(req.Database)
	owner := strings.TrimSpace(req.Owner)

	c, content, sha, ok := s.loadCluster(w, ctx, name)
	if !ok {
		return
	}
	if owner == "" {
		owner = c.Owner // par défaut : l'owner applicatif du cluster (existe déjà)
	}
	if err := clusterspec.ValidatePGIdent(db, "database"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err := clusterspec.ValidatePGIdent(owner, "owner"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	manifest := renderDatabase(name, db, owner)
	file := "db-" + k8sName(db) + ".yaml"
	msg := "cnpg-console: base " + db + " sur le cluster " + name
	body := fmt.Sprintf("Ajout de la base `%s` (owner `%s`) au cluster `%s` (namespace `pg-%s`).\n", db, owner, name, name)

	s.finishTeamManifest(w, ctx, c, content, sha, "add-db", file, []byte(manifest), msg, body, nil)
}

type addRoleRequest struct {
	Name  string `json:"name"`
	Login bool   `json:"login"`
}

func (s *Server) handleAddRole(w http.ResponseWriter, r *http.Request) {
	if s.gh == nil {
		writeError(w, http.StatusServiceUnavailable, "GitHub non configuré (token absent)")
		return
	}
	name := r.PathValue("name")
	var req addRoleRequest
	if !readJSON(w, r, &req) {
		return
	}
	ctx := r.Context()
	role := strings.TrimSpace(req.Name)

	c, content, sha, ok := s.loadCluster(w, ctx, name)
	if !ok {
		return
	}
	if err := clusterspec.ValidatePGIdent(role, "name"); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	// Mot de passe généré + Secret hors-git (jamais committé), si login.
	var secret *secretResult
	pwSecretName := ""
	if req.Login {
		pwSecretName = k8sName(role) + "-pw"
		pw := genPassword(24)
		secret = &secretResult{
			Name:     pwSecretName,
			Username: role,
			Password: pw,
			Kubectl:  secretKubectl("pg-"+name, pwSecretName, role, pw),
		}
	}
	manifest := renderDatabaseRole(name, role, req.Login, pwSecretName)
	file := "role-" + k8sName(role) + ".yaml"
	msg := "cnpg-console: rôle " + role + " sur le cluster " + name
	body := fmt.Sprintf("Ajout du rôle `%s` (login=%t) au cluster `%s` (namespace `pg-%s`).\n", role, req.Login, name, name)

	s.finishTeamManifest(w, ctx, c, content, sha, "add-role", file, []byte(manifest), msg, body, secret)
}

// loadCluster lit le values infra et renvoie l'entrée du cluster (+ content/sha
// du fichier pour un éventuel commit de promotion). Écrit l'erreur HTTP et renvoie
// ok=false en cas de problème.
func (s *Server) loadCluster(w http.ResponseWriter, ctx context.Context, name string) (clusterspec.Cluster, []byte, string, bool) {
	g := s.cfg.GitHub
	content, sha, err := s.gh.GetFile(ctx, g.InfraRepo, g.InfraValuesPath, g.InfraBranch)
	if err != nil {
		writeError(w, http.StatusBadGateway, "lecture values infra: "+err.Error())
		return clusterspec.Cluster{}, nil, "", false
	}
	existing, err := clusterspec.ParseExisting(content)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return clusterspec.Cluster{}, nil, "", false
	}
	c, ok := clusterspec.FindByName(existing, name)
	if !ok {
		writeError(w, http.StatusNotFound, "cluster "+name+" introuvable")
		return clusterspec.Cluster{}, nil, "", false
	}
	return c, content, sha, true
}

// finishTeamManifest ajoute un manifest (Database/DatabaseRole) au repo d'équipe,
// en promouvant le cluster en délégation si nécessaire (PR infra + PR repo équipe).
// Écrit directement la réponse HTTP.
func (s *Server) finishTeamManifest(w http.ResponseWriter, ctx context.Context, c clusterspec.Cluster, infraContent []byte, infraSHA, action, file string, manifest []byte, msg, body string, secret *secretResult) {
	g := s.cfg.GitHub
	teamName := g.TeamRepoName(c.Name)
	delegatedApp := "pg-" + c.Name + "-content"

	var prs []prRef
	var prLinks []prLink
	var apps []string
	var steps []string
	teamRepoURL := ""
	promoted := false

	if c.Team == nil {
		// PROMOTION : créer/seed le repo d'équipe + PR infra (bloc team:) + PR manifest.
		promoted = true
		repo, err := s.ensureTeamRepo(ctx, teamName, c.Name)
		if err != nil {
			writeError(w, http.StatusBadGateway, "repo d'équipe: "+err.Error())
			return
		}
		teamRepoURL = repo.HTMLURL
		team := clusterspec.Team{
			RepoURL:      fmt.Sprintf("git@github.com:%s/%s.git", g.Owner, teamName),
			RepoRevision: repo.DefaultBranch,
			RepoPath:     "manifests",
		}
		newText, err := clusterspec.AddTeamBlock(string(infraContent), c.Name, team)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		infraMsg := "cnpg-console: délégation GitOps du cluster " + c.Name
		infraBody := fmt.Sprintf("Active la délégation du cluster `%s` : AppProject + Application ArgoCD pointant vers `%s`.\n\nRequis pour gérer bases/users depuis le repo d'équipe.\n", c.Name, team.RepoURL)
		infraPR, err := s.openFilePR(ctx, g.InfraRepo, g.InfraValuesPath, g.InfraBranch, []byte(newText), infraSHA, "cnpg-console/delegate-"+c.Name+"-", infraMsg, infraBody)
		if err != nil {
			writeError(w, http.StatusBadGateway, "PR délégation: "+err.Error())
			return
		}
		base := repo.DefaultBranch
		if base == "" {
			base = "master"
		}
		teamPR, err := s.openFilePR(ctx, teamName, "manifests/"+file, base, manifest, "", "cnpg-console/"+action+"-", msg, body)
		if err != nil {
			writeError(w, http.StatusBadGateway, "PR repo d'équipe: "+err.Error())
			return
		}
		prs = []prRef{{Repo: g.InfraRepo, Number: infraPR.Number}, {Repo: teamName, Number: teamPR.Number}}
		prLinks = []prLink{{Repo: g.InfraRepo, URL: infraPR.HTMLURL}, {Repo: teamName, URL: teamPR.HTMLURL}}
		apps = []string{s.cfg.ArgoCD.ClustersApp, delegatedApp}
		steps = []string{
			"Merger la PR de délégation (infra) : " + infraPR.HTMLURL,
			"Merger la PR du repo d'équipe : " + teamPR.HTMLURL,
		}
	} else {
		// Déjà délégué : simple PR sur le repo d'équipe.
		rev := c.Team.RepoRevision
		if repo, rerr := s.gh.GetRepo(ctx, teamName); rerr == nil {
			teamRepoURL = repo.HTMLURL
			if rev == "" {
				rev = repo.DefaultBranch
			}
		}
		if rev == "" {
			rev = "master"
		}
		dir := c.Team.RepoPath
		if dir == "" {
			dir = "manifests"
		}
		fullPath := file
		if dir != "." {
			fullPath = dir + "/" + file
		}
		// Refuse d'écraser un manifest existant.
		if _, _, gerr := s.gh.GetFile(ctx, teamName, fullPath, rev); gerr == nil {
			writeError(w, http.StatusConflict, "un manifest existe déjà : "+fullPath+" (nom déjà utilisé ?)")
			return
		}
		teamPR, err := s.openFilePR(ctx, teamName, fullPath, rev, manifest, "", "cnpg-console/"+action+"-", msg, body)
		if err != nil {
			writeError(w, http.StatusBadGateway, "PR repo d'équipe: "+err.Error())
			return
		}
		prs = []prRef{{Repo: teamName, Number: teamPR.Number}}
		prLinks = []prLink{{Repo: teamName, URL: teamPR.HTMLURL}}
		apps = []string{delegatedApp}
		steps = []string{"Merger la PR du repo d'équipe : " + teamPR.HTMLURL}
	}

	if secret != nil {
		steps = append(steps, "AVANT de finaliser : créer le Secret de mot de passe (hors-git) — voir la commande kubectl ci-dessous (mot de passe affiché une seule fois).")
	}
	steps = append(steps, "Une fois la/les PR mergée(s), cliquer sur « Finaliser » pour déclencher le sync ArgoCD.")
	if promoted {
		steps = append(steps, "Note : l'enregistrement du repo d'équipe dans ArgoCD (helm-argocd.tool) reste une étape différée si l'app déléguée n'existe pas encore.")
	}

	tok := s.signToken(finalizePayload{Cluster: c.Name, Action: action, PRs: prs, Apps: apps})
	writeJSON(w, http.StatusOK, mutationResponse{
		Action: action, Cluster: c.Name,
		PRs: prLinks, TeamRepoURL: teamRepoURL, Promoted: promoted,
		Secret: secret, FinalizeToken: tok, NextSteps: steps,
	})
}

// ─── DELETE /api/clusters/{name}/databases|roles/{target} : suppression ──────

// teamObject = un objet CNPG lu dans le repo d'équipe (avec son fichier/sha).
type teamObject struct {
	Manifest clusterspec.Manifest
	File     string
	Path     string
	SHA      string
}

// resolveTeam calcule le repo d'équipe, la révision, le dossier de manifests et
// l'URL (best-effort) d'un cluster délégué.
func (s *Server) resolveTeam(ctx context.Context, c clusterspec.Cluster) (teamName, rev, dir, url string) {
	teamName = s.cfg.GitHub.TeamRepoName(c.Name)
	if c.Team != nil {
		rev = c.Team.RepoRevision
	}
	if repo, err := s.gh.GetRepo(ctx, teamName); err == nil {
		url = repo.HTMLURL
		if rev == "" {
			rev = repo.DefaultBranch
		}
	}
	if rev == "" {
		rev = "master"
	}
	dir = "manifests"
	if c.Team != nil && c.Team.RepoPath != "" {
		dir = c.Team.RepoPath
	}
	return
}

// listTeamObjects énumère les objets Database/DatabaseRole du repo d'équipe.
func (s *Server) listTeamObjects(ctx context.Context, teamName, dir, rev string) ([]teamObject, error) {
	entries, err := s.gh.ListDir(ctx, teamName, dir, rev)
	if err != nil {
		return nil, err
	}
	var out []teamObject
	for _, e := range entries {
		if e.Type != "file" || !(strings.HasSuffix(e.Name, ".yaml") || strings.HasSuffix(e.Name, ".yml")) {
			continue
		}
		content, sha, gerr := s.gh.GetFile(ctx, teamName, e.Path, rev)
		if gerr != nil {
			continue
		}
		for _, m := range clusterspec.ParseManifests(content) {
			out = append(out, teamObject{Manifest: m, File: e.Name, Path: e.Path, SHA: sha})
		}
	}
	return out, nil
}

// openDeletePR crée une branche, supprime un fichier et ouvre une PR.
func (s *Server) openDeletePR(ctx context.Context, repo, path, baseBranch, sha, branchPrefix, message, body string) (github.PR, error) {
	baseSHA, err := s.gh.BranchSHA(ctx, repo, baseBranch)
	if err != nil {
		return github.PR{}, fmt.Errorf("SHA branche %s: %w", baseBranch, err)
	}
	branch := branchPrefix + randSuffix()
	if err := s.gh.CreateBranch(ctx, repo, branch, baseSHA); err != nil {
		return github.PR{}, fmt.Errorf("création branche: %w", err)
	}
	if err := s.gh.DeleteFile(ctx, repo, path, branch, sha, message); err != nil {
		return github.PR{}, fmt.Errorf("suppression %s: %w", path, err)
	}
	pr, err := s.gh.CreatePR(ctx, repo, branch, baseBranch, message, body)
	if err != nil {
		return github.PR{}, fmt.Errorf("ouverture PR: %w", err)
	}
	return pr, nil
}

func matchManifest(m clusterspec.Manifest, target string) bool {
	return m.Name == target || m.MetaName == target || m.MetaName == k8sName(target)
}

func (s *Server) handleDeleteDatabase(w http.ResponseWriter, r *http.Request) {
	if s.gh == nil {
		writeError(w, http.StatusServiceUnavailable, "GitHub non configuré (token absent)")
		return
	}
	name := r.PathValue("name")
	target := r.PathValue("db")
	ctx := r.Context()

	c, _, _, ok := s.loadCluster(w, ctx, name)
	if !ok {
		return
	}
	if c.Team == nil {
		writeError(w, http.StatusBadRequest, "cluster non délégué : aucune base déléguée à supprimer")
		return
	}
	if target == c.Database {
		writeError(w, http.StatusBadRequest, "la base applicative (bootstrap) n'est pas supprimable — supprimer le cluster à la place")
		return
	}
	teamName, rev, dir, _ := s.resolveTeam(ctx, c)
	objs, err := s.listTeamObjects(ctx, teamName, dir, rev)
	if err != nil {
		writeError(w, http.StatusBadGateway, "lecture repo d'équipe: "+err.Error())
		return
	}
	var found *teamObject
	for i := range objs {
		if objs[i].Manifest.Kind == "Database" && matchManifest(objs[i].Manifest, target) {
			found = &objs[i]
			break
		}
	}
	if found == nil {
		writeError(w, http.StatusNotFound, "base déléguée introuvable : "+target)
		return
	}
	msg := "cnpg-console: suppression de la base " + target + " (cluster " + name + ")"
	body := fmt.Sprintf("Suppression de la base `%s` du cluster `%s`.\n\n⚠️ **Irréversible** : reclaimPolicy=delete → après merge + sync (prune), CNPG **droppe réellement** la base PostgreSQL.\n", target, name)
	pr, err := s.openDeletePR(ctx, teamName, found.Path, rev, found.SHA, "cnpg-console/del-db-"+name+"-", msg, body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "PR suppression: "+err.Error())
		return
	}
	s.respondDeletion(w, "del-db", name, teamName, pr)
}

func (s *Server) handleDeleteRole(w http.ResponseWriter, r *http.Request) {
	if s.gh == nil {
		writeError(w, http.StatusServiceUnavailable, "GitHub non configuré (token absent)")
		return
	}
	name := r.PathValue("name")
	target := r.PathValue("role")
	ctx := r.Context()

	c, _, _, ok := s.loadCluster(w, ctx, name)
	if !ok {
		return
	}
	if c.Team == nil {
		writeError(w, http.StatusBadRequest, "cluster non délégué : aucun rôle délégué à supprimer")
		return
	}
	if target == c.Owner {
		writeError(w, http.StatusBadRequest, "le rôle owner applicatif (bootstrap) n'est pas supprimable")
		return
	}
	teamName, rev, dir, _ := s.resolveTeam(ctx, c)
	objs, err := s.listTeamObjects(ctx, teamName, dir, rev)
	if err != nil {
		writeError(w, http.StatusBadGateway, "lecture repo d'équipe: "+err.Error())
		return
	}
	// Garde-fou : un rôle propriétaire d'une base ne peut pas être supprimé.
	var owned []string
	if c.Owner == target { // (déjà bloqué plus haut, mais on couvre le bootstrap)
		owned = append(owned, c.Database)
	}
	var found *teamObject
	for i := range objs {
		m := objs[i].Manifest
		if m.Kind == "Database" && m.Owner == target {
			owned = append(owned, firstNonEmpty(m.Name, m.MetaName))
		}
		if m.Kind == "DatabaseRole" && matchManifest(m, target) {
			found = &objs[i]
		}
	}
	if len(owned) > 0 {
		writeError(w, http.StatusConflict, "le rôle "+target+" est propriétaire de la/les base(s) "+strings.Join(owned, ", ")+" — réassigner l'owner avant suppression")
		return
	}
	if found == nil {
		writeError(w, http.StatusNotFound, "rôle délégué introuvable : "+target)
		return
	}
	msg := "cnpg-console: suppression du rôle " + target + " (cluster " + name + ")"
	body := fmt.Sprintf("Suppression du rôle `%s` du cluster `%s`.\n\n⚠️ **Irréversible** : reclaimPolicy=delete → après merge + sync (prune), CNPG **droppe réellement** le rôle PostgreSQL. Penser à supprimer le Secret de mot de passe associé si présent.\n", target, name)
	pr, err := s.openDeletePR(ctx, teamName, found.Path, rev, found.SHA, "cnpg-console/del-role-"+name+"-", msg, body)
	if err != nil {
		writeError(w, http.StatusBadGateway, "PR suppression: "+err.Error())
		return
	}
	s.respondDeletion(w, "del-role", name, teamName, pr)
}

func (s *Server) respondDeletion(w http.ResponseWriter, action, cluster, teamName string, pr github.PR) {
	delegatedApp := "pg-" + cluster + "-content"
	tok := s.signToken(finalizePayload{
		Cluster: cluster, Action: action,
		PRs:   []prRef{{Repo: teamName, Number: pr.Number}},
		Apps:  []string{delegatedApp},
		Prune: true,
	})
	writeJSON(w, http.StatusOK, mutationResponse{
		Action: action, Cluster: cluster,
		PRs:           []prLink{{Repo: teamName, URL: pr.HTMLURL}},
		FinalizeToken: tok,
		NextSteps: []string{
			"Relire et merger la PR de suppression : " + pr.HTMLURL,
			"Une fois mergée, cliquer sur « Finaliser » pour synchroniser avec prune (retire l'objet du cluster).",
		},
	})
}
