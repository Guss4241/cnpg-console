package httpapi

// Accès Kubernetes OPTIONNEL et local-admin : cnpg-console peut créer, pour toi,
// le Secret basic-auth attendu par un DatabaseRole (mot de passe généré, jamais
// committé) au lieu de te laisser lancer la commande kubectl à la main.
//
// Choix d'implémentation : shell-out `kubectl` (pas de client-go) — cohérent avec
// le design « binaire léger », et colle à la notion de « contexte kubectl » côté
// utilisateur. Dégradation gracieuse : si kubectl/kubeconfig est absent, l'API le
// signale (available=false) et l'UI retombe sur l'affichage de la commande manuelle.
//
// Sécurité : exec.Command à arguments séparés (jamais de shell → pas d'injection) ;
// le contexte demandé est validé contre la liste réelle ; namespace/nom/username
// sont validés (DNS-1123 / identifiant PG) ; le mot de passe est passé au manifest
// via STDIN (jamais en argv, donc invisible dans `ps`) ; le Secret n'est jamais
// écrasé s'il existe déjà.

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var (
	reDNS1123 = regexp.MustCompile(`^[a-z0-9]([-a-z0-9.]{0,251}[a-z0-9])?$`)
	rePGUser  = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)
)

// kubectlBin renvoie le binaire kubectl (override via CNPG_KUBECTL).
func kubectlBin() string {
	if b := strings.TrimSpace(os.Getenv("CNPG_KUBECTL")); b != "" {
		return b
	}
	return "kubectl"
}

func kubectlPresent() bool {
	_, err := exec.LookPath(kubectlBin())
	return err == nil
}

// listContexts renvoie les contextes du kubeconfig et le contexte courant.
func listContexts(ctx context.Context) (names []string, current string) {
	if out, err := exec.CommandContext(ctx, kubectlBin(), "config", "get-contexts", "-o", "name").Output(); err == nil {
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			if l := strings.TrimSpace(line); l != "" {
				names = append(names, l)
			}
		}
	}
	if out, err := exec.CommandContext(ctx, kubectlBin(), "config", "current-context").Output(); err == nil {
		current = strings.TrimSpace(string(out))
	}
	return names, current
}

type kubeContextsResponse struct {
	Available bool     `json:"available"` // true = kubectl présent ET au moins un contexte
	Current   string   `json:"current,omitempty"`
	Contexts  []string `json:"contexts"`
}

// GET /api/kube/contexts : liste les contextes kubectl disponibles.
func (s *Server) handleKubeContexts(w http.ResponseWriter, r *http.Request) {
	resp := kubeContextsResponse{Contexts: []string{}}
	if !s.cfg.KubeEnabled() || !kubectlPresent() {
		writeJSON(w, http.StatusOK, resp) // available=false → l'UI garde la commande manuelle
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	names, current := listContexts(ctx)
	resp.Contexts = names
	resp.Current = current
	resp.Available = len(names) > 0
	writeJSON(w, http.StatusOK, resp)
}

type createSecretRequest struct {
	Context   string `json:"context"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Username  string `json:"username"`
	Password  string `json:"password"`
}

type createSecretResponse struct {
	Created   bool   `json:"created"`
	Existed   bool   `json:"existed"`
	Context   string `json:"context"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Message   string `json:"message"`
}

// POST /api/kube/secret : crée le Secret basic-auth s'il n'existe pas déjà.
func (s *Server) handleCreateSecret(w http.ResponseWriter, r *http.Request) {
	if !s.cfg.KubeEnabled() {
		writeError(w, http.StatusForbidden, "création assistée de Secret désactivée (kube.enabled=false) — crée le Secret à la main (commande fournie)")
		return
	}
	if !kubectlPresent() {
		writeError(w, http.StatusServiceUnavailable, "kubectl indisponible : crée le Secret à la main (commande fournie ci-dessus)")
		return
	}
	var req createSecretRequest
	if !readJSON(w, r, &req) {
		return
	}
	req.Context = strings.TrimSpace(req.Context)
	req.Namespace = strings.TrimSpace(req.Namespace)
	req.Name = strings.TrimSpace(req.Name)
	req.Username = strings.TrimSpace(req.Username)

	if req.Context == "" || req.Password == "" {
		writeError(w, http.StatusBadRequest, "contexte et mot de passe requis")
		return
	}
	if !reDNS1123.MatchString(req.Namespace) {
		writeError(w, http.StatusBadRequest, "namespace invalide")
		return
	}
	if !reDNS1123.MatchString(req.Name) {
		writeError(w, http.StatusBadRequest, "nom de Secret invalide")
		return
	}
	if !rePGUser.MatchString(req.Username) {
		writeError(w, http.StatusBadRequest, "username invalide")
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
	defer cancel()

	// Le contexte doit exister (whitelist contre la liste réelle).
	names, _ := listContexts(ctx)
	if !contains(names, req.Context) {
		writeError(w, http.StatusBadRequest, "contexte kubectl inconnu : "+req.Context)
		return
	}

	// Jamais d'écrasement : si le Secret existe déjà, on ne touche à rien.
	if err := exec.CommandContext(ctx, kubectlBin(), "--context", req.Context, "-n", req.Namespace,
		"get", "secret", req.Name).Run(); err == nil {
		writeJSON(w, http.StatusOK, createSecretResponse{
			Existed: true, Context: req.Context, Namespace: req.Namespace, Name: req.Name,
			Message: "le Secret existe déjà — non modifié",
		})
		return
	}

	// Création via manifest sur STDIN (mot de passe hors argv). `create` (pas `apply`)
	// → échoue si le Secret apparaît entre-temps, on n'écrase jamais.
	manifest, _ := json.Marshal(map[string]any{
		"apiVersion": "v1",
		"kind":       "Secret",
		"type":       "kubernetes.io/basic-auth",
		"metadata":   map[string]string{"name": req.Name, "namespace": req.Namespace},
		"stringData": map[string]string{"username": req.Username, "password": req.Password},
	})
	cmd := exec.CommandContext(ctx, kubectlBin(), "--context", req.Context, "-n", req.Namespace, "create", "-f", "-")
	cmd.Stdin = bytes.NewReader(manifest)
	var errbuf bytes.Buffer
	cmd.Stderr = &errbuf
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(errbuf.String())
		if msg == "" {
			msg = err.Error()
		}
		writeError(w, http.StatusBadGateway, "échec de création du Secret : "+msg)
		return
	}
	writeJSON(w, http.StatusOK, createSecretResponse{
		Created: true, Context: req.Context, Namespace: req.Namespace, Name: req.Name,
		Message: "Secret créé dans le contexte " + req.Context,
	})
}

func contains(ss []string, v string) bool {
	for _, s := range ss {
		if s == v {
			return true
		}
	}
	return false
}
