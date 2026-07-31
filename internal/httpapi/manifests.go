package httpapi

import (
	"crypto/rand"
	"fmt"
	"strings"
)

// Générateurs de manifests CNPG (Database / DatabaseRole) pour le repo d'équipe.
// Formes conformes aux CRD postgresql.cnpg.io/v1 : `spec.cluster.name` est un
// objet (référence), pas une chaîne. Le namespace est `pg-<cluster>` (convention
// du chart helm-cnpg). metadata.name doit être DNS-1123 → underscores → tirets ;
// le nom logique PostgreSQL (avec underscores) va dans `spec.name`.

// k8sName convertit un identifiant PostgreSQL (underscores) en nom k8s DNS-1123.
func k8sName(pgIdent string) string {
	return strings.ReplaceAll(pgIdent, "_", "-")
}

// renderDatabase rend un objet Database CNPG.
func renderDatabase(cluster, dbName, owner string) string {
	var b strings.Builder
	b.WriteString("apiVersion: postgresql.cnpg.io/v1\n")
	b.WriteString("kind: Database\n")
	b.WriteString("metadata:\n")
	fmt.Fprintf(&b, "  name: %s\n", k8sName(dbName))
	fmt.Fprintf(&b, "  namespace: pg-%s\n", cluster)
	b.WriteString("spec:\n")
	b.WriteString("  cluster:\n")
	fmt.Fprintf(&b, "    name: %s\n", cluster)
	fmt.Fprintf(&b, "  name: %s\n", dbName)
	fmt.Fprintf(&b, "  owner: %s\n", owner)
	b.WriteString("  ensure: present\n")
	// Cycle de vie piloté par GitOps : retirer le manifest (+ prune ArgoCD) droppe
	// réellement la base PostgreSQL. Suppression toujours volontaire (confirm + PR).
	b.WriteString("  databaseReclaimPolicy: delete\n")
	return b.String()
}

// renderDatabaseRole rend un objet DatabaseRole CNPG. Si login et
// passwordSecret non vide, référence le Secret de mot de passe (jamais committé —
// créé hors-git). Sinon rôle NOLOGIN / sans mot de passe géré.
func renderDatabaseRole(cluster, roleName string, login bool, passwordSecret string) string {
	var b strings.Builder
	b.WriteString("apiVersion: postgresql.cnpg.io/v1\n")
	b.WriteString("kind: DatabaseRole\n")
	b.WriteString("metadata:\n")
	fmt.Fprintf(&b, "  name: %s\n", k8sName(roleName))
	fmt.Fprintf(&b, "  namespace: pg-%s\n", cluster)
	b.WriteString("spec:\n")
	b.WriteString("  cluster:\n")
	fmt.Fprintf(&b, "    name: %s\n", cluster)
	fmt.Fprintf(&b, "  name: %s\n", roleName)
	b.WriteString("  ensure: present\n")
	// Cycle de vie GitOps : retirer le manifest (+ prune) droppe réellement le rôle.
	b.WriteString("  databaseRoleReclaimPolicy: delete\n")
	fmt.Fprintf(&b, "  login: %t\n", login)
	if login && passwordSecret != "" {
		b.WriteString("  passwordSecret:\n")
		fmt.Fprintf(&b, "    name: %s\n", passwordSecret)
	}
	return b.String()
}

const pwAlphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"

// genPassword génère un mot de passe alphanumérique de n caractères (crypto/rand).
func genPassword(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		// Improbable ; en dernier recours on renvoie une chaîne fixe non vide
		// (l'appelant reste responsable de la sécurité). Ne devrait jamais arriver.
		return "changeme-" + strings.Repeat("x", n)
	}
	out := make([]byte, n)
	for i := range b {
		out[i] = pwAlphabet[int(b[i])%len(pwAlphabet)]
	}
	return string(out)
}

// secretKubectl produit la commande kubectl créant le Secret basic-auth attendu
// par le DatabaseRole (hors-git : ne transite jamais par une PR).
func secretKubectl(ns, secretName, username, password string) string {
	return fmt.Sprintf(
		"kubectl -n %s create secret generic %s "+
			"--type=kubernetes.io/basic-auth "+
			"--from-literal=username=%s --from-literal=password='%s'",
		ns, secretName, username, password)
}
