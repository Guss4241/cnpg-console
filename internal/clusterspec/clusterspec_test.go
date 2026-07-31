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
