// Package clusterspec est le cœur métier de cnpg-console : parser la liste des
// clusters d'un values GitOps (chart helm-cnpg), valider un nouveau cluster,
// allouer un port NLB libre, et rendre l'entrée YAML à insérer.
//
// Le parsing (yaml) sert à connaître l'état existant (noms/ports) et à valider.
// L'ÉCRITURE se fait par insertion TEXTUELLE (voir Insert) afin de préserver
// commentaires et mise en forme du fichier → diff de PR propre.
package clusterspec

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

// Cluster décrit un cluster PostgreSQL CNPG piloté par le chart helm-cnpg.
type Cluster struct {
	Name      string `json:"name"`
	Instances int    `json:"instances"`
	Storage   string `json:"storage"`
	Port      int    `json:"port"`
	Database  string `json:"database"`
	Owner     string `json:"owner"`
	Team      *Team  `json:"team,omitempty"`
}

// Team décrit la délégation GitOps d'un cluster (repo d'équipe).
type Team struct {
	RepoURL      string `json:"repoUrl"`
	RepoRevision string `json:"repoRevision"`
	RepoPath     string `json:"repoPath"`
	Group        string `json:"group,omitempty"`
}

// PortBase est le premier port NLB alloué (5432 = port PostgreSQL par défaut).
const PortBase = 5432

var (
	// reName : label DNS RFC1123 (nom k8s du cluster). Immuable après création.
	reName = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)
	// rePGIdent : identifiant PostgreSQL recommandé (underscore, pas de tiret →
	// évite le double-quote en SQL). Commence par lettre/underscore.
	rePGIdent = regexp.MustCompile(`^[a-z_][a-z0-9_]*$`)
	// reStorage : quantité k8s (ex. 50Gi).
	reStorage = regexp.MustCompile(`^[1-9][0-9]*(Mi|Gi|Ti)$`)
)

// --- parsing de l'existant ---

type valuesFile struct {
	Clusters []rawCluster `yaml:"clusters"`
}

type rawCluster struct {
	Name      string   `yaml:"name"`
	Instances int      `yaml:"instances"`
	Storage   string   `yaml:"storage"`
	Port      int      `yaml:"port"`
	Database  string   `yaml:"database"`
	Owner     string   `yaml:"owner"`
	Team      *rawTeam `yaml:"team"`
}

type rawTeam struct {
	RepoURL      string `yaml:"repoURL"`
	RepoRevision string `yaml:"repoRevision"`
	RepoPath     string `yaml:"repoPath"`
	Group        string `yaml:"group"`
}

// ParseExisting extrait la liste des clusters d'un fichier de values.
func ParseExisting(data []byte) ([]Cluster, error) {
	var vf valuesFile
	if err := yaml.Unmarshal(data, &vf); err != nil {
		return nil, fmt.Errorf("parse values: %w", err)
	}
	out := make([]Cluster, 0, len(vf.Clusters))
	for _, r := range vf.Clusters {
		c := Cluster{
			Name: r.Name, Instances: r.Instances, Storage: r.Storage,
			Port: r.Port, Database: r.Database, Owner: r.Owner,
		}
		if r.Team != nil {
			c.Team = &Team{RepoURL: r.Team.RepoURL, RepoRevision: r.Team.RepoRevision, RepoPath: r.Team.RepoPath, Group: r.Team.Group}
		}
		out = append(out, c)
	}
	return out, nil
}

// --- allocation de port ---

// AllocatePort renvoie le plus petit port libre >= PortBase non utilisé par
// l'existant.
func AllocatePort(existing []Cluster) int {
	used := make(map[int]bool, len(existing))
	for _, c := range existing {
		used[c.Port] = true
	}
	p := PortBase
	for used[p] {
		p++
	}
	return p
}

// --- validation ---

// Validate contrôle un nouveau cluster vis-à-vis de l'existant. Le port doit
// déjà être renseigné (via AllocatePort) et unique.
func Validate(c Cluster, existing []Cluster) error {
	if !reName.MatchString(c.Name) || len(c.Name) > 40 {
		return fmt.Errorf("name %q invalide : label DNS minuscule (a-z0-9-), 40 car. max, IMMUABLE", c.Name)
	}
	if !rePGIdent.MatchString(c.Database) || len(c.Database) > 63 {
		return fmt.Errorf("database %q invalide : identifiant PostgreSQL (a-z0-9_, commence par lettre/_), underscore recommandé", c.Database)
	}
	if !rePGIdent.MatchString(c.Owner) || len(c.Owner) > 63 {
		return fmt.Errorf("owner %q invalide : identifiant PostgreSQL (a-z0-9_, commence par lettre/_)", c.Owner)
	}
	if !reStorage.MatchString(c.Storage) {
		return fmt.Errorf("storage %q invalide : quantité k8s attendue (ex. 50Gi)", c.Storage)
	}
	if c.Instances != 1 && c.Instances != 3 {
		return fmt.Errorf("instances=%d invalide : 1 (mono) ou 3 (HA)", c.Instances)
	}
	if c.Port < PortBase {
		return fmt.Errorf("port=%d invalide : >= %d attendu", c.Port, PortBase)
	}
	for _, e := range existing {
		if e.Name == c.Name {
			return fmt.Errorf("cluster %q existe déjà", c.Name)
		}
		if e.Port == c.Port {
			return fmt.Errorf("port %d déjà utilisé par le cluster %q", c.Port, e.Name)
		}
	}
	if c.Team != nil && c.Team.RepoURL == "" {
		return fmt.Errorf("team.repoUrl requis quand la délégation est activée")
	}
	return nil
}

// --- rendu de l'entrée YAML ---

// RenderEntry produit le bloc YAML (indentation 2 espaces sous `clusters:`) de
// l'entrée du cluster, prêt à être inséré dans le fichier de values.
func RenderEntry(c Cluster) string {
	var b strings.Builder
	fmt.Fprintf(&b, "  - name: %s\n", c.Name)
	fmt.Fprintf(&b, "    instances: %d\n", c.Instances)
	fmt.Fprintf(&b, "    storage: %s\n", c.Storage)
	fmt.Fprintf(&b, "    port: %d\n", c.Port)
	fmt.Fprintf(&b, "    database: %s\n", c.Database)
	fmt.Fprintf(&b, "    owner: %s\n", c.Owner)
	if c.Team != nil {
		rev := c.Team.RepoRevision
		if rev == "" {
			rev = "master"
		}
		path := c.Team.RepoPath
		if path == "" {
			path = "manifests"
		}
		b.WriteString("    team:\n")
		fmt.Fprintf(&b, "      repoURL: %s\n", c.Team.RepoURL)
		fmt.Fprintf(&b, "      repoRevision: %s\n", rev)
		fmt.Fprintf(&b, "      repoPath: %s\n", path)
		if c.Team.Group != "" {
			fmt.Fprintf(&b, "      group: %s\n", c.Team.Group)
		}
	}
	return b.String()
}

// Insert insère textuellement l'entrée rendue à la fin de la liste `clusters:`
// du fichier, en préservant le reste (commentaires/mise en forme). Gère le cas
// `clusters: []` (liste vide) et une liste existante suivie d'une autre clé de
// premier niveau (ex. `edge:`).
func Insert(fileText, entry string) (string, error) {
	lines := strings.Split(fileText, "\n")
	clustersIdx := -1
	empty := false
	for i, ln := range lines {
		t := strings.TrimRight(ln, " \t")
		if t == "clusters:" {
			clustersIdx = i
			break
		}
		if t == "clusters: []" {
			clustersIdx = i
			empty = true
			break
		}
	}
	if clustersIdx < 0 {
		return "", fmt.Errorf("clé `clusters:` introuvable dans le fichier de values")
	}

	if empty {
		// Remplace `clusters: []` par `clusters:` + entrée.
		lines[clustersIdx] = "clusters:"
		newLines := append([]string{}, lines[:clustersIdx+1]...)
		newLines = append(newLines, strings.Split(strings.TrimRight(entry, "\n"), "\n")...)
		newLines = append(newLines, lines[clustersIdx+1:]...)
		return strings.Join(newLines, "\n"), nil
	}

	// Trouve la frontière : première ligne APRÈS clusters: qui est une clé de
	// premier niveau (non indentée, non vide, non commentaire) → fin de liste.
	boundary := len(lines)
	for i := clustersIdx + 1; i < len(lines); i++ {
		ln := lines[i]
		trimmed := strings.TrimSpace(ln)
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		if !strings.HasPrefix(ln, " ") && !strings.HasPrefix(ln, "\t") {
			boundary = i
			break
		}
	}
	// Recule la frontière au-dessus d'éventuelles lignes vides/commentaires de
	// fin, pour insérer juste après le dernier élément de liste.
	insertAt := boundary
	for insertAt > clustersIdx+1 {
		prev := strings.TrimSpace(lines[insertAt-1])
		if prev == "" || strings.HasPrefix(prev, "#") {
			insertAt--
			continue
		}
		break
	}

	entryLines := strings.Split(strings.TrimRight(entry, "\n"), "\n")
	newLines := append([]string{}, lines[:insertAt]...)
	newLines = append(newLines, entryLines...)
	newLines = append(newLines, lines[insertAt:]...)
	return strings.Join(newLines, "\n"), nil
}

// SortedNames renvoie les noms de clusters triés (pour affichage stable).
func SortedNames(cs []Cluster) []string {
	names := make([]string, 0, len(cs))
	for _, c := range cs {
		names = append(names, c.Name)
	}
	sort.Strings(names)
	return names
}
