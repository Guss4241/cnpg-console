package httpapi

import (
	"strings"
	"testing"

	"cnpg-console/internal/clusterspec"
)

func TestRenderDatabaseRoundTrip(t *testing.T) {
	y := renderDatabase("data-pprod", "reports_db", "reports_app")
	if !strings.Contains(y, "databaseReclaimPolicy: delete") {
		t.Errorf("reclaimPolicy=delete manquante:\n%s", y)
	}
	// spec.cluster.name doit être un OBJET (référence), pas une chaîne.
	if !strings.Contains(y, "cluster:\n    name: data-pprod") {
		t.Errorf("spec.cluster.name mal formé:\n%s", y)
	}
	ms := clusterspec.ParseManifests([]byte(y))
	if len(ms) != 1 || ms[0].Kind != "Database" || ms[0].Cluster != "data-pprod" || ms[0].Name != "reports_db" || ms[0].Owner != "reports_app" {
		t.Fatalf("Database non parsable/incorrect: %+v\n%s", ms, y)
	}
	// metadata.name DNS-1123 (underscore → tiret).
	if !strings.Contains(y, "name: reports-db") {
		t.Errorf("metadata.name devrait être DNS-safe:\n%s", y)
	}
}

func TestRenderDatabaseRoleRoundTrip(t *testing.T) {
	y := renderDatabaseRole("data-pprod", "reports_ro", true, "reports-ro-pw")
	if !strings.Contains(y, "databaseRoleReclaimPolicy: delete") || !strings.Contains(y, "login: true") {
		t.Errorf("champs attendus manquants:\n%s", y)
	}
	if !strings.Contains(y, "passwordSecret:\n    name: reports-ro-pw") {
		t.Errorf("passwordSecret mal formé:\n%s", y)
	}
	ms := clusterspec.ParseManifests([]byte(y))
	if len(ms) != 1 || ms[0].Kind != "DatabaseRole" || ms[0].Cluster != "data-pprod" || ms[0].Name != "reports_ro" || !ms[0].Login {
		t.Fatalf("DatabaseRole non parsable/incorrect: %+v\n%s", ms, y)
	}
	// Rôle sans login → pas de passwordSecret.
	y2 := renderDatabaseRole("c", "svc", false, "")
	if strings.Contains(y2, "passwordSecret") || !strings.Contains(y2, "login: false") {
		t.Errorf("rôle nologin mal rendu:\n%s", y2)
	}
}
