package clusterspec

import (
	"strings"
	"testing"
)

const sampleValues = `# Overlay consoneoTech (EKS eu-west-3).
clusters:
  # Cluster de l'analyste data (preprod).
  - name: data-pprod        # IMMUABLE
    instances: 1
    storage: 50Gi
    port: 5432
    database: data_pprod
    owner: data_elie
    team:
      repoURL: git@github.com:Consoneo/pg-data-pprod.git
      repoRevision: master
      repoPath: manifests

edge:
  nlb:
    internal: true
`

func TestParseExisting(t *testing.T) {
	cs, err := ParseExisting([]byte(sampleValues))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(cs) != 1 {
		t.Fatalf("attendu 1 cluster, obtenu %d", len(cs))
	}
	c := cs[0]
	if c.Name != "data-pprod" || c.Port != 5432 || c.Database != "data_pprod" || c.Owner != "data_elie" {
		t.Fatalf("cluster mal parsé: %+v", c)
	}
	if c.Team == nil || c.Team.RepoURL != "git@github.com:Consoneo/pg-data-pprod.git" {
		t.Fatalf("team mal parsée: %+v", c.Team)
	}
}

func TestAllocatePort(t *testing.T) {
	cs := []Cluster{{Name: "a", Port: 5432}, {Name: "b", Port: 5433}, {Name: "c", Port: 5435}}
	if got := AllocatePort(cs); got != 5434 {
		t.Fatalf("attendu 5434 (premier libre), obtenu %d", got)
	}
	if got := AllocatePort(nil); got != PortBase {
		t.Fatalf("liste vide → %d attendu, obtenu %d", PortBase, got)
	}
}

func TestValidate(t *testing.T) {
	existing := []Cluster{{Name: "data-pprod", Port: 5432}}
	base := Cluster{Name: "analytics", Instances: 1, Storage: "20Gi", Port: 5433, Database: "analytics", Owner: "analytics_app"}
	if err := Validate(base, existing); err != nil {
		t.Fatalf("cluster valide rejeté: %v", err)
	}

	cases := map[string]func(*Cluster){
		"nom majuscule":      func(c *Cluster) { c.Name = "Analytics" },
		"nom underscore":     func(c *Cluster) { c.Name = "an_alytics" },
		"db tiret":           func(c *Cluster) { c.Database = "an-alytics" },
		"owner tiret":        func(c *Cluster) { c.Owner = "an-alytics" },
		"storage sans unité": func(c *Cluster) { c.Storage = "20" },
		"instances 2":        func(c *Cluster) { c.Instances = 2 },
		"nom dupliqué":       func(c *Cluster) { c.Name = "data-pprod" },
		"port dupliqué":      func(c *Cluster) { c.Port = 5432 },
	}
	for name, mut := range cases {
		c := base
		mut(&c)
		if err := Validate(c, existing); err == nil {
			t.Errorf("cas %q : erreur attendue, aucune obtenue", name)
		}
	}
}

func TestInsertPreservesAndAppends(t *testing.T) {
	existing, _ := ParseExisting([]byte(sampleValues))
	nc := Cluster{
		Name: "analytics", Instances: 3, Storage: "20Gi",
		Port:     AllocatePort(existing),
		Database: "analytics", Owner: "analytics_app",
		Team: &Team{RepoURL: "git@github.com:Consoneo/pg-analytics.git"},
	}
	if nc.Port != 5433 {
		t.Fatalf("port attendu 5433, obtenu %d", nc.Port)
	}
	out, err := Insert(sampleValues, RenderEntry(nc))
	if err != nil {
		t.Fatalf("insert: %v", err)
	}
	// L'entrée est insérée AVANT la clé top-level `edge:`.
	if strings.Index(out, "name: analytics") > strings.Index(out, "edge:") {
		t.Fatalf("nouvelle entrée insérée après edge: — frontière mal détectée\n%s", out)
	}
	// Re-parse : l'existant est préservé et le nouveau cluster présent.
	cs, err := ParseExisting([]byte(out))
	if err != nil {
		t.Fatalf("re-parse: %v", err)
	}
	if len(cs) != 2 {
		t.Fatalf("attendu 2 clusters après insert, obtenu %d\n%s", len(cs), out)
	}
	var found *Cluster
	for i := range cs {
		if cs[i].Name == "analytics" {
			found = &cs[i]
		}
	}
	if found == nil {
		t.Fatalf("cluster analytics absent après insert")
	}
	if found.Instances != 3 || found.Port != 5433 || found.Team == nil || found.Team.RepoPath != "manifests" {
		t.Fatalf("cluster analytics mal rendu: %+v team=%+v", found, found.Team)
	}
	// Le commentaire d'origine est toujours là (préservation).
	if !strings.Contains(out, "# Overlay consoneoTech") {
		t.Fatalf("commentaire d'en-tête perdu")
	}
}

func TestInsertEmptyList(t *testing.T) {
	src := "clusters: []\nedge:\n  nlb:\n    internal: true\n"
	nc := Cluster{Name: "first", Instances: 1, Storage: "10Gi", Port: 5432, Database: "first", Owner: "first_app"}
	out, err := Insert(src, RenderEntry(nc))
	if err != nil {
		t.Fatalf("insert vide: %v", err)
	}
	cs, err := ParseExisting([]byte(out))
	if err != nil || len(cs) != 1 || cs[0].Name != "first" {
		t.Fatalf("insert sur liste vide raté: %v / %+v\n%s", err, cs, out)
	}
}

func TestParseAndCompareStorage(t *testing.T) {
	if _, err := ParseStorage("20"); err == nil {
		t.Errorf("20 sans unité aurait dû être rejeté")
	}
	if cmp, _ := CompareStorage("80Gi", "50Gi"); cmp != 1 {
		t.Errorf("80Gi > 50Gi attendu (1), obtenu %d", cmp)
	}
	if cmp, _ := CompareStorage("50Gi", "50Gi"); cmp != 0 {
		t.Errorf("50Gi == 50Gi attendu (0), obtenu %d", cmp)
	}
	// 1024Mi == 1Gi (comparaison inter-unités).
	if cmp, _ := CompareStorage("1024Mi", "1Gi"); cmp != 0 {
		t.Errorf("1024Mi == 1Gi attendu (0), obtenu %d", cmp)
	}
	if cmp, _ := CompareStorage("40Gi", "50Gi"); cmp != -1 {
		t.Errorf("40Gi < 50Gi attendu (-1), obtenu %d", cmp)
	}
}

func TestEditStorage(t *testing.T) {
	out, err := EditStorage(sampleValues, "data-pprod", "80Gi")
	if err != nil {
		t.Fatalf("edit storage: %v", err)
	}
	cs, err := ParseExisting([]byte(out))
	if err != nil || len(cs) != 1 || cs[0].Storage != "80Gi" {
		t.Fatalf("storage non mis à jour: %v / %+v\n%s", err, cs, out)
	}
	// Le reste de l'entrée est préservé (commentaire IMMUABLE + team + port).
	if !strings.Contains(out, "# IMMUABLE") || cs[0].Team == nil || cs[0].Port != 5432 {
		t.Fatalf("édition non chirurgicale:\n%s", out)
	}
	if _, err := EditStorage(sampleValues, "absent", "80Gi"); err == nil {
		t.Errorf("cluster absent aurait dû être rejeté")
	}
}

func TestAddTeamBlock(t *testing.T) {
	// Entrée SANS délégation.
	src := "clusters:\n" +
		"  - name: solo\n    instances: 1\n    storage: 10Gi\n    port: 5432\n    database: solo\n    owner: solo_app\n" +
		"edge:\n  nlb:\n    internal: true\n"
	team := Team{RepoURL: "git@github.com:Consoneo/pg-solo.git", RepoRevision: "main"}
	out, err := AddTeamBlock(src, "solo", team)
	if err != nil {
		t.Fatalf("add team: %v", err)
	}
	cs, err := ParseExisting([]byte(out))
	if err != nil || len(cs) != 1 || cs[0].Team == nil || cs[0].Team.RepoURL != team.RepoURL {
		t.Fatalf("team block non ajouté: %v / %+v\n%s", err, cs, out)
	}
	if cs[0].Team.RepoPath != "manifests" || cs[0].Team.RepoRevision != "main" {
		t.Fatalf("valeurs team incorrectes: %+v", cs[0].Team)
	}
	// La clé top-level edge: est préservée après le bloc inséré.
	if strings.Index(out, "team:") > strings.Index(out, "edge:") {
		t.Fatalf("team inséré après edge: — frontière ratée\n%s", out)
	}
	// Déjà délégué → erreur.
	if _, err := AddTeamBlock(sampleValues, "data-pprod", team); err == nil {
		t.Errorf("cluster déjà délégué aurait dû être rejeté")
	}
}

func TestParseManifests(t *testing.T) {
	doc := "apiVersion: postgresql.cnpg.io/v1\nkind: Database\nmetadata:\n  name: reports\n  namespace: pg-x\n" +
		"spec:\n  cluster:\n    name: x\n  name: reports_db\n  owner: reports_app\n  ensure: present\n" +
		"---\n" +
		"apiVersion: postgresql.cnpg.io/v1\nkind: DatabaseRole\nmetadata:\n  name: reports-app\n  namespace: pg-x\n" +
		"spec:\n  cluster:\n    name: x\n  name: reports_app\n  login: true\n"
	ms := ParseManifests([]byte(doc))
	if len(ms) != 2 {
		t.Fatalf("attendu 2 manifests, obtenu %d: %+v", len(ms), ms)
	}
	if ms[0].Kind != "Database" || ms[0].Name != "reports_db" || ms[0].Owner != "reports_app" || ms[0].Cluster != "x" {
		t.Fatalf("Database mal parsé: %+v", ms[0])
	}
	if ms[1].Kind != "DatabaseRole" || ms[1].Name != "reports_app" || !ms[1].Login {
		t.Fatalf("DatabaseRole mal parsé: %+v", ms[1])
	}
}
