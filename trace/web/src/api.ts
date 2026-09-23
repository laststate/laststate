const TOKEN_KEY = 'trace_session'
const PROJECT_KEY = 'trace_project_id'

export class ApiError extends Error {
  status: number
  body: string
  code?: string
  constructor(status: number, body: string) {
    super(apiErrorMessage(status, body))
    this.status = status
    this.body = body
    try {
      const j = JSON.parse(body)
      this.code = j?.error?.code
    } catch { /* */ }
  }
}

function apiErrorMessage(status: number, body: string): string {
  try {
    const j = JSON.parse(body)
    if (j?.error?.message) return `${status}: ${j.error.message}`
  } catch { /* */ }
  if (body.length < 200) return `${status}: ${body}`
  return `HTTP ${status}`
}

export function token() {
  // Prefer in-memory / local fallback only for API tooling; browser sessions use HttpOnly cookie.
  return localStorage.getItem(TOKEN_KEY) || ''
}
export function setToken(t: string) {
  // Keep optional localStorage for non-browser clients and OIDC legacy; prefer cookie from server.
  if (t) localStorage.setItem(TOKEN_KEY, t)
  else localStorage.removeItem(TOKEN_KEY)
}

export function projectId() {
  return localStorage.getItem(PROJECT_KEY) || ''
}
export function setProjectId(id: string) {
  if (id) localStorage.setItem(PROJECT_KEY, id)
  else localStorage.removeItem(PROJECT_KEY)
}

export type ApiOpts = Omit<RequestInit, 'body'> & { body?: any; timeoutMs?: number }

export async function api(path: string, opts: ApiOpts = {}) {
  const headers: Record<string, string> = { ...(opts.headers as any) }
  if (token()) headers.Authorization = 'Bearer ' + token()
  if (projectId()) headers['X-Project-ID'] = projectId()

  let body: BodyInit | undefined
  if (opts.body != null) {
    if (opts.body instanceof Uint8Array || typeof opts.body === 'string' || opts.body instanceof ArrayBuffer || opts.body instanceof FormData || opts.body instanceof Blob) {
      body = opts.body as BodyInit
    } else {
      headers['Content-Type'] = 'application/json'
      body = JSON.stringify(opts.body)
    }
  }

  const timeoutMs = opts.timeoutMs ?? 30000
  const ctrl = new AbortController()
  const t = setTimeout(() => ctrl.abort(), timeoutMs)
  const { body: _b, timeoutMs: _t, ...rest } = opts
  try {
    const r = await fetch(path, {
      ...rest,
      headers,
      body,
      signal: opts.signal || ctrl.signal,
      credentials: 'same-origin', // send HttpOnly session cookie
    })
    if (!r.ok) {
      const text = await r.text()
      if (r.status === 401) {
        // session expired - clear token for non-login routes
        if (!path.includes('/api/auth/login')) setToken('')
        // Bounce authenticated app views to login. Public/landing/auth
        // pages handle 401 themselves (forms show errors, not redirects).
        if (typeof window !== 'undefined' && !path.startsWith('/api/auth/')) {
          const p = window.location.pathname
          if (!/^\/(login|register|forgot-password|reset-password|verify-email|accept-invite|landing|blog|faq|docs|pricing)/.test(p)) {
            window.location.assign('/login')
          }
        }
      }
      throw new ApiError(r.status, text)
    }
    const ct = r.headers.get('content-type') || ''
    if (ct.includes('application/json')) return r.json()
    return r
  } catch (e: any) {
    if (e?.name === 'AbortError') throw new Error('Request timed out')
    throw e
  } finally {
    clearTimeout(t)
  }
}
