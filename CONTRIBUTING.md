# Contributing to cnpg-console

Thanks for your interest in improving **cnpg-console** — a small GitOps web UI to
provision and manage [CloudNativePG](https://cloudnative-pg.io/) PostgreSQL
clusters in self-service (prepare → Pull Request → merge → finalize → ArgoCD
sync). Contributions of all kinds are welcome: bug reports, documentation,
tests, and code.

By participating in this project you agree to abide by our
[Code of Conduct](CODE_OF_CONDUCT.md).

## Before you start

- **Search first.** Check the
  [issues](https://github.com/Guss4241/cnpg-console/issues) and
  [pull requests](https://github.com/Guss4241/cnpg-console/pulls) to avoid
  duplicating work.
- **Open an issue for anything non-trivial** before writing code, so we can
  agree on the approach. Small fixes (typos, obvious bugs) can go straight to a
  PR.
- **Never commit secrets.** This repository is public. GitHub and ArgoCD tokens,
  the session secret and the admin password are always injected through the
  environment (`*Env` config fields), never hard-coded or committed.
  `config.example.yaml` is the only config template that belongs in git;
  `config.yaml` is git-ignored.

## Design principles (please preserve)

cnpg-console is deliberately **minimal and safe by construction**. Keep these
invariants when contributing:

- **Nothing reaches a cluster without a human-merged PR.** Writes to the GitOps
  repos happen on a branch and open a Pull Request; the `finalize` step only
  triggers an ArgoCD sync *after* the PR is merged.
- **Two tokens, no cluster access.** The tool holds only a GitHub token and an
  ArgoCD token. It never talks to Kubernetes or PostgreSQL directly, and has no
  cloud/DNS credentials (DNS is an external-dns annotation carried in the PR).
- **Config-driven, no embedded environment data.** No cluster names, hostnames,
  or secrets belong in the code — everything is config.
- **Clean PR diffs.** Edits to the values file are done by *textual* insertion /
  in-place edit (`internal/clusterspec`) to preserve comments and formatting.

## Development setup

Prerequisites:

- **Go >= 1.24**. If your system `go` is older, install a user-space toolchain
  in `~/go-sdk/go` — the `Makefile` detects it automatically (`GOTOOLCHAIN=local`).
- **Node.js >= 20** and npm (to build the Vue 3 SPA).

```bash
make web-build   # build the Vue SPA -> internal/web/dist
make build       # build the Go binary (embeds internal/web/dist)
make test        # unit tests (clusterspec + manifest round-trips) — no external deps
make vet         # go vet
make fmt         # gofmt
make run         # run the server locally
```

Build order matters: **`make web-build` then `make build`** (the binary embeds
`internal/web/dist` via `go:embed`; `make build` also triggers the SPA build).
For frontend work, `cd web && npm run dev` serves the SPA on `:5173` and proxies
`/api` to the Go binary on `:8080`.

Minimal local run (login + forms; the cluster list needs a GitHub token):

```bash
CNPG_ADMIN_PASSWORD=changeme \
CNPG_SESSION_SECRET=local-dev-secret \
CNPG_GITHUB_TOKEN=... \
CNPG_ADDR=:8088 ./bin/cnpg-console -config config.yaml
```

## Project layout

```
cmd/cnpg-console      entry point (wiring, flags, graceful shutdown)
internal/config       YAML config + env overlay, secret resolution (fail-fast)
internal/clusterspec  the correctness core — parse/validate/allocate port,
                      textual insert & in-place edit, manifest rendering (tested)
internal/github       minimal REST client (get/put/delete file, branch, PR, repo)
internal/argocd       minimal REST client (get status, sync with optional prune)
internal/auth         local auth + HMAC-signed session cookie + CSRF
internal/httpapi      router, middleware, REST handlers, HMAC finalize token
internal/web          embed of the built SPA
web/                  Vue 3 + Vite source
deploy/               Dockerfile (distroless) + Helm chart (deploy/helm/cnpg-console)
```

## Making changes

1. **Fork** the repository and create a topic branch off `master`
   (`git checkout -b fix/short-description`).
2. **Keep changes focused.** One logical change per pull request.
3. **Match the existing style.** Go code is formatted with `gofmt`/`go vet`.
   Comments in this codebase are written in French — keep new comments consistent
   with the file you touch.
4. **Add or update tests.** `internal/clusterspec` is the heart of the tool: any
   change to parsing, validation, port allocation, textual editing or manifest
   rendering must come with tests. New handlers get unit tests where practical.
5. **Update docs** (`README.md`, chart `values.yaml`, this file) when behavior,
   config, or deployment changes.

## Before opening a pull request

Run the local checks and make sure they pass:

```bash
make web-build && make build   # it must compile with the SPA embedded
make test                      # unit tests green
gofmt -l .                     # no formatting diffs (should print nothing)
go vet ./...                   # no vet warnings
helm lint deploy/helm/cnpg-console   # if you touched the chart
```

If your change affects the write path (PR creation, in-place edits, delete +
prune), describe how you validated it. **Never run destructive tests against a
production cluster or repo** — a valid `prepare`/`scale`/add/delete against a
real GitOps repo opens real PRs and, on delete + finalize, really drops the
object (`reclaimPolicy: delete`).

## Pull request expectations

- Fill in the [pull request template](.github/PULL_REQUEST_TEMPLATE.md).
- Reference the issue it closes (`Closes #123`).
- Keep the history clean; a short, imperative commit summary is enough
  (e.g. `clusterspec: preserve inline comment on in-place storage edit`).
- Expect review feedback — it's a conversation, not a gate.

## Reporting security issues

**Do not open a public issue for security vulnerabilities.** Follow the process
in [SECURITY.md](SECURITY.md).

## License

By contributing, you agree that your contributions will be licensed under the
[MIT License](LICENSE) that covers this project.
