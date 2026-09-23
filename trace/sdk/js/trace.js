/**
 * Last State Trace — minimal browser/Node ingest client (LEP binary or JSON envelope).
 * Usage:
 *   import { TraceClient } from './trace.js'
 *   const c = new TraceClient({ baseUrl: 'https://trace.example', token: '...' })
 *   await c.ingestBinary(uint8Array)
 *   await c.heartbeat({ relay_id: 'r1', version: '1.0' })
 */

export class TraceClient {
  /**
   * @param {{ baseUrl: string, token: string, projectId?: string, fetchImpl?: typeof fetch }} opts
   */
  constructor(opts) {
    if (!opts?.baseUrl || !opts?.token) throw new Error('baseUrl and token required')
    this.baseUrl = opts.baseUrl.replace(/\/$/, '')
    this.token = opts.token
    this.projectId = opts.projectId || ''
    this.fetchImpl = opts.fetchImpl || globalThis.fetch.bind(globalThis)
  }

  headers(extra = {}) {
    const h = {
      Authorization: 'Bearer ' + this.token,
      ...extra,
    }
    if (this.projectId) h['X-Project-ID'] = this.projectId
    return h
  }

  async ingestBinary(body, contentType = 'application/octet-stream') {
    const r = await this.fetchImpl(this.baseUrl + '/v1/ingest', {
      method: 'POST',
      headers: this.headers({ 'Content-Type': contentType }),
      body,
    })
    if (!r.ok) throw new Error(`ingest ${r.status}: ${await r.text()}`)
    return r.json()
  }

  async ingestBatch(items) {
    const r = await this.fetchImpl(this.baseUrl + '/v1/events:batch', {
      method: 'POST',
      headers: this.headers({ 'Content-Type': 'application/json' }),
      body: JSON.stringify({ items }),
    })
    if (!r.ok) throw new Error(`batch ${r.status}: ${await r.text()}`)
    return r.json()
  }

  async uploadArtifact(bytes, meta = {}) {
    const r = await this.fetchImpl(this.baseUrl + '/v1/artifacts', {
      method: 'POST',
      headers: this.headers({
        'Content-Type': 'application/octet-stream',
        'X-Artifact-Name': meta.name || 'firmware.elf',
        'X-Build-Id': meta.buildId || '',
      }),
      body: bytes,
    })
    if (!r.ok) throw new Error(`artifact ${r.status}: ${await r.text()}`)
    return r.json()
  }

  async heartbeat(body) {
    const r = await this.fetchImpl(this.baseUrl + '/v1/relay/heartbeat', {
      method: 'POST',
      headers: this.headers({ 'Content-Type': 'application/json' }),
      body: JSON.stringify(body || {}),
    })
    if (!r.ok) throw new Error(`heartbeat ${r.status}: ${await r.text()}`)
    return r.json().catch(() => ({}))
  }

  async capabilities() {
    const r = await this.fetchImpl(this.baseUrl + '/v1/relay/capabilities', {
      headers: this.headers(),
    })
    if (!r.ok) throw new Error(`capabilities ${r.status}`)
    return r.json()
  }
}

export default TraceClient
