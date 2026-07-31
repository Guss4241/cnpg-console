<script setup>
import { ref, reactive, onMounted } from 'vue'
import { api } from './api.js'

const actor = ref('')
const booting = ref(true)
const loginForm = reactive({ username: 'admin', password: '' })
const loginErr = ref('')

const state = reactive({ clusters: [], nextPort: null, hostnameSuffix: '', infraRepo: '' })
const loadErr = ref('')

const form = reactive({ name: '', instances: 1, storage: '20Gi', database: '', owner: '', delegate: true })
const busy = ref(false)
const formErr = ref('')
const prepared = ref(null)
const finalized = ref(null)
const finalizeErr = ref('')

async function boot() {
  try { const me = await api.me(); actor.value = me.actor; await loadClusters() } catch { /* non connecté */ }
  booting.value = false
}
async function doLogin() {
  loginErr.value = ''
  try { const d = await api.login(loginForm.username, loginForm.password); actor.value = d.actor; await loadClusters() }
  catch (e) { loginErr.value = e.message }
}
async function doLogout() { await api.logout(); actor.value = ''; prepared.value = null; finalized.value = null }

async function loadClusters() {
  loadErr.value = ''
  try {
    const d = await api.clusters()
    state.clusters = d.clusters || []
    state.nextPort = d.nextPort
    state.hostnameSuffix = d.hostnameSuffix
    state.infraRepo = d.infraRepo
  } catch (e) { loadErr.value = e.message }
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

      <h2>Clusters existants — repo <code>{{ state.infraRepo }}</code></h2>
      <div class="panel">
        <div v-if="loadErr" class="err">{{ loadErr }}</div>
        <table v-if="state.clusters.length">
          <thead><tr><th>Nom</th><th>Port</th><th>Base</th><th>Owner</th><th>Inst.</th><th>Storage</th><th>Délég.</th></tr></thead>
          <tbody>
            <tr v-for="c in state.clusters" :key="c.name">
              <td><code>{{ c.name }}</code></td><td>{{ c.port }}</td><td>{{ c.database }}</td>
              <td>{{ c.owner }}</td><td>{{ c.instances }}</td><td>{{ c.storage }}</td>
              <td>{{ c.team ? '✓' : '—' }}</td>
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

      <!-- Résultat prepare -->
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

      <!-- Résultat finalize -->
      <div v-if="finalized" class="panel">
        <h2 style="margin-top:0" class="ok">Finalisé ✔ — {{ finalized.cluster }}</h2>
        <p>App <code>{{ finalized.clustersApp.name }}</code> : sync={{ finalized.clustersApp.sync }} health={{ finalized.clustersApp.health }}</p>
        <p v-if="finalized.delegatedApp">App déléguée <code>{{ finalized.delegatedApp.name }}</code> : sync={{ finalized.delegatedApp.sync }} health={{ finalized.delegatedApp.health }}</p>
        <ul><li v-for="(n,i) in finalized.notes" :key="i">{{ n }}</li></ul>
      </div>
    </div>
  </div>
</template>
