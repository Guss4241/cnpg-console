package httpapi

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"cnpg-console/internal/config"
)

// testServer construit un Server minimal pour tester le scaffold.
func testServer() *Server {
	return &Server{cfg: &config.Config{
		GitHub: config.GitHubConfig{Owner: "acme", InfraRepo: "helm-cnpg.tool", InfraBranch: "master", InfraValuesPath: "values-acme.yaml"},
		ArgoCD: config.ArgoCDConfig{Namespace: "argocd", Project: "default", ClustersApp: "cnpg-clusters"},
		CNPG:   config.CNPGConfig{HostnameSuffix: ".pg.acme.test"},
	}}
}

func TestScaffoldFilesPresent(t *testing.T) {
	files := testServer().scaffoldFiles()
	for _, p := range []string{
		"Chart.yaml", "values.yaml", "values-acme.yaml", "README.md",
		"templates/_helpers.tpl", "templates/namespaces.yaml", "templates/clusters.yaml",
		"templates/delegation.yaml", "templates/edge-service.yaml",
		"argocd/root-app.yaml", "argocd/apps/cnpg-operator.yaml", "argocd/apps/cnpg-clusters.yaml",
	} {
		if _, ok := files[p]; !ok {
			t.Errorf("fichier scaffold manquant: %s", p)
		}
	}
	// Substitutions injectées.
	if !strings.Contains(files["argocd/root-app.yaml"], "git@github.com:acme/helm-cnpg.tool.git") {
		t.Errorf("repoURL non substituée dans root-app")
	}
	if !strings.Contains(files["argocd/apps/cnpg-clusters.yaml"], "- values.yaml") ||
		!strings.Contains(files["argocd/apps/cnpg-clusters.yaml"], "- values-acme.yaml") {
		t.Errorf("valueFiles non substitués:\n%s", files["argocd/apps/cnpg-clusters.yaml"])
	}
}

// TestScaffoldRendersWithHelm écrit le chart généré et le valide avec helm
// (lint + template avec un cluster d'exemple). Skippé si helm est absent.
func TestScaffoldRendersWithHelm(t *testing.T) {
	helm, err := exec.LookPath("helm")
	if err != nil {
		t.Skip("helm absent — validation du rendu ignorée")
	}
	dir := t.TempDir()
	for p, content := range testServer().scaffoldFiles() {
		full := filepath.Join(dir, p)
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// Overlay exerçant les boucles (clusters + délégation + placement + edge).
	overlay := filepath.Join(dir, "test-cluster.yaml")
	os.WriteFile(overlay, []byte(`
postgres:
  nodeSelector: { env: prd }
  tolerations:
    - { key: env, operator: Equal, value: prd, effect: NoSchedule }
clusters:
  - name: demo
    instances: 3
    storage: 20Gi
    port: 5433
    database: demo_db
    owner: demo_app
    team:
      repoURL: git@github.com:acme/pg-demo.git
      repoRevision: main
      repoPath: manifests
      group: pg-demo-admins
`), 0o644)

	if out, err := exec.Command(helm, "lint", dir).CombinedOutput(); err != nil {
		t.Fatalf("helm lint a échoué: %v\n%s", err, out)
	}
	out, err := exec.Command(helm, "template", "test", dir, "-f", overlay).CombinedOutput()
	if err != nil {
		t.Fatalf("helm template a échoué: %v\n%s", err, out)
	}
	s := string(out)
	for _, want := range []string{
		"kind: Cluster", "kind: Namespace", "kind: AppProject", "kind: Application",
		"kind: RoleBinding", "kind: ConfigMap", "kind: Service",
		"name: demo", "aws-load-balancer-type", "demo.pg.acme.test",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("rendu helm ne contient pas %q", want)
		}
	}
}
