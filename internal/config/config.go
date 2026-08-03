// Package config charge la configuration de cnpg-console (YAML + overlay env).
// Principe s3ctl : le fichier ne contient JAMAIS de secret, seulement des
// références `*Env`. Les valeurs réelles sont lues depuis l'environnement au
// démarrage (fail-fast pour les secrets requis).
package config

// Config est la configuration résolue.
type Config struct {
	Server  ServerConfig  `yaml:"server"`
	Auth    AuthConfig    `yaml:"auth"`
	Session SessionConfig `yaml:"session"`
	GitHub  GitHubConfig  `yaml:"github"`
	CNPG    CNPGConfig    `yaml:"cnpg"`
	ArgoCD  ArgoCDConfig  `yaml:"argocd"`
}

type ServerConfig struct {
	Addr string `yaml:"addr"`
}

// AuthConfig : auth locale (MVP). L'OIDC pourra être ajouté ultérieurement.
type AuthConfig struct {
	Username    string `yaml:"username"`
	PasswordEnv string `yaml:"passwordEnv"`
	Password    string `yaml:"-"`
}

type SessionConfig struct {
	SecretEnv string `yaml:"secretEnv"`
	Secret    string `yaml:"-"`
}

// GitHubConfig : repos GitOps ciblés + token API (PR/lecture/création de repo).
type GitHubConfig struct {
	Owner    string `yaml:"owner"`    // ex. Consoneo
	TokenEnv string `yaml:"tokenEnv"` // PAT/App token (scope repo)

	// Repo infra portant la liste des clusters (chart helm-cnpg).
	InfraRepo       string `yaml:"infraRepo"`       // ex. helm-cnpg.tool
	InfraBranch     string `yaml:"infraBranch"`     // ex. master
	InfraValuesPath string `yaml:"infraValuesPath"` // ex. values-consoneotech.yaml

	// Délégation : préfixe du repo d'équipe (pg-<name>).
	TeamRepoPrefix string `yaml:"teamRepoPrefix"` // ex. pg-

	Token string `yaml:"-"`
}

// CNPGConfig : conventions du chart CNPG.
type CNPGConfig struct {
	HostnameSuffix string `yaml:"hostnameSuffix"` // ex. .pg.consoneo.tech
	PortBase       int    `yaml:"portBase"`       // ex. 5432
}

// ArgoCDConfig : endpoint + token pour déclencher les syncs.
type ArgoCDConfig struct {
	Server      string `yaml:"server"`   // ex. argo-cd.consoneo.tech
	TokenEnv    string `yaml:"tokenEnv"` // token de compte local ArgoCD
	Insecure    bool   `yaml:"insecure"`
	ClustersApp string `yaml:"clustersApp"` // ex. cnpg-clusters
	Project     string `yaml:"project"`     // ex. it-project
	Namespace   string `yaml:"namespace"`   // ns où tournent les Application ArgoCD (ex. argocd) — utilisé au bootstrap

	Token string `yaml:"-"`
}

// TeamRepoName renvoie le nom du repo d'équipe pour un cluster.
func (g GitHubConfig) TeamRepoName(cluster string) string {
	return g.TeamRepoPrefix + cluster
}

// GitHubEnabled indique si un token GitHub a été résolu.
func (c *Config) GitHubEnabled() bool { return c.GitHub.Token != "" }

// ArgoCDEnabled indique si ArgoCD est configuré (serveur + token).
func (c *Config) ArgoCDEnabled() bool { return c.ArgoCD.Server != "" && c.ArgoCD.Token != "" }
