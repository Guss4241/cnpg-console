package config

import (
	"bytes"
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	defaultAddr           = ":8080"
	defaultUsername       = "admin"
	defaultInfraBranch    = "master"
	defaultInfraValues    = "values-consoneotech.yaml"
	defaultTeamPrefix     = "pg-"
	defaultHostnameSuffix = ".pg.consoneo.tech"
	defaultPortBase       = 5432
	defaultClustersApp    = "cnpg-clusters"
	defaultProject        = "it-project"
)

// Load lit le YAML, applique les défauts, résout les secrets (fail-fast pour
// ceux qui sont requis) et valide.
func Load(path string) (*Config, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("lecture config %s: %w", path, err)
	}
	var c Config
	dec := yaml.NewDecoder(bytes.NewReader(raw))
	dec.KnownFields(true)
	if err := dec.Decode(&c); err != nil {
		return nil, fmt.Errorf("parse config %s: %w", path, err)
	}
	applyDefaults(&c)
	if v := os.Getenv("CNPG_ADDR"); v != "" {
		c.Server.Addr = v
	}
	if err := resolveSecrets(&c); err != nil {
		return nil, err
	}
	if err := validate(&c); err != nil {
		return nil, err
	}
	return &c, nil
}

func applyDefaults(c *Config) {
	if c.Server.Addr == "" {
		c.Server.Addr = defaultAddr
	}
	if c.Auth.Username == "" {
		c.Auth.Username = defaultUsername
	}
	if c.GitHub.InfraBranch == "" {
		c.GitHub.InfraBranch = defaultInfraBranch
	}
	if c.GitHub.InfraValuesPath == "" {
		c.GitHub.InfraValuesPath = defaultInfraValues
	}
	if c.GitHub.TeamRepoPrefix == "" {
		c.GitHub.TeamRepoPrefix = defaultTeamPrefix
	}
	if c.CNPG.HostnameSuffix == "" {
		c.CNPG.HostnameSuffix = defaultHostnameSuffix
	}
	if c.CNPG.PortBase == 0 {
		c.CNPG.PortBase = defaultPortBase
	}
	if c.ArgoCD.ClustersApp == "" {
		c.ArgoCD.ClustersApp = defaultClustersApp
	}
	if c.ArgoCD.Project == "" {
		c.ArgoCD.Project = defaultProject
	}
}

func resolveSecrets(c *Config) error {
	var err error
	// Secret de session : requis (signature des cookies).
	if c.Session.Secret, err = requireEnv("session.secretEnv", c.Session.SecretEnv); err != nil {
		return err
	}
	// Mot de passe admin : requis (auth locale MVP).
	if c.Auth.Password, err = requireEnv("auth.passwordEnv", c.Auth.PasswordEnv); err != nil {
		return err
	}
	// Token GitHub : optionnel au démarrage (mais requis pour prepare).
	if c.GitHub.TokenEnv != "" {
		c.GitHub.Token = os.Getenv(c.GitHub.TokenEnv)
	}
	// Token ArgoCD : optionnel (requis pour finalize).
	if c.ArgoCD.TokenEnv != "" {
		c.ArgoCD.Token = os.Getenv(c.ArgoCD.TokenEnv)
	}
	return nil
}

func requireEnv(field, envName string) (string, error) {
	if envName == "" {
		return "", fmt.Errorf("config: %s doit référencer une variable d'environnement", field)
	}
	v := os.Getenv(envName)
	if v == "" {
		return "", fmt.Errorf("config: variable d'environnement %s (référencée par %s) absente ou vide", envName, field)
	}
	return v, nil
}

func validate(c *Config) error {
	if c.GitHub.Owner == "" {
		return fmt.Errorf("config: github.owner requis")
	}
	if c.GitHub.InfraRepo == "" {
		return fmt.Errorf("config: github.infraRepo requis")
	}
	return nil
}
