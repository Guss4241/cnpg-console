# cnpg-console

Petite UI **GitOps** pour provisionner des clusters PostgreSQL
[CloudNativePG](https://cloudnative-pg.io/) gérés par un chart type `helm-cnpg`.
Un binaire Go unique embarque une SPA Vue. **Aucun secret dans le code** : tout
est config (`config.yaml`) + variables d'environnement (`*Env`).

## Ce que ça fait

Un formulaire → un cluster opérationnel, en deux temps :

1. **Prepare** : lit la liste `clusters:` du repo GitOps, **alloue le prochain
   port NLB libre**, valide (nom k8s immuable, base/owner en underscore, storage,
   instances 1/3, unicité), puis **ouvre une Pull Request** ajoutant l'entrée du
   cluster. Si délégation : **crée le repo d'équipe** `pg-<nom>` (Database /
   DatabaseRole gérés par l'équipe) et ajoute le bloc `team`.
2. **Finalize** (après merge) : vérifie que la PR est mergée puis déclenche le
   **sync ArgoCD** (hard refresh + sync `ServerSideApply`), et restitue les infos
   de connexion (`<nom>.pg.consoneo.tech`, Secret `<nom>-app`).

Le DNS est géré par **external-dns en git** : le chart CNPG expose une annotation
`external-dns.alpha.kubernetes.io/hostname` générée par cluster — aucun droit
Route53 côté outil.

## Privilèges (minimaux)

| Capacité | Credential (env) |
|---|---|
| Lire repos / créer repo d'équipe / ouvrir PR | `CNPG_GITHUB_TOKEN` (PAT/App, scope repo) |
| Déclencher les syncs | `CNPG_ARGOCD_TOKEN` (compte local ArgoCD `apiKey`) |
| DNS | *aucun* (annotation external-dns dans la PR) |
| k8s | *aucun accès direct* (tout via l'API ArgoCD) |

## Dév local

```bash
cp config.example.yaml config.yaml   # adapter
export CNPG_SESSION_SECRET=dev-secret CNPG_ADMIN_PASSWORD=dev
export CNPG_GITHUB_TOKEN=...          # optionnel (clusters/prepare)
export CNPG_ARGOCD_TOKEN=...          # optionnel (finalize)
make web-build   # build la SPA -> internal/web/dist
make run         # http://127.0.0.1:8080
# ou, en dev front avec hot-reload : cd web && npm run dev (proxy /api -> :8080)
```

## Build & image

```bash
make build       # binaire bin/cnpg-console (embarque la SPA)
make docker      # image distroless multi-stage
```

## Déploiement (Helm)

Chart auto-contenu dans `deploy/helm/cnpg-console`. Secrets via `existingSecret`
(Vault / External Secrets recommandé). Ingress `nginx` + cert-manager.

```bash
helm upgrade --install cnpg-console deploy/helm/cnpg-console \
  --set image.repository=ghcr.io/<owner>/cnpg-console \
  --set ingress.enabled=true
```

## Statut / limites (MVP)

- Auth **locale** (OIDC/PKCE branchable ensuite).
- **Différé** : enregistrement du repo d'équipe dans ArgoCD (PR helm-argocd),
  déploiement d'external-dns sur le cluster cible, provisioning du PAT GitHub et
  du compte ArgoCD, secrets via Vault. Ces étapes sont signalées dans l'UI.

## Licence

[MIT](LICENSE)
