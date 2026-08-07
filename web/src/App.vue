<script setup>
import { ref, reactive, computed, onMounted } from 'vue'
import { api } from './api.js'

const actor = ref('')
const booting = ref(true)
const loginForm = reactive({ username: 'admin', password: '' })
const loginErr = ref('')

const state = reactive({ clusters: [], nextPort: null, hostnameSuffix: '', infraRepo: '', infraReady: true })
const loadErr = ref('')

// Bootstrap du repo d'infra (quand infraReady === false).
const bootstrapBusy = ref(false)
const bootstrapErr = ref('')
const bootstrapResult = ref(null)

const form = reactive({ name: '', instances: 1, storage: '20Gi', database: '', owner: '', delegate: true })
const busy = ref(false)
const formErr = ref('')
const prepared = ref(null)
const finalized = ref(null)
const finalizeErr = ref('')

// --- vue détail d'un cluster ---
const selected = ref('')           // nom du cluster ouvert ('' = liste)
const detail = ref(null)           // réponse GET /api/clusters/{name}
const detailErr = ref('')
const detailBusy = ref(false)

const scaleForm = reactive({ storage: '' })
const dbForm = reactive({ database: '', owner: '' })
const roleForm = reactive({ name: '', login: true })

// Une seule opération (scale/add-db/add-role) en cours à la fois.
const opPrepared = ref(null)
const opFinalized = ref(null)
const opErr = ref('')
const opBusy = ref(false)

// Création assistée du Secret de mot de passe (accès kubectl local, optionnel).
const kube = reactive({ available: false, contexts: [], current: '', chosen: '', busy: false, done: null, err: '' })
function resetKube() { Object.assign(kube, { available: false, contexts: [], current: '', chosen: '', busy: false, done: null, err: '' }) }
async function loadKubeContexts() {
  resetKube()
  try {
    const r = await api.kubeContexts()
    kube.available = !!r.available
    kube.contexts = r.contexts || []
    kube.current = r.current || ''
    kube.chosen = r.current || (kube.contexts[0] || '')
  } catch (e) { /* silencieux : on retombe sur la commande manuelle */ }
}
async function doCreateSecret() {
  kube.err = ''; kube.busy = true
  try {
    kube.done = await api.createSecret({
      context: kube.chosen,
      namespace: 'pg-' + selected.value,
      name: opPrepared.value.secret.name,
      username: opPrepared.value.secret.username,
      password: opPrepared.value.secret.password,
    })
  } catch (e) { kube.err = e.message }
  finally { kube.busy = false }
}

async function boot() {
  try { const me = await api.me(); actor.value = me.actor; await loadClusters() } catch { /* non connecté */ }
  booting.value = false
}
async function doLogin() {
  loginErr.value = ''
  try { const d = await api.login(loginForm.username, loginForm.password); actor.value = d.actor; await loadClusters() }
  catch (e) { loginErr.value = e.message }
}
async function doLogout() { await api.logout(); actor.value = ''; prepared.value = null; finalized.value = null; closeDetail() }

const listBusy = ref(false)
async function loadClusters() {
  loadErr.value = ''
  try {
    const d = await api.clusters()
    state.clusters = d.clusters || []
    state.nextPort = d.nextPort
    state.hostnameSuffix = d.hostnameSuffix
    state.infraRepo = d.infraRepo
    state.infraReady = d.infraReady !== false
  } catch (e) { loadErr.value = e.message }
}
async function refreshList() { listBusy.value = true; try { await loadClusters() } finally { listBusy.value = false } }

async function doBootstrap() {
  if (!confirm(`Initialiser le repo d'infra « ${state.infraRepo} » ?\n\nCela CRÉE le repo et y pose l'umbrella chart helm-cnpg + l'app-of-apps ArgoCD. Refusé si le repo existe déjà.`)) return
  bootstrapErr.value = ''; bootstrapResult.value = null; bootstrapBusy.value = true
  try { bootstrapResult.value = await api.bootstrap(); await loadClusters() }
  catch (e) { bootstrapErr.value = e.message }
  finally { bootstrapBusy.value = false }
}

// Aide : dérive des noms PG en underscore depuis le nom de cluster.
function suggestFromName() {
  const base = form.name.replace(/-/g, '_')
  if (!form.database) form.database = base
  if (!form.owner) form.owner = base + '_app'
}

async function doPrepare() {
  formErr.value = ''; prepared.value = null; finalized.value = null; busy.value = true
  try {
    prepared.value = await api.prepare({
      name: form.name, instances: Number(form.instances), storage: form.storage,
      database: form.database, owner: form.owner, delegate: form.delegate,
    })
  } catch (e) { formErr.value = e.message }
  finally { busy.value = false }
}

async function doFinalize() {
  finalizeErr.value = ''; finalized.value = null; busy.value = true
  try { finalized.value = await api.finalize(prepared.value.finalizeToken); await loadClusters() }
  catch (e) { finalizeErr.value = e.message }
  finally { busy.value = false }
}

// ---- détail ----
function resetOp() { opPrepared.value = null; opFinalized.value = null; opErr.value = '' }

async function openDetail(name) {
  selected.value = name
  detail.value = null; detailErr.value = ''; resetOp()
  dbForm.database = ''; dbForm.owner = ''; roleForm.name = ''; roleForm.login = true
  detailBusy.value = true
  try {
    detail.value = await api.cluster(name)
    scaleForm.storage = detail.value.cluster.storage
  } catch (e) { detailErr.value = e.message }
  finally { detailBusy.value = false }
}
function closeDetail() { selected.value = ''; detail.value = null; resetOp() }

// Parse une quantité k8s (Mi/Gi/Ti) en octets pour bloquer le scale-down côté UI.
function bytesOf(q) {
  const m = /^([1-9][0-9]*)(Mi|Gi|Ti)$/.exec((q || '').trim())
  if (!m) return null
  const unit = { Mi: 2 ** 20, Gi: 2 ** 30, Ti: 2 ** 40 }[m[2]]
  return Number(m[1]) * unit
}
const scaleInvalid = computed(() => {
  const cur = detail.value && bytesOf(detail.value.cluster.storage)
  const next = bytesOf(scaleForm.storage)
  if (next === null) return 'Quantité invalide (ex. 80Gi).'
  if (cur !== null && next <= cur) return `Scale-down interdit : doit être > ${detail.value.cluster.storage}.`
  return ''
})

async function runOp(fn) {
  opErr.value = ''; opPrepared.value = null; opFinalized.value = null; opBusy.value = true; resetKube()
  try {
    opPrepared.value = await fn()
    if (opPrepared.value?.secret) await loadKubeContexts()
  }
  catch (e) { opErr.value = e.message }
  finally { opBusy.value = false }
}
function doScale() {
  if (scaleInvalid.value) { opErr.value = scaleInvalid.value; return }
  runOp(() => api.scale(selected.value, scaleForm.storage))
}
function doAddDb() {
  runOp(() => api.addDatabase(selected.value, { database: dbForm.database.trim(), owner: dbForm.owner.trim() }))
}
function doAddRole() {
  runOp(() => api.addRole(selected.value, { name: roleForm.name.trim(), login: roleForm.login }))
}
// Owners de bases (pour bloquer la suppression d'un rôle propriétaire côté UI).
const dbOwners = computed(() => new Set((detail.value?.databases || []).map(d => d.owner).filter(Boolean)))
function doDeleteDb(dbName) {
  if (!confirm(`Supprimer la base « ${dbName} » du cluster ${selected.value} ?\n\n⚠️ IRRÉVERSIBLE : après merge de la PR + finalize, CNPG droppe RÉELLEMENT la base PostgreSQL (reclaimPolicy=delete). Une PR sera ouverte.`)) return
  runOp(() => api.deleteDatabase(selected.value, dbName))
}
function doDeleteRole(roleName) {
  if (!confirm(`Supprimer le rôle « ${roleName} » du cluster ${selected.value} ?\n\n⚠️ IRRÉVERSIBLE : après merge de la PR + finalize, CNPG droppe RÉELLEMENT le rôle PostgreSQL (reclaimPolicy=delete). Une PR sera ouverte.`)) return
  runOp(() => api.deleteRole(selected.value, roleName))
}
async function doFinalizeOp() {
  opErr.value = ''; opBusy.value = true
  try {
    opFinalized.value = await api.finalize(opPrepared.value.finalizeToken)
    await refreshDetail()
    await loadClusters()
  } catch (e) { opErr.value = e.message }
  finally { opBusy.value = false }
}
// Recharge le détail (bases/rôles/storage) sans effacer le résultat d'opération.
const detailRefreshing = ref(false)
async function refreshDetail() {
  detailRefreshing.value = true; detailErr.value = ''
  try {
    detail.value = await api.cluster(selected.value)
    scaleForm.storage = detail.value.cluster.storage
  } catch (e) { detailErr.value = e.message }
  finally { detailRefreshing.value = false }
}

const opLabel = computed(() => ({ scale: 'Scale-up du volume', 'add-db': 'Ajout de base', 'add-role': 'Ajout de rôle', 'del-db': 'Suppression de base', 'del-role': 'Suppression de rôle' }[opPrepared.value?.action] || 'Opération'))

onMounted(boot)
</script>

<template>
  <div class="wrap">
    <div v-if="booting" class="muted">Chargement…</div>

    <!-- Login -->
    <div v-else-if="!actor" class="panel" style="max-width:360px;margin:80px auto;">
      <h1>🐘 cnpg-console</h1>
      <label>Identifiant</label>
      <input v-model="loginForm.username" @keyup.enter="doLogin" />
      <label>Mot de passe</label>
      <input type="password" v-model="loginForm.password" @keyup.enter="doLogin" />
      <button @click="doLogin">Se connecter</button>
      <div v-if="loginErr" class="err">{{ loginErr }}</div>
    </div>

    <!-- App -->
    <div v-else>
      <div class="topbar">
        <h1>🐘 cnpg-console</h1>
        <div><span class="pill">{{ actor }}</span> <button class="secondary" style="margin:0 0 0 8px;padding:5px 10px" @click="doLogout">Déconnexion</button></div>
      </div>

      <!-- ============ VUE LISTE + CRÉATION ============ -->
      <template v-if="!selected">

        <!-- Repo d'infra non initialisé → bouton d'init -->
        <template v-if="state.infraReady === false">
          <h2>Repo d'infra</h2>
          <div class="panel">
            <p>Le repo <code>{{ state.infraRepo }}</code> n'est pas initialisé.</p>
            <p class="muted">L'initialisation crée le repo puis y pose un <b>umbrella chart helm-cnpg générique</b> (namespaces, Cluster CNPG, délégation, proxy edge) + l'<b>app-of-apps ArgoCD</b> (opérateur + clusters). Les futures PR de création de cluster iront sur ce repo.</p>
            <button :disabled="bootstrapBusy" @click="doBootstrap">{{ bootstrapBusy ? 'Initialisation…' : 'Initialiser le repo d\'infra' }}</button>
            <div v-if="bootstrapErr" class="err">{{ bootstrapErr }}</div>
          </div>
          <div v-if="bootstrapResult" class="panel">
            <h2 style="margin-top:0" class="ok">Repo initialisé ✔</h2>
            <p>Repo : <a :href="bootstrapResult.repoUrl" target="_blank">{{ bootstrapResult.repo }}</a> · branche <code>{{ bootstrapResult.branch }}</code> · {{ bootstrapResult.files.length }} fichiers</p>
            <h2>Étapes</h2>
            <ul><li v-for="(s,i) in bootstrapResult.nextSteps" :key="i">{{ s }}</li></ul>
          </div>
        </template>

        <!-- Repo prêt → liste + création -->
        <template v-else>
        <h2>Clusters existants — repo <code>{{ state.infraRepo }}</code></h2>
        <div class="panel">
          <div style="display:flex;justify-content:flex-end">
            <button class="secondary" style="margin:0;padding:4px 10px;font-size:12px" :disabled="listBusy" @click="refreshList">↻ {{ listBusy ? 'Rafraîchissement…' : 'Rafraîchir' }}</button>
          </div>
          <div v-if="loadErr" class="err">{{ loadErr }}</div>
          <table v-if="state.clusters.length">
            <thead><tr><th>Nom</th><th>Port</th><th>Base</th><th>Owner</th><th>Inst.</th><th>Storage</th><th>Délég.</th><th></th></tr></thead>
            <tbody>
              <tr v-for="c in state.clusters" :key="c.name" class="clickable" @click="openDetail(c.name)">
                <td><code>{{ c.name }}</code></td><td>{{ c.port }}</td><td>{{ c.database }}</td>
                <td>{{ c.owner }}</td><td>{{ c.instances }}</td><td>{{ c.storage }}</td>
                <td>{{ c.team ? '✓' : '—' }}</td>
                <td class="muted" style="text-align:right">détails →</td>
              </tr>
            </tbody>
          </table>
          <div v-else class="muted">Aucun cluster (ou GitHub non configuré).</div>
        </div>

        <h2>Nouveau cluster</h2>
        <div class="panel">
          <div class="row">
            <div>
              <label>Nom du cluster (k8s, immuable — tirets ok)</label>
              <input v-model="form.name" @blur="suggestFromName" placeholder="ex. analytics-pprod" />
            </div>
            <div>
              <label>Port NLB (auto)</label>
              <input :value="state.nextPort" disabled />
            </div>
          </div>
          <div class="row">
            <div>
              <label>Base (immuable — underscore)</label>
              <input v-model="form.database" placeholder="ex. analytics" />
            </div>
            <div>
              <label>Owner (rôle — underscore)</label>
              <input v-model="form.owner" placeholder="ex. analytics_app" />
            </div>
          </div>
          <div class="row">
            <div>
              <label>Instances</label>
              <select v-model="form.instances"><option :value="1">1 (mono)</option><option :value="3">3 (HA)</option></select>
            </div>
            <div>
              <label>Storage</label>
              <input v-model="form.storage" placeholder="ex. 20Gi" />
            </div>
          </div>
          <div class="check">
            <input type="checkbox" id="deleg" v-model="form.delegate" />
            <label for="deleg" style="margin:0">Délégation GitOps (crée le repo d'équipe <code>pg-&lt;nom&gt;</code>)</label>
          </div>
          <div class="muted" style="margin-top:8px">Hostname : <code>{{ form.name || '<nom>' }}{{ state.hostnameSuffix }}</code></div>
          <button :disabled="busy || !form.name" @click="doPrepare">Préparer (ouvrir la PR)</button>
          <div v-if="formErr" class="err">{{ formErr }}</div>
        </div>

        <!-- Résultat prepare (création) -->
        <div v-if="prepared" class="panel">
          <h2 style="margin-top:0">Préparé ✔</h2>
          <p>PR infra : <a :href="prepared.infraPrUrl" target="_blank">{{ prepared.infraPrUrl }}</a></p>
          <p v-if="prepared.teamRepoUrl">Repo d'équipe : <a :href="prepared.teamRepoUrl" target="_blank">{{ prepared.teamRepoUrl }}</a></p>
          <p class="muted">Hostname : <code>{{ prepared.hostname }}</code> · port {{ prepared.cluster.port }}</p>
          <h2>Étapes</h2>
          <ul><li v-for="(s,i) in prepared.nextSteps" :key="i">{{ s }}</li></ul>
          <button :disabled="busy" @click="doFinalize">Finaliser (sync ArgoCD)</button>
          <div v-if="finalizeErr" class="err">{{ finalizeErr }}</div>
        </div>

        <!-- Résultat finalize (création) -->
        <div v-if="finalized" class="panel">
          <h2 style="margin-top:0" class="ok">Finalisé ✔ — {{ finalized.cluster }}</h2>
          <p v-for="(a,i) in finalized.apps" :key="i">App <code>{{ a.name }}</code> : sync={{ a.sync || '—' }} health={{ a.health || '—' }}</p>
          <ul><li v-for="(n,i) in finalized.notes" :key="i">{{ n }}</li></ul>
        </div>
        </template>
      </template>

      <!-- ============ VUE DÉTAIL D'UN CLUSTER ============ -->
      <template v-else>
        <div class="topbar" style="margin-top:8px">
          <h2 style="margin:0">Cluster <code>{{ selected }}</code></h2>
          <button class="secondary" style="margin:0;padding:5px 10px" @click="closeDetail">← Retour</button>
        </div>

        <div v-if="detailErr" class="panel"><div class="err">{{ detailErr }}</div></div>
        <div v-else-if="detailBusy" class="panel muted">Chargement du détail…</div>

        <template v-else-if="detail">
          <!-- Infos + scale-up -->
          <div class="panel">
            <div class="row">
              <div class="muted">Namespace : <code>{{ detail.namespace }}</code></div>
              <div class="muted">Hostname : <code>{{ detail.hostname }}</code></div>
            </div>
            <div class="row">
              <div class="muted">Port NLB : <code>{{ detail.cluster.port }}</code></div>
              <div class="muted">Instances : <code>{{ detail.cluster.instances }}</code></div>
            </div>
            <div class="muted" style="margin-top:8px">
              Délégation :
              <span v-if="detail.delegated" class="ok">✓ active</span>
              <span v-else>— non déléguée (l'ajout de base/rôle la mettra en place)</span>
              <template v-if="detail.teamRepoUrl"> · <a :href="detail.teamRepoUrl" target="_blank">{{ detail.teamRepoName }}</a></template>
            </div>
            <div v-if="detail.manifestsError" class="muted" style="margin-top:6px;color:var(--warn)">⚠ {{ detail.manifestsError }}</div>

            <h2 style="margin-top:18px">Volume (scale-up)</h2>
            <div class="row">
              <div>
                <label>Storage actuel</label>
                <input :value="detail.cluster.storage" disabled />
              </div>
              <div>
                <label>Nouveau storage (croissance uniquement)</label>
                <input v-model="scaleForm.storage" placeholder="ex. 80Gi" />
              </div>
            </div>
            <div v-if="scaleInvalid" class="muted" style="margin-top:6px;color:var(--warn)">⚠ {{ scaleInvalid }}</div>
            <button :disabled="opBusy || !!scaleInvalid" @click="doScale">Scaler le volume (ouvrir la PR)</button>
          </div>

          <!-- Bases -->
          <h2>Bases de données</h2>
          <div class="panel">
            <div style="display:flex;justify-content:flex-end">
              <button class="secondary" style="margin:0;padding:4px 10px;font-size:12px" :disabled="detailRefreshing" @click="refreshDetail">↻ {{ detailRefreshing ? 'Rafraîchissement…' : 'Rafraîchir' }}</button>
            </div>
            <table>
              <thead><tr><th>Base</th><th>Owner</th><th>Origine</th><th>Fichier</th><th></th></tr></thead>
              <tbody>
                <tr v-for="(d,i) in detail.databases" :key="i">
                  <td><code>{{ d.name }}</code></td><td>{{ d.owner || '—' }}</td>
                  <td>{{ d.source === 'bootstrap' ? 'bootstrap (plateforme)' : 'manifest (équipe)' }}</td>
                  <td class="muted">{{ d.file || '—' }}</td>
                  <td style="text-align:right">
                    <button v-if="d.source === 'manifest'" class="secondary danger" style="margin:0;padding:2px 8px;font-size:12px" :disabled="opBusy" @click="doDeleteDb(d.name)">Supprimer</button>
                    <span v-else class="muted" title="objet bootstrap (plateforme) — non supprimable">🔒</span>
                  </td>
                </tr>
              </tbody>
            </table>
            <h2 style="margin-top:16px">Ajouter une base</h2>
            <div class="row">
              <div>
                <label>Nom de la base (underscore)</label>
                <input v-model="dbForm.database" placeholder="ex. reporting" />
              </div>
              <div>
                <label>Owner (rôle ; défaut = <code>{{ detail.cluster.owner }}</code>)</label>
                <input v-model="dbForm.owner" placeholder="(optionnel)" />
              </div>
            </div>
            <button :disabled="opBusy || !dbForm.database" @click="doAddDb">Ajouter la base (ouvrir la PR)</button>
          </div>

          <!-- Rôles / users -->
          <h2>Rôles (users)</h2>
          <div class="panel">
            <div style="display:flex;justify-content:flex-end">
              <button class="secondary" style="margin:0;padding:4px 10px;font-size:12px" :disabled="detailRefreshing" @click="refreshDetail">↻ {{ detailRefreshing ? 'Rafraîchissement…' : 'Rafraîchir' }}</button>
            </div>
            <table>
              <thead><tr><th>Rôle</th><th>Login</th><th>Origine</th><th>Fichier</th><th></th></tr></thead>
              <tbody>
                <tr v-for="(r,i) in detail.roles" :key="i">
                  <td><code>{{ r.name }}</code></td><td>{{ r.login ? '✓' : '—' }}</td>
                  <td>{{ r.source === 'bootstrap' ? 'bootstrap (plateforme)' : 'manifest (équipe)' }}</td>
                  <td class="muted">{{ r.file || '—' }}</td>
                  <td style="text-align:right">
                    <button v-if="r.source === 'manifest' && !dbOwners.has(r.name)" class="secondary danger" style="margin:0;padding:2px 8px;font-size:12px" :disabled="opBusy" @click="doDeleteRole(r.name)">Supprimer</button>
                    <span v-else-if="r.source === 'manifest'" class="muted" title="propriétaire d'une base — réassigner l'owner avant suppression">🔒 owner</span>
                    <span v-else class="muted" title="objet bootstrap (plateforme) — non supprimable">🔒</span>
                  </td>
                </tr>
              </tbody>
            </table>
            <h2 style="margin-top:16px">Ajouter un user</h2>
            <div class="row">
              <div>
                <label>Nom du rôle (underscore)</label>
                <input v-model="roleForm.name" placeholder="ex. reporting_ro" />
              </div>
              <div class="check" style="align-items:flex-end">
                <input type="checkbox" id="rlogin" v-model="roleForm.login" />
                <label for="rlogin" style="margin:0">login (génère un mot de passe)</label>
              </div>
            </div>
            <button :disabled="opBusy || !roleForm.name" @click="doAddRole">Ajouter le user (ouvrir la PR)</button>
          </div>

          <div v-if="opErr" class="panel"><div class="err">{{ opErr }}</div></div>

          <!-- Résultat d'une opération (prepare) -->
          <div v-if="opPrepared" class="panel">
            <h2 style="margin-top:0">{{ opLabel }} — préparé ✔</h2>
            <p v-if="opPrepared.promoted" class="muted">↑ Le cluster a été <b>promu en délégation</b> (repo d'équipe créé + bloc <code>team:</code> ajouté).</p>
            <p v-for="(pr,i) in opPrepared.prs" :key="i">PR <code>{{ pr.repo }}</code> : <a :href="pr.url" target="_blank">{{ pr.url }}</a></p>

            <!-- Secret affiché UNE seule fois -->
            <div v-if="opPrepared.secret" class="panel" style="border-color:var(--warn);background:#1c1a12">
              <p class="ok" style="margin-top:0">🔑 Mot de passe généré — affiché une seule fois, non committé</p>
              <p class="muted">User <code>{{ opPrepared.secret.username }}</code> · Secret <code>{{ opPrepared.secret.name }}</code> · namespace <code>pg-{{ selected }}</code></p>
              <p>Mot de passe : <code style="user-select:all">{{ opPrepared.secret.password }}</code></p>

              <!-- Résultat de la création assistée -->
              <p v-if="kube.done && kube.done.created" class="ok">✔ {{ kube.done.message }}</p>
              <p v-else-if="kube.done && kube.done.existed" class="muted">ℹ️ {{ kube.done.message }}</p>

              <!-- Création assistée : choix du contexte kubectl + bouton -->
              <template v-else-if="kube.available">
                <p class="muted">Créer le Secret pour toi (hors-git), sur le contexte kubectl :</p>
                <div style="display:flex;gap:8px;align-items:center;flex-wrap:wrap">
                  <select v-model="kube.chosen" :disabled="kube.busy">
                    <option v-for="c in kube.contexts" :key="c" :value="c">{{ c }}{{ c === kube.current ? '  (courant)' : '' }}</option>
                  </select>
                  <button :disabled="kube.busy || !kube.chosen" @click="doCreateSecret">Créer le Secret</button>
                </div>
                <div v-if="kube.err" class="err" style="margin-top:6px">{{ kube.err }}</div>
              </template>

              <!-- Repli : kubectl indisponible → commande manuelle -->
              <p v-else class="muted">kubectl indisponible ici — créer le Secret AVANT de finaliser (hors-git) :</p>

              <details :open="!kube.available && !kube.done" style="margin-top:8px">
                <summary class="muted" style="cursor:pointer">Commande kubectl manuelle</summary>
                <pre style="white-space:pre-wrap;background:#0d0f14;padding:10px;border-radius:6px;font-size:12px;margin-top:6px">{{ opPrepared.secret.kubectl }}</pre>
              </details>
            </div>

            <h2>Étapes</h2>
            <ul><li v-for="(s,i) in opPrepared.nextSteps" :key="i">{{ s }}</li></ul>
            <button :disabled="opBusy" @click="doFinalizeOp">Finaliser (sync ArgoCD)</button>
          </div>

          <!-- Résultat finalize d'une opération -->
          <div v-if="opFinalized" class="panel">
            <h2 style="margin-top:0" class="ok">{{ opLabel }} — finalisé ✔</h2>
            <p v-for="(a,i) in opFinalized.apps" :key="i">App <code>{{ a.name }}</code> : sync={{ a.sync || '—' }} health={{ a.health || '—' }}<span v-if="a.message" class="muted"> — {{ a.message }}</span></p>
            <ul><li v-for="(n,i) in opFinalized.notes" :key="i">{{ n }}</li></ul>
          </div>
        </template>
      </template>
    </div>
  </div>
</template>
