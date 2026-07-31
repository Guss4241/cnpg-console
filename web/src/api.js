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
  async prepare(payload) { return req('POST', '/api/prepare', payload) },
  async finalize(token) { return req('POST', '/api/finalize', { token }) },
}
