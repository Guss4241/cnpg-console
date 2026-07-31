// Command cnpg-console : petite UI GitOps pour provisionner des clusters
// PostgreSQL CNPG (prepare → PR ; finalize → sync ArgoCD).
package main

import (
	"errors"
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"cnpg-console/internal/argocd"
	"cnpg-console/internal/auth"
	"cnpg-console/internal/config"
	"cnpg-console/internal/github"
	"cnpg-console/internal/httpapi"
	"cnpg-console/internal/web"
)

var version = "dev"

func main() {
	configPath := flag.String("config", envOr("CNPG_CONFIG", "config.yaml"), "chemin du fichier de configuration YAML")
	flag.Parse()

	cfg, err := config.Load(*configPath)
	if err != nil {
		log.Fatalf("config: %v", err)
	}

	var gh *github.Client
	if cfg.GitHubEnabled() {
		gh = github.New(cfg.GitHub.Owner, cfg.GitHub.Token)
	} else {
		log.Printf("github: token absent — endpoints clusters/prepare indisponibles jusqu'à configuration")
	}

	var argo *argocd.Client
	if cfg.ArgoCDEnabled() {
		argo = argocd.New(cfg.ArgoCD.Server, cfg.ArgoCD.Token, cfg.ArgoCD.Insecure)
	} else {
		log.Printf("argocd: non configuré — finalize indisponible jusqu'à configuration")
	}

	secure := envOr("CNPG_COOKIE_SECURE", "false") == "true"
	sessions := auth.NewSessionManager(cfg.Session.Secret, 12*time.Hour, secure)
	authn := auth.NewLocal(cfg.Auth.Username, cfg.Auth.Password)

	spa, err := web.Handler()
	if err != nil {
		log.Printf("web: SPA indisponible (%v) — l'API reste servie", err)
	}

	srv := httpapi.NewServer(httpapi.Deps{
		Config:   cfg,
		GitHub:   gh,
		ArgoCD:   argo,
		Authn:    authn,
		Sessions: sessions,
		Secret:   cfg.Session.Secret,
		Version:  version,
		Web:      spa,
	})

	httpServer := &http.Server{
		Addr:              cfg.Server.Addr,
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}

	go func() {
		log.Printf("cnpg-console %s — écoute sur %s (github=%v, argocd=%v)", version, cfg.Server.Addr, cfg.GitHubEnabled(), cfg.ArgoCDEnabled())
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("serveur: %v", err)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	log.Println("arrêt en cours…")
	_ = httpServer.Close()
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
