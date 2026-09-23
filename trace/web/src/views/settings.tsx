// Settings panel — real forms for retention, token management, notifications.
// Replaces the old prompt()-driven controls.
import React, { useEffect, useState } from 'react'
import { api } from '../api'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { Bell, Shield, Trash2, Volume2, VolumeX } from '../icons'
import {
  getNotificationPermission,
  requestNotificationPermission,
  isNotificationsEnabled,
  setNotificationsEnabled,
  isSoundEnabled,
  setSoundEnabled,
  clearAlertHistory,
  getAlertHistory,
  sendTestNotification,
} from '../notifications'

interface TokenRow {
  id: string
  name: string
  prefix?: string
  scopes?: string[]
  created_at?: string
  last_used_at?: string | null
}

interface SessionRow {
  id: string
  prefix?: string
  ip?: string
  user_agent?: string
  created_at?: string
  last_used_at?: string | null
  expires_at?: string
}

interface OrgRow {
  id: string
  name: string
  slug?: string
  role?: string
}

interface MemberRow {
  id: string
  email: string
  name: string
  role: string
}

type Toast = { id: string; type: 'success' | 'error' | 'info'; message: string }

export default function SettingsPanel({ data, reload, dispatchToast = () => {} }: {
  data: any
  reload: () => void
  dispatchToast?: (t: Toast) => void
}) {
  const st = data.settings || {}
  const perm = getNotificationPermission()
  const notifOn = isNotificationsEnabled()
  const soundOn = isSoundEnabled()

  const [ret, setRet] = useState({
    events: String(st.retention_events_days ?? 90),
    health: String(st.retention_health_days ?? 30),
    logs: String(st.retention_logs_days ?? 14),
    metrics: String(st.retention_metrics_days ?? 30),
  })
  const [savingRet, setSavingRet] = useState(false)
  const [tokens, setTokens] = useState<TokenRow[]>([])
  const [tokenName, setTokenName] = useState('')
  const [creatingToken, setCreatingToken] = useState(false)
  const [newSecret, setNewSecret] = useState('')
  const [clearing, setClearing] = useState(0)

  // MFA state
  const [mfaOn, setMfaOn] = useState<boolean | null>(null)
  const [mfaSecret, setMfaSecret] = useState('')
  const [mfaUri, setMfaUri] = useState('')
  const [mfaCode, setMfaCode] = useState('')
  const [mfaBusy, setMfaBusy] = useState(false)

  // Sessions state
  const [sessions, setSessions] = useState<SessionRow[]>([])
  const [currentSession, setCurrentSession] = useState('')

  // Organization state
  const [orgs, setOrgs] = useState<OrgRow[]>([])
  const [activeOrg, setActiveOrg] = useState('')
  const [members, setMembers] = useState<MemberRow[]>([])
  const [inviteEmail, setInviteEmail] = useState('')
  const [inviteRole, setInviteRole] = useState('viewer')

  async function refreshTokens() {
    try {
      const res = await api('/api/tokens')
      setTokens(res.items || [])
    } catch {
      setTokens([])
    }
  }

  useEffect(() => { refreshTokens() }, [])

  async function refreshMfa() {
    try {
      const res = await api('/api/auth/mfa/status')
      setMfaOn(!!res.mfa_enabled)
    } catch {
      setMfaOn(null)
    }
  }

  async function refreshSessions() {
    try {
      const res = await api('/api/me/sessions')
      setSessions(res.items || [])
      setCurrentSession(res.current_session_id || '')
    } catch {
      setSessions([])
    }
  }

  async function refreshOrgs() {
    try {
      const res = await api('/api/me/orgs')
      setOrgs(res.items || [])
      setActiveOrg(res.active_org_id || '')
    } catch {
      setOrgs([])
    }
  }

  async function refreshMembers() {
    try {
      const res = await api('/api/organizations/members')
      setMembers(res.items || [])
    } catch {
      setMembers([])
    }
  }

  useEffect(() => {
    refreshMfa()
    refreshSessions()
    refreshOrgs()
    refreshMembers()
  }, [])

  async function enrollMfa(e: React.FormEvent) {
    e.preventDefault()
    if (mfaBusy) return
    setMfaBusy(true)
    try {
      const res = await api('/api/auth/mfa/enroll', { method: 'POST' })
      setMfaSecret(res.secret || '')
      setMfaUri(res.otpauth || '')
    } catch (err: any) {
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'error', message: err.message || 'MFA enroll failed' })
    } finally {
      setMfaBusy(false)
    }
  }

  async function verifyMfa(e: React.FormEvent) {
    e.preventDefault()
    if (mfaBusy || !mfaCode) return
    setMfaBusy(true)
    try {
      await api('/api/auth/mfa/verify', { method: 'POST', body: { code: mfaCode } })
      setMfaSecret('')
      setMfaUri('')
      setMfaCode('')
      await refreshMfa()
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'success', message: 'MFA enabled — logins now require a code' })
    } catch (err: any) {
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'error', message: err.message || 'Invalid code' })
    } finally {
      setMfaBusy(false)
    }
  }

  async function disableMfa() {
    if (mfaBusy) return
    setMfaBusy(true)
    try {
      await api('/api/auth/mfa/disable', { method: 'POST' })
      await refreshMfa()
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'info', message: 'MFA disabled' })
    } catch (err: any) {
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'error', message: err.message || 'Failed' })
    } finally {
      setMfaBusy(false)
    }
  }

  async function revokeSession(id: string) {
    try {
      await api('/api/me/sessions/' + id, { method: 'DELETE' })
      await refreshSessions()
    } catch (err: any) {
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'error', message: err.message || 'Failed' })
    }
  }

  async function revokeOthers() {
    try {
      await api('/api/me/sessions/revoke-others', { method: 'POST' })
      await refreshSessions()
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'info', message: 'Other sessions revoked' })
    } catch (err: any) {
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'error', message: err.message || 'Failed' })
    }
  }

  async function switchOrg(id: string) {
    try {
      await api('/api/auth/switch-org', { method: 'POST', body: { organization_id: id } })
      window.location.reload()
    } catch (err: any) {
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'error', message: err.message || 'Failed' })
    }
  }

  async function inviteMember(e: React.FormEvent) {
    e.preventDefault()
    if (!inviteEmail) return
    try {
      await api('/api/organizations/members', { method: 'POST', body: { email: inviteEmail, role: inviteRole } })
      setInviteEmail('')
      await refreshMembers()
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'success', message: 'Invitation sent' })
    } catch (err: any) {
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'error', message: err.message || 'Failed' })
    }
  }

  async function setMemberRole(id: string, role: string) {
    try {
      await api('/api/organizations/members', { method: 'PATCH', body: { user_id: id, role } })
      await refreshMembers()
    } catch (err: any) {
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'error', message: err.message || 'Failed' })
    }
  }

  async function removeMember(id: string) {
    try {
      await api('/api/organizations/members?user_id=' + encodeURIComponent(id), { method: 'DELETE' })
      await refreshMembers()
    } catch (err: any) {
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'error', message: err.message || 'Failed' })
    }
  }

  async function saveRetention(e: React.FormEvent) {
    e.preventDefault()
    if (savingRet) return
    setSavingRet(true)
    try {
      await api('/api/settings', {
        method: 'PUT',
        body: {
          retention_events_days: Number(ret.events) || 90,
          retention_health_days: Number(ret.health) || 30,
          retention_logs_days: Number(ret.logs) || 14,
          retention_metrics_days: Number(ret.metrics) || 30,
          analyzer_version_min: st.analyzer_version_min || 1,
        },
      })
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'success', message: 'Retention settings saved' })
      reload()
    } catch (err: any) {
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'error', message: err.message || 'Failed to save settings' })
    } finally {
      setSavingRet(false)
    }
  }

  async function createToken(e: React.FormEvent) {
    e.preventDefault()
    const name = tokenName.trim()
    if (!name || creatingToken) return
    setCreatingToken(true)
    try {
      const res = await api('/api/tokens', { method: 'POST', body: { name, scopes: ['event:write', 'event:read', 'artifact:write'] } })
      setNewSecret(res.secret || '')
      setTokenName('')
      await refreshTokens()
    } catch (err: any) {
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'error', message: err.message || 'Failed to create token' })
    } finally {
      setCreatingToken(false)
    }
  }

  async function revokeToken(id: string) {
    try {
      await api('/api/tokens/' + id, { method: 'DELETE' })
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'info', message: 'Token revoked' })
      await refreshTokens()
    } catch (err: any) {
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'error', message: err.message || 'Failed to revoke token' })
    }
  }

  return (
    <div className="panel" data-testid="settings-panel">
      <h2>Project settings</h2>
      <p className="meta">Open UI: {String(data.open_ui)} · Bootstrapped: {String(data.bootstrapped)}</p>

      {/* Browser notifications */}
      <div className="notif-widget" style={{ marginTop: '1.25rem', marginBottom: '1.25rem' }}>
        <div className="notif-widget-head">
          <h3><Bell size={16} /> Browser Notifications &amp; Real-Time Alerts</h3>
          <span className={`notif-status-badge ${perm}`}>
            {perm === 'granted' ? 'Permission Granted' : perm === 'denied' ? 'Blocked in Browser' : 'Permission Pending'}
          </span>
        </div>
        <p className="meta" style={{ marginTop: 0, marginBottom: '1rem' }}>
          Configure how Trace alerts your development environment about new Fatal errors, queue failures, and hardware anomalies.
        </p>
        <div className="grid-2">
          <div>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', marginBottom: '0.75rem', padding: '0.5rem 0.75rem', background: 'rgba(255,255,255,0.02)', borderRadius: '0.375rem' }}>
              <div>
                <strong>Desktop Notifications (Push)</strong>
                <div className="meta" style={{ fontSize: '0.75rem' }}>Native OS cards when critical incidents are detected</div>
              </div>
              <Button
                type="button"
                variant={notifOn ? 'default' : 'secondary'}
                style={{ fontSize: '0.75rem', padding: '0.25rem 0.6rem' }}
                onClick={async () => {
                  if (perm !== 'granted') {
                    const granted = await requestNotificationPermission()
                    if (granted) setNotificationsEnabled(true)
                  } else {
                    setNotificationsEnabled(!notifOn)
                  }
                  setClearing(c => c + 1)
                }}
              >
                {notifOn ? 'Enabled' : 'Disabled'}
              </Button>
            </div>
            <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '0.5rem 0.75rem', background: 'rgba(255,255,255,0.02)', borderRadius: '0.375rem' }}>
              <div>
                <strong>Sound Effects</strong>
                <div className="meta" style={{ fontSize: '0.75rem' }}>Subtle chime via Web Audio API on critical events</div>
              </div>
              <Button
                type="button"
                variant={soundOn ? 'default' : 'secondary'}
                style={{ fontSize: '0.75rem', padding: '0.25rem 0.6rem' }}
                onClick={() => { setSoundEnabled(!soundOn); setClearing(c => c + 1) }}
              >
                {soundOn ? <><Volume2 size={13} style={{ marginRight: 4, verticalAlign: '-2px' }} /> Sound On</> : <><VolumeX size={13} style={{ marginRight: 4, verticalAlign: '-2px' }} /> Muted</>}
              </Button>
            </div>
          </div>
          <div style={{ display: 'flex', flexDirection: 'column', gap: '0.5rem' }}>
            <div style={{ padding: '0.5rem 0.75rem', background: 'rgba(255,255,255,0.02)', borderRadius: '0.375rem' }}>
              <strong>Quick test actions</strong>
              <div className="meta" style={{ fontSize: '0.75rem', marginBottom: '0.5rem' }}>Validate sound and OS alert functionality</div>
              <div className="row gap">
                <Button type="button" variant="secondary" size="sm" onClick={() => sendTestNotification()}>
                  <Bell size={14} style={{ marginRight: 4 }} /> Test Alert
                </Button>
                <Button type="button" variant="ghost" size="sm" onClick={() => { clearAlertHistory(); setClearing(c => c + 1) }}>
                  Clear History ({getAlertHistory().length})
                </Button>
              </div>
            </div>
          </div>
        </div>
      </div>

      <div className="grid-2" style={{ marginTop: '1rem' }}>
        {/* Retention form */}
        <div>
          <h3><Shield size={14} style={{ verticalAlign: '-2px', marginRight: 4 }} />Retention</h3>
          <form className="settings-form" onSubmit={saveRetention}>
            {([
              ['events', 'Events (days)', 90],
              ['health', 'Health (days)', 30],
              ['logs', 'Logs (days)', 14],
              ['metrics', 'Metrics (days)', 30],
            ] as const).map(([key, label, dflt]) => (
              <label key={key}>
                {label}
                <input type="number" min={1} max={3650} placeholder={String(dflt)}
                  value={ret[key]} onChange={e => setRet({ ...ret, [key]: e.target.value })} />
              </label>
            ))}
            <Button type="submit" disabled={savingRet}>{savingRet ? 'Saving…' : 'Save retention'}</Button>
          </form>
          <details style={{ marginTop: '0.75rem' }}>
            <summary className="meta">Raw settings JSON</summary>
            <pre>{JSON.stringify(st, null, 2)}</pre>
          </details>
        </div>

        {/* Tokens */}
        <div>
          <h3>Tokens &amp; tools</h3>
          <form className="settings-form" onSubmit={createToken} style={{ marginBottom: '0.75rem' }}>
            <label>
              New token name
              <input type="text" placeholder="relay" required minLength={2}
                value={tokenName} onChange={e => setTokenName(e.target.value)} />
            </label>
            <Button type="submit" disabled={creatingToken}>{creatingToken ? 'Creating…' : 'Create token'}</Button>
          </form>
          {newSecret && (
            <div className="auth-alert success" data-testid="token-secret" style={{ marginBottom: '0.75rem' }}>
              <strong>Copy your token now — it won't be shown again:</strong>
              <pre className="mono" style={{ margin: '0.35rem 0 0', userSelect: 'all', whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>{newSecret}</pre>
            </div>
          )}
          {tokens.length > 0 && (
            <div className="table-wrap" style={{ marginBottom: '0.75rem' }}>
              <table>
                <thead><tr><th>Name</th><th>Prefix</th><th>Scopes</th><th /></tr></thead>
                <tbody>
                  {tokens.map((t: TokenRow) => (
                    <tr key={t.id}>
                      <td>{t.name}</td>
                      <td className="mono">{t.prefix || t.id.slice(0, 8) + '…'}</td>
                      <td>{(t.scopes || []).map(s => <Badge key={s} variant="outline" style={{ marginRight: 4 }}>{s}</Badge>)}</td>
                      <td>
                        <Button type="button" variant="ghost" size="sm" title="Revoke" onClick={() => revokeToken(t.id)}>
                          <Trash2 size={14} />
                        </Button>
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
          )}
          <div className="row gap">
            <Button type="button" variant="secondary" onClick={async () => {
              try {
                const res = await api('/api/events/reprocess-stale', { method: 'POST' })
                dispatchToast({ id: Math.random().toString(36).slice(2), type: 'info', message: `Queued ${res.queued} events` })
              } catch (err: any) {
                dispatchToast({ id: Math.random().toString(36).slice(2), type: 'error', message: err.message || 'Failed' })
              }
            }}>Reprocess stale</Button>
          </div>
          <p className="meta" style={{ marginTop: 12 }}>
            <a href="/metrics" target="_blank" rel="noreferrer">/metrics</a>
            {' · '}
            <a href="/openapi.json" target="_blank" rel="noreferrer">OpenAPI</a>
          </p>
        </div>
      </div>

      {/* MFA */}
      <div style={{ marginTop: '1.5rem' }}>
        <h3><Shield size={14} style={{ verticalAlign: '-2px', marginRight: 4 }} />Multi-factor auth</h3>
        {mfaOn === null ? (
          <p className="meta">MFA status unavailable.</p>
        ) : mfaOn ? (
          <div className="row gap" style={{ alignItems: 'center' }}>
            <Badge>MFA on — logins require a code</Badge>
            <Button type="button" variant="secondary" size="sm" disabled={mfaBusy} onClick={disableMfa}>Disable MFA</Button>
          </div>
        ) : mfaSecret ? (
          <form className="settings-form" onSubmit={verifyMfa}>
            <p className="meta">Scan this URI in your authenticator app (or type the secret), then enter the 6-digit code to confirm.</p>
            {mfaUri && (
              <pre className="mono" style={{ whiteSpace: 'pre-wrap', wordBreak: 'break-all' }}>{mfaUri}</pre>
            )}
            <p className="meta">Secret: <code className="mono">{mfaSecret}</code></p>
            <label>
              Authenticator code
              <input type="text" inputMode="numeric" required value={mfaCode} onChange={e => setMfaCode(e.target.value)} />
            </label>
            <Button type="submit" disabled={mfaBusy}>{mfaBusy ? 'Verifying…' : 'Verify & enable'}</Button>
          </form>
        ) : (
          <form className="settings-form" onSubmit={enrollMfa}>
            <p className="meta">TOTP authenticator. Enrollment completes only after you verify a code.</p>
            <Button type="submit" disabled={mfaBusy}>{mfaBusy ? 'Starting…' : 'Start MFA enrollment'}</Button>
          </form>
        )}
      </div>

      {/* Sessions */}
      <div style={{ marginTop: '1.5rem' }}>
        <h3>Sessions</h3>
        {sessions.length === 0 ? (
          <p className="meta">No sessions found.</p>
        ) : (
          <>
            <div className="table-wrap">
              <table>
                <thead><tr><th>Session</th><th>IP</th><th>Created</th><th /></tr></thead>
                <tbody>
                  {sessions.map(s => (
                    <tr key={s.id}>
                      <td className="mono">{s.prefix || s.id.slice(0, 12) + '…'}{s.id === currentSession ? ' (this device)' : ''}</td>
                      <td className="mono">{s.ip || '—'}</td>
                      <td className="mono">{s.created_at ? new Date(s.created_at).toLocaleString() : '—'}</td>
                      <td>
                        {s.id !== currentSession && (
                          <Button type="button" variant="ghost" size="sm" onClick={() => revokeSession(s.id)}>Revoke</Button>
                        )}
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <div className="row gap" style={{ marginTop: '0.5rem' }}>
              <Button type="button" variant="secondary" size="sm" onClick={revokeOthers}>Revoke all other sessions</Button>
            </div>
          </>
        )}
      </div>

      {/* Organization */}
      <div style={{ marginTop: '1.5rem' }}>
        <h3>Organization</h3>
        {orgs.length > 0 && (
          <div className="row gap" style={{ marginBottom: '0.75rem', alignItems: 'center' }}>
            <span className="meta">Active:</span>
            {orgs.map(o => (
              <Button key={o.id} type="button" size="sm"
                variant={o.id === activeOrg ? 'default' : 'secondary'}
                onClick={() => o.id !== activeOrg && switchOrg(o.id)}>
                {o.name} · {o.role}
              </Button>
            ))}
          </div>
        )}
        <h3 style={{ marginTop: '1rem' }}>Members</h3>
        {members.length === 0 ? (
          <p className="meta">No members listed (admin only).</p>
        ) : (
          <div className="table-wrap" style={{ marginBottom: '0.75rem' }}>
            <table>
              <thead><tr><th>Email</th><th>Name</th><th>Role</th><th /></tr></thead>
              <tbody>
                {members.map(m => (
                  <tr key={m.id}>
                    <td>{m.email}</td>
                    <td>{m.name}</td>
                    <td>
                      <select value={m.role} onChange={e => setMemberRole(m.id, e.target.value)}
                        style={{ background: 'transparent', border: '1px solid var(--border)', borderRadius: 4, padding: '2px 4px' }}>
                        {['viewer', 'developer', 'maintainer', 'admin', 'owner'].map(r => (
                          <option key={r} value={r}>{r}</option>
                        ))}
                      </select>
                    </td>
                    <td>
                      <Button type="button" variant="ghost" size="sm" title="Remove" onClick={() => removeMember(m.id)}>
                        <Trash2 size={14} />
                      </Button>
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        )}
        <form className="settings-form" onSubmit={inviteMember}>
          <div className="grid-2">
            <label>
              Invite by email
              <input type="email" required value={inviteEmail} onChange={e => setInviteEmail(e.target.value)} />
            </label>
            <label>
              Role
              <select value={inviteRole} onChange={e => setInviteRole(e.target.value)}>
                {['viewer', 'developer', 'maintainer', 'admin'].map(r => (
                  <option key={r} value={r}>{r}</option>
                ))}
              </select>
            </label>
          </div>
          <Button type="submit">Send invite</Button>
        </form>
      </div>
    </div>
  )
}
