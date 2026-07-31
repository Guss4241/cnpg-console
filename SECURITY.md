# Security Policy

## Supported versions

cnpg-console is pre-1.0 and evolving quickly. Security fixes are applied to the
latest `master` and the most recent tagged release only.

| Version | Supported |
|---|---|
| `master` (latest) | ✅ |
| older tags | ❌ |

## Reporting a vulnerability

**Please do not report security vulnerabilities through public GitHub issues,
discussions, or pull requests.**

Instead, use GitHub's private vulnerability reporting:

1. Go to the **[Security tab](https://github.com/Guss4241/cnpg-console/security)**
   of the repository.
2. Click **"Report a vulnerability"** to open a private advisory visible only to
   the maintainers.

Please include, as far as you can:

- the type of issue (e.g. authentication bypass, CSRF, secret exposure, finalize
  token forgery/replay, unauthorized PR or sync);
- the affected component/file and version or commit;
- step-by-step reproduction instructions and a proof of concept if possible;
- the impact and any suggested mitigation.

You can expect an initial acknowledgement within a few days. We will keep you
informed of progress and coordinate a disclosure timeline with you once a fix is
available.

## Scope and threat model

cnpg-console drives **GitOps changes** to PostgreSQL clusters (opening PRs and
triggering ArgoCD syncs), so its security posture is a core feature. The tool is
designed to be minimal by construction: it holds only two tokens (GitHub +
ArgoCD), never talks to Kubernetes or PostgreSQL directly, and applies nothing
without a human-merged PR. Areas of particular interest for reports:

- **Secret handling.** Credentials (GitHub token, ArgoCD token, session secret,
  admin password) are resolved from environment variables referenced by `*Env`
  config fields. They must never be logged, returned by the API, or embedded in
  a git commit / PR. A generated role password is shown once in the UI and is
  **never committed** (the k8s Secret is created out-of-band via a `kubectl`
  command surfaced to the operator). A path that leaks a secret is a vulnerability.
- **Authentication & session.** Local auth with an HMAC-signed session cookie and
  CSRF protection (double-submit token) on all mutating requests. Report any
  bypass, fixation, or CSRF gap.
- **Prepare → finalize integrity.** A prepare step returns an HMAC-signed token
  that binds the exact cluster / PR(s) / app(s) to finalize (anti-tamper,
  anti-replay). `finalize` verifies every bound PR is **merged** before syncing.
  Report any way to finalize (sync, or a pruning sync that deletes objects) that
  a matching prepare did not authorize, or with an unmerged PR.
- **Write-path safety.** Ways to make the tool open a PR against, or sync, a repo
  or ArgoCD Application outside its configured scope; or to trick the in-place
  values editor / manifest renderer into producing an unintended change.
- **Delete guardrails.** Deletion targets only delegated (`manifest`-sourced)
  objects, refuses bootstrap objects, and refuses a role that owns a database.
  Report any way to bypass these and drop a protected object.

## Out of scope

- The security of the GitHub, ArgoCD, Kubernetes or PostgreSQL backends themselves.
- Misconfiguration of secrets by the operator (e.g. committing a real
  `config.yaml` with inline secrets — cnpg-console reads secrets from env by design).
- Denial of service from deliberately excessive input on a self-hosted instance.

## Good hygiene when running cnpg-console

- Inject all secrets via Kubernetes Secrets / Vault / External Secrets — never
  commit them. This repository's `.gitignore` excludes local config by default.
- Run the container as the provided non-root, distroless user.
- Restrict the GitHub token to the minimum repositories/permissions needed and
  the ArgoCD account to the specific project/applications it must sync.
