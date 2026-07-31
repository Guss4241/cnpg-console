// Petit client fetch : cookie de session (credentials) + token CSRF sur les
// mutations (double-submit via en-tête X-CSRF-Token).
let csrfToken = ''

async function req(method, path, body) {
  const headers = {}
  if (body !== undefined) headers['Content-Type'] = 'application/json'
  if (method !== 'GET' && csrfToken) headers['X-CSRF-Token'] = csrfToken
  const res = await fetch(path, {
    method,
    headers,
    credentials: 'include',
    body: body !== undefined ? JSON.stringify(body) : undefined,
  })
  const text = await res.text()
  const data = text ? JSON.parse(text) : {}
  if (!res.ok) throw new Error(data.error || `HTTP ${res.status}`)
  return data
}

export const api = {
  get csrf() { return csrfToken },
  async authConfig() { return req('GET', '/api/auth/config') },
  async me() {
    const d = await req('GET', '/api/auth/me')
    csrfToken = d.csrfToken || ''
    return d
  },
  async login(username, password) {
    const d = await req('POST', '/api/auth/login', { username, password })
    csrfToken = d.csrfToken || ''
    return d
  },
  async logout() { await req('POST', '/api/auth/logout'); csrfToken = '' },
  async clusters() { return req('GET', '/api/clusters') },
  async cluster(name) { return req('GET', '/api/clusters/' + encodeURIComponent(name)) },
  async prepare(payload) { return req('POST', '/api/prepare', payload) },
  async scale(name, storage) { return req('POST', '/api/clusters/' + encodeURIComponent(name) + '/scale', { storage }) },
  async addDatabase(name, payload) { return req('POST', '/api/clusters/' + encodeURIComponent(name) + '/databases', payload) },
  async addRole(name, payload) { return req('POST', '/api/clusters/' + encodeURIComponent(name) + '/roles', payload) },
  async deleteDatabase(name, db) { return req('DELETE', '/api/clusters/' + encodeURIComponent(name) + '/databases/' + encodeURIComponent(db)) },
  async deleteRole(name, role) { return req('DELETE', '/api/clusters/' + encodeURIComponent(name) + '/roles/' + encodeURIComponent(role)) },
  async finalize(token) { return req('POST', '/api/finalize', { token }) },
}
