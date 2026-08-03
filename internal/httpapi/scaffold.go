package httpapi

import "strings"

// Scaffold d'un repo d'infra "helm-cnpg" GÉNÉRIQUE (pas de spécifique
// Consoneo/AWS en dur — tout ce qui varie est paramétré en values ou injecté
// depuis la config). Utilisé par le bootstrap (POST /api/bootstrap) pour poser
// un umbrella chart + l'app-of-apps ArgoCD + l'app d'installation de l'opérateur
// CNPG dans un repo neuf. Les fichiers sont en anglais (artefact public).

// operatorChartVersion : version du chart Helm cloudnative-pg (opérateur ~1.30).
const operatorChartVersion = "0.29.0"

// scaffoldFiles renvoie path->contenu de tous les fichiers du starter, paramétrés
// depuis la config (owner/repo/branch/namespace ArgoCD/projet/app/suffixe DNS).
func (s *Server) scaffoldFiles() map[string]string {
	g := s.cfg.GitHub
	a := s.cfg.ArgoCD
	// Liste des value files (indentée 8 espaces) pour l'Application cnpg-clusters.
	var vf strings.Builder
	for i, f := range s.valueFilesList() {
		if i > 0 {
			vf.WriteString("\n")
		}
		vf.WriteString("        - " + f)
	}
	rep := strings.NewReplacer(
		"__OWNER__", g.Owner,
		"__REPO__", g.InfraRepo,
		"__BRANCH__", g.InfraBranch,
		"__ARGONS__", a.Namespace,
		"__PROJECT__", a.Project,
		"__CLUSTERSAPP__", a.ClustersApp,
		"__OPCHART__", operatorChartVersion,
		"__SUFFIX__", s.cfg.CNPG.HostnameSuffix,
		"__VALUEFILES__", vf.String(),
	)
	sub := func(t string) string { return rep.Replace(t) }

	files := map[string]string{
		"Chart.yaml":                        sub(tplChart),
		"README.md":                         sub(tplReadme),
		".gitignore":                        tplGitignore,
		"templates/_helpers.tpl":            tplHelpers,
		"templates/namespaces.yaml":         tplNamespaces,
		"templates/clusters.yaml":           tplClusters,
		"templates/delegation.yaml":         tplDelegation,
		"templates/edge-configmap.yaml":     tplEdgeConfigMap,
		"templates/edge-deployment.yaml":    tplEdgeDeployment,
		"templates/edge-service.yaml":       tplEdgeService,
		"argocd/root-app.yaml":              sub(tplRootApp),
		"argocd/apps/cnpg-operator.yaml":    sub(tplOperatorApp),
		"argocd/apps/cnpg-clusters.yaml":    sub(tplClustersApp),
	}

	// Base values + overlay des clusters. Le fichier édité par cnpg-console est
	// InfraValuesPath. Si c'est "values.yaml", tout tient dans un seul fichier.
	if g.InfraValuesPath == "values.yaml" {
		files["values.yaml"] = sub(tplBaseValues) + "\n" + tplClustersOverlay
	} else {
		files["values.yaml"] = sub(tplBaseValues)
		files[g.InfraValuesPath] = tplClustersOverlay
	}
	return files
}

// valueFilesList renvoie la liste des value files pour l'Application cnpg-clusters.
func (s *Server) valueFilesList() []string {
	if s.cfg.GitHub.InfraValuesPath == "values.yaml" {
		return []string{"values.yaml"}
	}
	return []string{"values.yaml", s.cfg.GitHub.InfraValuesPath}
}

const tplChart = `apiVersion: v2
name: helm-cnpg
description: >-
  Multi-tenant PostgreSQL clusters (CloudNativePG) plus a shared TCP proxy
  (pg-edge) exposing every cluster through a single LoadBalancer. The CNPG
  operator itself is installed by a separate ArgoCD Application
  (argocd/apps/cnpg-operator.yaml).
type: application
version: 0.1.0
appVersion: "1.30.0"
`

const tplBaseValues = `# helm-cnpg — platform chart scaffolded by cnpg-console.
#
# Multi-tenant CloudNativePG: one namespace + one Cluster per entry in the
# ` + "`clusters`" + ` list, optional per-cluster GitOps delegation (a team repo that
# holds extra Database/DatabaseRole objects), and a single shared LoadBalancer
# (pg-edge) that fans out to every cluster by port.
#
# Edit the values below for your environment and commit. cnpg-console appends
# and edits entries under ` + "`clusters:`" + ` (kept in the values file it is configured
# to read) through pull requests.

postgres:
  # StorageClass for cluster volumes. "" = the cluster's default StorageClass.
  # It MUST allow volume expansion for the scale-up feature to work.
  storageClass: ""
  # Default PostgreSQL image for clusters (overridable per cluster entry).
  imageName: ghcr.io/cloudnative-pg/postgresql:16.4
  # Optional placement for the Cluster pods (e.g. a dedicated node pool).
  nodeSelector: {}
  tolerations: []

argocd:
  # Namespace where the ArgoCD Application/AppProject CRs live.
  namespace: __ARGONS__
  destinationServer: https://kubernetes.default.svc

# Shared TCP proxy (pg-edge): one LoadBalancer, one port per cluster.
# TCP passthrough → TLS stays end-to-end (client <-> Postgres).
edge:
  enabled: true
  namespace: pg-edge
  replicas: 2
  image: haproxy:3-alpine
  # DNS suffix → one external-dns annotation per cluster (<name><dnsSuffix>)
  # pointing at this LoadBalancer. Empty = no annotation. Effective once
  # external-dns runs on the cluster.
  dnsSuffix: "__SUFFIX__"
  # LoadBalancer cloud annotations: "aws" = AWS NLB annotations, "none" = a
  # plain Service of type LoadBalancer (let your cloud/controller decide).
  cloud: aws
  resources:
    requests: { cpu: 25m, memory: 32Mi }
    limits: { memory: 64Mi }
  nodeSelector: {}
  tolerations: []
  nlb:
    internal: true       # true = internal LB (VPN model) ; false = public
    sourceRanges: []     # optional CIDR allowlist (public mode)
`

const tplClustersOverlay = `# Clusters managed by cnpg-console (one entry per cluster). cnpg-console edits
# this list through pull requests — keep it tidy and commented.
#
# Example entry:
#   - name: app-staging        # k8s name (DNS-safe), IMMUTABLE after creation
#     instances: 1             # 1 = single, 3 = HA
#     storage: 20Gi
#     port: 5432               # unique port on the shared LoadBalancer
#     database: appdb          # first application DB (bootstrap), underscore ident
#     owner: app               # owner role (password auto-generated in <name>-app)
#     team:                    # optional GitOps delegation (extra DBs/users repo)
#       repoURL: git@github.com:<owner>/pg-app-staging.git
#       repoRevision: master
#       repoPath: manifests
clusters: []
`

const tplHelpers = `{{/*
Namespace of a cluster: explicit ` + "`namespace`" + ` field, otherwise "pg-<name>".
Call with a cluster dict: {{ include "cnpg.ns" . }}
*/}}
{{- define "cnpg.ns" -}}
{{- .namespace | default (printf "pg-%s" .name) -}}
{{- end -}}

{{/*
Name of the delegated ArgoCD project/application for a cluster.
*/}}
{{- define "cnpg.project" -}}
{{- printf "pg-%s" .name -}}
{{- end -}}
`

const tplNamespaces = `# One namespace per cluster (multi-tenant isolation) + the edge proxy namespace.
# sync-wave -1: created before the objects that reference them.
{{- range .Values.clusters }}
apiVersion: v1
kind: Namespace
metadata:
  name: {{ include "cnpg.ns" . }}
  labels:
    app.kubernetes.io/managed-by: cnpg-console
    cnpg-console/cluster: {{ .name }}
  annotations:
    argocd.argoproj.io/sync-wave: "-1"
---
{{- end }}
{{- if .Values.edge.enabled }}
apiVersion: v1
kind: Namespace
metadata:
  name: {{ .Values.edge.namespace }}
  annotations:
    argocd.argoproj.io/sync-wave: "-1"
{{- end }}
`

const tplClusters = `{{- $sc := .Values.postgres.storageClass -}}
{{- $img := .Values.postgres.imageName -}}
{{- range .Values.clusters }}
{{- $ns := include "cnpg.ns" . -}}
---
# PostgreSQL Cluster managed by CloudNativePG.
# TLS: CNPG generates an internal CA + certs automatically. Auth: scram-sha-256.
# The first application database ` + "`{{ .database }}`" + ` (owner ` + "`{{ .owner }}`" + `) is created
# at bootstrap; its password lives in the Secret ` + "`{{ .name }}-app`" + `.
# Extra databases/users are managed via Database/DatabaseRole objects (delegation).
apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: {{ .name }}
  namespace: {{ $ns }}
  annotations:
    argocd.argoproj.io/sync-wave: "0"
spec:
  instances: {{ .instances | default 1 }}
  imageName: {{ .imageName | default $img }}
  primaryUpdateStrategy: unsupervised

  storage:
    {{- if or .storageClass $sc }}
    storageClass: {{ .storageClass | default $sc }}
    {{- end }}
    size: {{ .storage | default "10Gi" }}

  bootstrap:
    initdb:
      database: {{ .database }}
      owner: {{ .owner }}

  postgresql:
    parameters:
      password_encryption: scram-sha-256

  {{- if or $.Values.postgres.nodeSelector $.Values.postgres.tolerations }}
  affinity:
    {{- with $.Values.postgres.nodeSelector }}
    nodeSelector:
      {{- toYaml . | nindent 6 }}
    {{- end }}
    {{- with $.Values.postgres.tolerations }}
    tolerations:
      {{- toYaml . | nindent 6 }}
    {{- end }}
  {{- end }}

  # TODO backups: enable barmanObjectStore (S3 + IAM/credentials) when needed.
{{- end }}
`

const tplDelegation = `{{- range $c := .Values.clusters }}
{{- $ns := include "cnpg.ns" $c }}
{{- $project := include "cnpg.project" $c }}
{{- $team := $c.team | default dict }}
---
# ─── k8s RBAC barrier: what the team may do INSIDE its namespace ───
# Manage databases/users/replication + read-only on its own Cluster.
# No write access to the Cluster object, no access to the operator namespace.
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: pg-tenant-admin
  namespace: {{ $ns }}
rules:
  - apiGroups: ["postgresql.cnpg.io"]
    resources: ["databases", "databaseroles", "publications", "subscriptions"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: ["postgresql.cnpg.io"]
    resources: ["clusters", "poolers", "backups", "scheduledbackups"]
    verbs: ["get", "list", "watch"]
  - apiGroups: [""]
    resources: ["secrets"]
    verbs: ["get", "list", "watch", "create", "update", "patch", "delete"]
  - apiGroups: [""]
    resources: ["pods", "services", "events", "persistentvolumeclaims"]
    verbs: ["get", "list", "watch"]
{{ if $team.group }}
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: pg-tenant-admin
  namespace: {{ $ns }}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: pg-tenant-admin
subjects:
  - apiGroup: rbac.authorization.k8s.io
    kind: Group
    name: {{ $team.group }}
{{ end }}
{{ if $team.repoURL }}
---
# ─── ArgoCD barrier: project delegated to the team ───
# sourceRepos limited to the team repo, destination limited to its namespace,
# and only the DB/user/replication CRDs + Secret are allowed.
apiVersion: argoproj.io/v1alpha1
kind: AppProject
metadata:
  name: {{ $project }}
  namespace: {{ $.Values.argocd.namespace }}
spec:
  description: "Delegated database/user management for cluster {{ $c.name }}"
  sourceRepos:
    - {{ $team.repoURL | quote }}
  destinations:
    - namespace: {{ $ns }}
      server: {{ $.Values.argocd.destinationServer }}
  clusterResourceWhitelist: []
  namespaceResourceWhitelist:
    - { group: "postgresql.cnpg.io", kind: "Database" }
    - { group: "postgresql.cnpg.io", kind: "DatabaseRole" }
    - { group: "postgresql.cnpg.io", kind: "Publication" }
    - { group: "postgresql.cnpg.io", kind: "Subscription" }
    - { group: "", kind: "Secret" }
---
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: {{ $project }}-content
  namespace: {{ $.Values.argocd.namespace }}
spec:
  project: {{ $project }}
  source:
    repoURL: {{ $team.repoURL | quote }}
    path: {{ $team.repoPath | default "manifests" | quote }}
    targetRevision: {{ $team.repoRevision | default "master" | quote }}
  destination:
    server: {{ $.Values.argocd.destinationServer }}
    namespace: {{ $ns }}
  syncPolicy:
    syncOptions:
      - CreateNamespace=false
{{ end }}
{{- end }}
`

const tplEdgeConfigMap = `{{- if .Values.edge.enabled }}
# HAProxy config generated from the ` + "`clusters`" + ` list: 1 frontend + 1 backend per
# cluster. TCP passthrough → end-to-end TLS. ` + "`resolvers`" + ` + ` + "`init-addr none`" + `:
# HAProxy re-resolves each -rw Service DNS, so it follows the primary on failover.
apiVersion: v1
kind: ConfigMap
metadata:
  name: pg-edge-haproxy
  namespace: {{ .Values.edge.namespace }}
data:
  haproxy.cfg: |
    global
      log stdout format raw local0 info
      maxconn 4000

    defaults
      mode tcp
      log global
      option tcplog
      timeout connect 5s
      timeout client  1h
      timeout server  1h

    resolvers kube
      nameserver dns1 kube-dns.kube-system.svc.cluster.local:53
      hold valid 10s
      resolve_retries 3
      timeout resolve 1s
      timeout retry   1s

    frontend health
      bind *:8404
      mode http
      monitor-uri /healthz
{{- range .Values.clusters }}
{{- $ns := include "cnpg.ns" . }}

    frontend fe_{{ .name }}
      bind *:{{ .port }}
      default_backend be_{{ .name }}

    backend be_{{ .name }}
      server {{ .name }} {{ .name }}-rw.{{ $ns }}.svc.cluster.local:5432 check resolvers kube init-addr none
{{- end }}
{{- end }}
`

const tplEdgeDeployment = `{{- if .Values.edge.enabled }}
apiVersion: apps/v1
kind: Deployment
metadata:
  name: pg-edge-haproxy
  namespace: {{ .Values.edge.namespace }}
  labels:
    app: pg-edge-haproxy
spec:
  replicas: {{ .Values.edge.replicas }}
  selector:
    matchLabels:
      app: pg-edge-haproxy
  template:
    metadata:
      labels:
        app: pg-edge-haproxy
      annotations:
        checksum/config: {{ include (print $.Template.BasePath "/edge-configmap.yaml") . | sha256sum }}
    spec:
      {{- with .Values.edge.nodeSelector }}
      nodeSelector:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      {{- with .Values.edge.tolerations }}
      tolerations:
        {{- toYaml . | nindent 8 }}
      {{- end }}
      topologySpreadConstraints:
        - maxSkew: 1
          topologyKey: kubernetes.io/hostname
          whenUnsatisfiable: ScheduleAnyway
          labelSelector:
            matchLabels:
              app: pg-edge-haproxy
      containers:
        - name: haproxy
          image: {{ .Values.edge.image }}
          ports:
{{- range .Values.clusters }}
            - { name: pg-{{ .port }}, containerPort: {{ .port }} }
{{- end }}
            - { name: health, containerPort: 8404 }
          readinessProbe:
            httpGet: { path: /healthz, port: 8404 }
            initialDelaySeconds: 3
            periodSeconds: 10
          livenessProbe:
            httpGet: { path: /healthz, port: 8404 }
            initialDelaySeconds: 5
            periodSeconds: 15
          resources:
            {{- toYaml .Values.edge.resources | nindent 12 }}
          volumeMounts:
            - name: cfg
              mountPath: /usr/local/etc/haproxy
              readOnly: true
      volumes:
        - name: cfg
          configMap:
            name: pg-edge-haproxy
{{- end }}
`

const tplEdgeService = `{{- if .Values.edge.enabled }}
# The single shared LoadBalancer: one Service, one port per cluster.
apiVersion: v1
kind: Service
metadata:
  name: pg-edge-nlb
  namespace: {{ .Values.edge.namespace }}
  labels:
    app: pg-edge-haproxy
  annotations:
    {{- if eq .Values.edge.cloud "aws" }}
    service.beta.kubernetes.io/aws-load-balancer-type: "nlb"
    service.beta.kubernetes.io/aws-load-balancer-backend-protocol: "tcp"
    service.beta.kubernetes.io/aws-load-balancer-cross-zone-load-balancing-enabled: "true"
    {{- if .Values.edge.nlb.internal }}
    service.beta.kubernetes.io/aws-load-balancer-internal: "true"
    {{- end }}
    {{- end }}
    {{- if .Values.edge.dnsSuffix }}
    {{- $suffix := .Values.edge.dnsSuffix }}
    {{- $hosts := list }}
    {{- range .Values.clusters }}{{ $hosts = append $hosts (printf "%s%s" .name $suffix) }}{{- end }}
    {{- if $hosts }}
    external-dns.alpha.kubernetes.io/hostname: {{ join "," $hosts | quote }}
    {{- end }}
    {{- end }}
spec:
  type: LoadBalancer
  externalTrafficPolicy: Local
  selector:
    app: pg-edge-haproxy
  ports:
{{- range .Values.clusters }}
    - name: pg-{{ .name }}
      port: {{ .port }}
      targetPort: {{ .port }}
      protocol: TCP
{{- end }}
  {{- with .Values.edge.nlb.sourceRanges }}
  loadBalancerSourceRanges:
    {{- toYaml . | nindent 4 }}
  {{- end }}
{{- end }}
`

const tplRootApp = `# App-of-apps root: manages the CNPG Applications (operator + clusters/pg-edge).
# Bootstrap once, manually:
#   kubectl apply -f argocd/root-app.yaml
# After that, this repo (argocd/apps) is the source of truth.
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: cnpg-root
  namespace: __ARGONS__
spec:
  project: __PROJECT__
  source:
    repoURL: git@github.com:__OWNER__/__REPO__.git
    path: argocd/apps
    targetRevision: __BRANCH__
    directory:
      recurse: true
  destination:
    server: https://kubernetes.default.svc
    namespace: __ARGONS__
  # Manual sync on purpose: keep control over rollout order
  # (operator + CRDs first, then clusters + pg-edge).
  syncPolicy:
    syncOptions:
      - CreateNamespace=false
`

const tplOperatorApp = `# CloudNativePG operator via the official Helm chart (installs the CRDs too).
# Sync this FIRST — the Cluster objects depend on the CRDs.
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: cnpg-operator
  namespace: __ARGONS__
spec:
  project: __PROJECT__
  source:
    repoURL: https://cloudnative-pg.github.io/charts
    chart: cloudnative-pg
    targetRevision: __OPCHART__
    helm:
      valuesObject:
        crds:
          create: true
  destination:
    server: https://kubernetes.default.svc
    namespace: cnpg-system
  syncPolicy:
    syncOptions:
      - CreateNamespace=true
      - ServerSideApply=true
`

const tplClustersApp = `# PostgreSQL clusters + pg-edge proxy, rendered by the platform chart.
# Sync AFTER cnpg-operator (depends on the postgresql.cnpg.io CRDs).
apiVersion: argoproj.io/v1alpha1
kind: Application
metadata:
  name: __CLUSTERSAPP__
  namespace: __ARGONS__
spec:
  project: __PROJECT__
  source:
    repoURL: git@github.com:__OWNER__/__REPO__.git
    path: .
    targetRevision: __BRANCH__
    helm:
      valueFiles:
__VALUEFILES__
  destination:
    server: https://kubernetes.default.svc
    namespace: default
  syncPolicy:
    syncOptions:
      - CreateNamespace=false
      - ServerSideApply=true
`

const tplReadme = "# helm-cnpg\n\n" +
	"Platform chart to run multi-tenant **PostgreSQL** clusters with " +
	"[CloudNativePG](https://cloudnative-pg.io/), scaffolded by " +
	"[cnpg-console](https://github.com/Guss4241/cnpg-console).\n\n" +
	"## What's here\n\n" +
	"- `Chart.yaml`, `values.yaml` — the umbrella chart and its defaults.\n" +
	"- `templates/` — one namespace + one `Cluster` per entry in `clusters`, the\n" +
	"  optional per-cluster delegation (RBAC + ArgoCD AppProject/Application), and\n" +
	"  the shared `pg-edge` HAProxy LoadBalancer that fans out to every cluster.\n" +
	"- `argocd/root-app.yaml` — an app-of-apps; `argocd/apps/` holds the operator\n" +
	"  install and the clusters application.\n\n" +
	"## Bootstrap (once)\n\n" +
	"1. Review `values.yaml` (storageClass, node placement, edge/DNS, cloud).\n" +
	"2. Apply the root app: `kubectl apply -f argocd/root-app.yaml`.\n" +
	"3. ArgoCD syncs the operator first, then the clusters.\n\n" +
	"After that, **cnpg-console** manages the `clusters:` list through pull requests.\n"

const tplGitignore = "*.local\n.DS_Store\n.idea/\n"
