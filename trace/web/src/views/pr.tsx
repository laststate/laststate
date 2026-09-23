// Crash-to-PR view — lists PRs generated from crash issues and lets
// maintainers open new ones. Backed by /api/pr/status, /api/prs, /api/pr/create.
import React, { useCallback, useEffect, useState } from 'react'
import { Link } from 'react-router-dom'
import { api } from '../api'
import { Badge } from '../components/ui/badge'
import { Button } from '../components/ui/button'
import { ExternalLink, GitPullRequest, RefreshCw } from '../icons'

interface PRItem {
  id: string
  issue_id: string
  repo: string
  branch: string
  status: string
  html_url?: string
  title?: string
  created_at?: string
}

export default function PRView({ dispatchToast = () => {} }: { dispatchToast?: (t: { id: string; type: 'success' | 'error' | 'info'; message: string }) => void }) {
  const [status, setStatus] = useState<any>(null)
  const [items, setItems] = useState<PRItem[]>([])
  const [loading, setLoading] = useState(true)
  const [creating, setCreating] = useState(false)
  const [issueId, setIssueId] = useState('')
  const [repo, setRepo] = useState('')

  const reload = useCallback(async () => {
    setLoading(true)
    try {
      const [st, list] = await Promise.all([
        api('/api/pr/status').catch(() => null),
        api('/api/prs').catch(() => ({ items: [] })),
      ])
      setStatus(st)
      setItems(list.items || [])
      if (st?.repo && !repo) setRepo(st.repo)
    } finally {
      setLoading(false)
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [])

  useEffect(() => { reload() }, [reload])

  async function createPR(e: React.FormEvent) {
    e.preventDefault()
    if (!issueId.trim() || creating) return
    setCreating(true)
    try {
      const res = await api('/api/pr/create', { method: 'POST', body: { issue_id: issueId.trim(), repo: repo.trim() || undefined } })
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'success', message: 'PR created: ' + (res.html_url || res.id) })
      setIssueId('')
      reload()
    } catch (err: any) {
      dispatchToast({ id: Math.random().toString(36).slice(2), type: 'error', message: err.message || 'Failed to create PR' })
    } finally {
      setCreating(false)
    }
  }

  const enabled = status?.enabled !== false

  return (
    <div className="pr-view" data-testid="pr-panel">
      <div className="panel nw-panel" style={{ marginBottom: '1rem' }}>
        <div className="panel-head">
          <div>
            <h2><GitPullRequest size={18} style={{ verticalAlign: '-3px', marginRight: 6 }} />Crash-to-PR</h2>
            <p className="meta" style={{ margin: 0 }}>
              {enabled
                ? <>Repo <span className="mono">{status?.repo || '—'}</span> · branch <span className="mono">{status?.branch || 'main'}</span></>
                : 'Set TRACE_GITHUB_TOKEN and TRACE_GITHUB_REPO to enable automatic fix branches from crash issues.'}
            </p>
          </div>
          <Button type="button" variant="secondary" size="sm" onClick={reload}>
            <RefreshCw size={13} style={{ marginRight: 4 }} />Refresh
          </Button>
        </div>
      </div>

      {enabled && (
        <div className="panel nw-panel" style={{ marginBottom: '1rem' }}>
          <div className="panel-head"><h3 style={{ margin: 0 }}>Open a fix branch from an issue</h3></div>
          <form className="pr-create-form" onSubmit={createPR}>
            <label>
              Issue ID
              <input className="mono" placeholder="00000000-0000-…" value={issueId}
                onChange={e => setIssueId(e.target.value)} required />
            </label>
            <label>
              Repo (optional)
              <input placeholder="owner/name" value={repo} onChange={e => setRepo(e.target.value)} />
            </label>
            <Button type="submit" disabled={creating || !issueId.trim()}>
              {creating ? 'Creating…' : 'Create PR'}
            </Button>
          </form>
        </div>
      )}

      <div className="panel nw-panel" style={{ padding: 0, overflow: 'hidden' }}>
        {loading ? (
          <div className="empty">Loading PRs…</div>
        ) : items.length === 0 ? (
          <div className="empty" data-testid="pr-empty">
            No crash-to-PR entries yet — resolve an issue and open a fix branch from it.
          </div>
        ) : (
          <table>
            <thead>
              <tr><th>PR</th><th>Issue</th><th>Branch</th><th>Status</th><th /></tr>
            </thead>
            <tbody>
              {items.map((pr, idx) => (
                <tr key={pr.id || idx} className="animate-fade-in-up" style={{ animationDelay: `${idx * 0.03}s` }}>
                  <td className="mono">{pr.id}</td>
                  <td className="mono">
                    {pr.issue_id
                      ? <Link to={`/issues/${pr.issue_id}`}>{String(pr.issue_id).slice(0, 8)}…</Link>
                      : '—'}
                  </td>
                  <td className="mono">{pr.branch || '—'}</td>
                  <td><Badge>{pr.status}</Badge></td>
                  <td>
                    {pr.html_url && (
                      <a className="btn ghost" href={pr.html_url} target="_blank" rel="noreferrer">
                        <ExternalLink size={13} style={{ marginRight: 4 }} />View
                      </a>
                    )}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        )}
      </div>
    </div>
  )
}
