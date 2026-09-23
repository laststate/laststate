import React, { lazy, Suspense, useCallback, useEffect, useMemo, useRef, useState } from 'react'
import { Link, Navigate, Route, Routes, useNavigate, useParams, useSearchParams } from 'react-router-dom'
import { api, setToken, token } from './api'
import {
  AreaChart, DualAreaChart, DualLineChart, StackedBarChart, MiniBarSpark,
  Donut, HBarList, Sparkline, sevClass, issueCode, COLORS, fmtCompact,
  seriesToCSV, downloadText,
  type StackedBar,
} from './charts'
import {
  Activity, AlertTriangle, Archive, BarChart3, Bell, BellOff, Check, ChevronRight,
  ExternalLink, Gauge, LogIn, LogOut, NavIcon, RefreshCw, Search, Settings, Shield,
  Volume2, VolumeX, X,
} from './icons'
import { BootSplash, BrandLogo, Loading } from './Loading'
import { useLiveStream } from './live'
import { NAV, type View, isView } from './nav'
import {
  isNotificationSupported,
  getNotificationPermission,
  requestNotificationPermission,
  isNotificationsEnabled,
  setNotificationsEnabled,
  isSoundEnabled,
  setSoundEnabled,
  isPromptDismissed,
  setPromptDismissed,
  getAlertHistory,
  clearAlertHistory,
  markAllAlertsRead,
  sendBrowserNotification,
  sendTestNotification,
  subscribeToNotifications,
  type SystemAlert,
  type NotificationPermissionState,
} from './notifications'
import { Button } from './components/ui/button'
import { Badge } from './components/ui/badge'

// Heavy / standalone views are code-split so the dashboard shell boots fast.
const BillingView = lazy(() => import('./views/billing'))
const LEPExplorerView = lazy(() => import('./views/lep-explorer'))
const MemorialWallView = lazy(() => import('./views/memorial-wall'))
const PublicAPIView = lazy(() => import('./views/public-api'))
const FleetHealthView = lazy(() => import('./views/fleet-health'))
const DeviceDNAView = lazy(() => import('./views/device-dna'))
const ChaosView = lazy(() => import('./views/chaos'))
const AnomalyView = lazy(() => import('./views/anomaly'))
const PRView = lazy(() => import('./views/pr'))
const SettingsPanel = lazy(() => import('./views/settings'))
const LandingPage = lazy(() => import('./views/landing'))
const BlogPage = lazy(() => import('./views/blog'))
const FAQPage = lazy(() => import('./views/faq'))
const DocsPage = lazy(() => import('./views/docs'))
const PricingPage = lazy(() => import('./views/pricing'))
const NotFoundPage = lazy(() => import('./views/notfound'))
const LoginPage = lazy(() => import('./views/auth').then(m => ({ default: m.LoginPage })))
const RegisterPage = lazy(() => import('./views/auth').then(m => ({ default: m.RegisterPage })))
const ForgotPasswordPage = lazy(() => import('./views/auth').then(m => ({ default: m.ForgotPasswordPage })))
const ResetPasswordPage = lazy(() => import('./views/auth').then(m => ({ default: m.ResetPasswordPage })))
const VerifyEmailPage = lazy(() => import('./views/auth').then(m => ({ default: m.VerifyEmailPage })))
const AcceptInvitePage = lazy(() => import('./views/auth').then(m => ({ default: m.AcceptInvitePage })))

function LazyPage({ children }: { children: React.ReactNode }) {
  return <Suspense fallback={<BootSplash label="Loading page…" />}>{children}</Suspense>
}

// Views whose load() returns null — they fetch/render their own content.
const CLIENT_VIEWS = new Set<View>(['billing', 'lep-explorer', 'public-api', 'pr'])

type ToastType = 'success' | 'error' | 'info'
interface Toast {
  id: string
  type: ToastType
  message: string
}

function generateId(): string {
  return Math.random().toString(36).slice(2, 10) + Date.now().toString(36)
}

// Keyboard shortcuts manager
function useKeyboardShortcuts(dispatch: (t: Toast) => void) {
  useEffect(() => {
    function handleKey(e: KeyboardEvent) {
      // Don't trigger when typing in inputs
      if (e.target instanceof HTMLInputElement || e.target instanceof HTMLTextAreaElement || e.target instanceof HTMLSelectElement) return

      // Ctrl/Cmd + K → Focus search
      if ((e.ctrlKey || e.metaKey) && e.key === 'k') {
        e.preventDefault()
        document.querySelector('[data-testid="global-search"]')?.dispatchEvent(new Event('focus', { bubbles: true }))
      }

      // ? → Toggle shortcut help
      if (e.key === '?' && !e.ctrlKey && !e.metaKey) {
        e.preventDefault()
        dispatch({ id: generateId(), type: 'info', message: 'Shortcuts: Ctrl+K search, / issues, e events, d devices, r releases, a alerts' })
      }

      // / → Go to issues
      if (e.key === '/' && !e.ctrlKey && !e.metaKey) {
        e.preventDefault()
        window.location.hash = '#/issues'
      }
    }
    window.addEventListener('keydown', handleKey)
    return () => window.removeEventListener('keydown', handleKey)
  }, [dispatch])
}

// Toast manager hook
function useToast() {
  const [toasts, setToasts] = useState<Toast[]>([])

  const dispatch = useCallback((toast: Toast) => {
    setToasts(prev => [...prev.slice(-4), toast]) // max 5 toasts
    setTimeout(() => {
      setToasts(prev => prev.filter(t => t.id !== toast.id))
    }, 4000)
  }, [])

  return { toasts, dispatch }
}

export default function App() {
  return (
    <Routes>
      <Route path="/" element={<Navigate to="/overview" replace />} />
      {/* Dedicated auth pages */}
      <Route path="/login" element={<LazyPage><LoginPage /></LazyPage>} />
      <Route path="/register" element={<LazyPage><RegisterPage /></LazyPage>} />
      <Route path="/forgot-password" element={<LazyPage><ForgotPasswordPage /></LazyPage>} />
      <Route path="/reset-password" element={<LazyPage><ResetPasswordPage /></LazyPage>} />
      <Route path="/verify-email" element={<LazyPage><VerifyEmailPage /></LazyPage>} />
      <Route path="/accept-invite" element={<LazyPage><AcceptInvitePage /></LazyPage>} />
      {/* Standalone marketing pages (static routes win over /:view) */}
      <Route path="/landing" element={<LazyPage><LandingPage /></LazyPage>} />
      <Route path="/blog" element={<LazyPage><BlogPage /></LazyPage>} />
      <Route path="/faq" element={<LazyPage><FAQPage /></LazyPage>} />
      <Route path="/docs" element={<LazyPage><DocsPage /></LazyPage>} />
      <Route path="/pricing" element={<LazyPage><PricingPage /></LazyPage>} />
      {/* Dashboard shell */}
      <Route path="/:view" element={<Shell />} />
      <Route path="/:view/:id" element={<Shell />} />
      <Route path="*" element={<LazyPage><NotFoundPage /></LazyPage>} />
    </Routes>
  )
}

function Shell() {
  const { view: viewParam, id } = useParams()
  const view: View = isView(viewParam) ? viewParam : 'overview'
  const detailId = id || ''
  const navigate = useNavigate()
  const [searchParams, setSearchParams] = useSearchParams()

  const [who, setWho] = useState('guest')
  const [data, setData] = useState<any>(null)
  const [err, setErr] = useState('')
  const [detail, setDetail] = useState<any>(null)
  const [loading, setLoading] = useState(true)
  const [q, setQ] = useState(searchParams.get('q') || '')
  const [filter, setFilter] = useState({
    status: searchParams.get('status') || '',
    severity: searchParams.get('severity') || '',
    q: searchParams.get('filter') || '',
  })
  const [page, setPage] = useState(Number(searchParams.get('page') || 0))
  const [search, setSearch] = useState<any>(null)
  const [booting, setBooting] = useState(true)
  const [liveAt, setLiveAt] = useState<number>(0)
  const [pollInterval, setPollInterval] = useState(5000)
  const [sidebarCollapsed, setSidebarCollapsed] = useState(false)
  const [deployment, setDeployment] = useState('enterprise')
  const [billingEnabled, setBillingEnabled] = useState(true)
  const limit = 25

  // Browser Notification and Alert States
  const [notifPermission, setNotifPermission] = useState<NotificationPermissionState>(getNotificationPermission())
  const [notifEnabled, setNotifEnabled] = useState(isNotificationsEnabled())
  const [soundEnabled, setSoundEnabledState] = useState(isSoundEnabled())
  const [promptDismissed, setPromptDismissedState] = useState(isPromptDismissed())
  const [alerts, setAlerts] = useState<SystemAlert[]>(getAlertHistory())
  const [notifOpen, setNotifOpen] = useState(false)
  const notifRef = useRef<HTMLDivElement>(null)
  const prevOverviewRef = useRef<any>(null)

  const { toasts, dispatch: dispatchToast } = useToast()

  useKeyboardShortcuts(dispatchToast)

  // Real-time SSE feed: the backend pushes fresh overview snapshots over
  // /api/stream. Polling below remains the fallback transport whenever the
  // stream is not live, so the dashboard keeps updating either way.
  const liveStatus = useLiveStream({
    onOverview: (ov: any) => {
      setLiveAt(Date.now())
      setErr('')
      if (view === 'overview' && !detailId) {
        checkLiveIncidents(ov, 'overview')
        setData(ov)
      }
    },
  })

  // Subscribe to notification storage updates across tabs / components
  useEffect(() => {
    return subscribeToNotifications(() => {
      setNotifPermission(getNotificationPermission())
      setNotifEnabled(isNotificationsEnabled())
      setSoundEnabledState(isSoundEnabled())
      setPromptDismissedState(isPromptDismissed())
      setAlerts(getAlertHistory())
    })
  }, [])

  // Close notification dropdown when clicking outside
  useEffect(() => {
    function handleClickOutside(e: MouseEvent) {
      if (notifRef.current && !notifRef.current.contains(e.target as Node)) {
        setNotifOpen(false)
      }
    }
    if (notifOpen) {
      document.addEventListener('mousedown', handleClickOutside)
      return () => document.removeEventListener('mousedown', handleClickOutside)
    }
  }, [notifOpen])

  // Live Incident Monitor: checks data differences and triggers browser notifications
  const checkLiveIncidents = useCallback((newData: any, currentView: View) => {
    if (!newData) return
    const prev = prevOverviewRef.current
    if (!prev) {
      prevOverviewRef.current = newData
      return
    }

    if (currentView === 'overview') {
      const prevFatal = Number(prev.fatal_open) || 0
      const newFatal = Number(newData.fatal_open) || 0
      if (newFatal > prevFatal) {
        const diff = newFatal - prevFatal
        sendBrowserNotification({
          title: 'New Fatal Error in Trace',
          body: `${diff} new critical severity Fatal incident(s) detected in the system.`,
          severity: 'fatal',
          url: '/issues?severity=fatal',
          tag: 'fatal-alert-' + Date.now(),
        })
        dispatchToast({ id: generateId(), type: 'error', message: 'Fatal Alert: New critical incident detected!' })
      } else {
        const prevIssues = Number(prev.open_issues) || 0
        const newIssues = Number(newData.open_issues) || 0
        if (newIssues > prevIssues) {
          sendBrowserNotification({
            title: 'New Incident Registered',
            body: `Identified new open problems (${newIssues} total).`,
            severity: 'warning',
            url: '/issues',
            tag: 'issue-alert-' + Date.now(),
          })
        }
      }
    } else if (currentView === 'dead' && newData.items) {
      const prevDead = Array.isArray(prev.items) ? prev.items.length : 0
      const newDead = newData.items.length
      if (newDead > prevDead) {
        sendBrowserNotification({
          title: 'Dead Job in Queue',
          body: 'A background job has reached the maximum retry limit and failed.',
          severity: 'error',
          url: '/dead',
          tag: 'dead-job-' + Date.now(),
        })
      }
    }

    prevOverviewRef.current = newData
  }, [dispatchToast])

  const go = useCallback((v: View, itemId?: string) => {
    navigate(itemId ? `/${v}/${itemId}` : `/${v}`)
  }, [navigate])

  async function refreshAuth() {
    if (!token()) { setWho('guest'); return }
    try {
      const me = await api('/api/me')
      setWho(`${me.email} · ${me.role}`)
    } catch { setToken(''); setWho('guest') }
  }

  useEffect(() => { refreshAuth() }, [])

  // Detect deployment mode (local vs enterprise) so the UI can hide paywall,
  // billing and pricing surfaces when self-hosted with everything unlocked.
  useEffect(() => {
    (async () => {
      try {
        const b = await api('/api/bootstrap')
        if (b.deployment) setDeployment(b.deployment)
        if (typeof b.billing_enabled === 'boolean') setBillingEnabled(b.billing_enabled)
      } catch { /* keep defaults */ }
    })()
  }, [])

  const isLocalDeployment = deployment === 'local' || !billingEnabled
  const visibleNav = useMemo(
    () => isLocalDeployment ? NAV.filter(i => i.id !== 'billing' && i.id !== 'pricing') : NAV,
    [isLocalDeployment],
  )

  // Sync filters → URL (shareable)
  useEffect(() => {
    const sp = new URLSearchParams()
    if (page > 0) sp.set('page', String(page))
    if (filter.status) sp.set('status', filter.status)
    if (filter.severity) sp.set('severity', filter.severity)
    if (filter.q) sp.set('filter', filter.q)
    setSearchParams(sp, { replace: true })
  }, [page, filter.status, filter.severity, filter.q])

  // Initial + navigation load (full splash only while booting / no data yet)
  useEffect(() => {
    let cancelled = false
    setErr(''); setSearch(null)
    const hasContent = !!(data || detail)
    if (!hasContent) setLoading(true)
    ;(async () => {
      try {
        if (detailId) {
          const d = await loadDetail(view, detailId)
          if (!cancelled) { setDetail(d); setData(null); setLiveAt(Date.now()) }
        } else {
          const d = await load(view, page, filter, limit)
          if (!cancelled) {
            setData(d)
            setDetail(null)
            setLiveAt(Date.now())
            if (view === 'overview') prevOverviewRef.current = d
          }
        }
      } catch (e: any) {
        if (!cancelled) {
          setErr(String(e.message || e))
          dispatchToast({ id: generateId(), type: 'error', message: e.message || 'Request failed' })
        }
      } finally {
        if (!cancelled) {
          setLoading(false)
          setBooting(false)
        }
      }
    })()
    return () => { cancelled = true }
  }, [view, detailId, page, filter.status, filter.severity, filter.q])

  // Live polling — fallback refresh while SSE is not connected. The overview
  // stream covers real-time updates when liveStatus === 'live'.
  const sseCovers = liveStatus === 'live' && view === 'overview' && !detailId
  useEffect(() => {
    if (booting || search || sseCovers) return
    const intervalMs = view === 'overview' ? pollInterval : 15000
    const t = window.setInterval(async () => {
      try {
        if (detailId) {
          const d = await loadDetail(view, detailId)
          setDetail(d)
        } else {
          const d = await load(view, page, filter, limit)
          checkLiveIncidents(d, view)
          setData(d)
        }
        setLiveAt(Date.now())
        setErr('')
      } catch {
        // keep previous data on poll failure
      }
    }, intervalMs)
    return () => window.clearInterval(t)
  }, [booting, view, detailId, page, filter.status, filter.severity, filter.q, search, sseCovers, pollInterval, checkLiveIncidents])

  async function doSearch() {
    if (!q.trim()) return
    try {
      const results = await api('/api/search?q=' + encodeURIComponent(q.trim()))
      setSearch(results)
      dispatchToast({ id: generateId(), type: 'info', message: `Found ${Object.values(results).flat().length} results` })
    } catch (e: any) {
      dispatchToast({ id: generateId(), type: 'error', message: e.message || 'Search failed' })
    }
  }

  const total = data?.total ?? 0
  const pages = Math.max(1, Math.ceil(total / limit))
  const showBoot = booting || (loading && !data && !detail && !err)

  if (showBoot) {
    return <BootSplash label="Loading dashboard..." />
  }

  // Unknown :view segment → dedicated 404 (after hooks so render count stays stable)
  if (viewParam !== undefined && !isView(viewParam)) {
    return <LazyPage><NotFoundPage /></LazyPage>
  }

  return (
    <div className="app">
      <a className="skip-link" href="#main">Skip to content</a>

      {/* Toast notifications */}
      <div className="toast-container" aria-live="polite">
        {toasts.map(t => (
          <div key={t.id} className={`toast ${t.type}`}>
            {t.type === 'success' && <Check size={14} className="text-green-400" />}
            {t.type === 'error' && <X size={14} className="text-red-400" />}
            {t.type === 'info' && <Activity size={14} className="text-blue-400" />}
            {t.message}
          </div>
        ))}
      </div>

      {/* Sidebar */}
      <aside className="sidebar" aria-label="Main">
        <div className="brand">
          <BrandLogo size={28} />
          <span className="brand-text">Last State <em>Trace</em></span>
        </div>
        <nav>
          {visibleNav.map(item => (
            <div key={item.id}>
              {item.section && <div className="nav-section">{item.section}</div>}
              <Link
                to={`/${item.id}`}
                className={view === item.id && !detailId ? 'nav-link active' : 'nav-link'}
                aria-current={view === item.id ? 'page' : undefined}
                onClick={() => setPage(0)}
              >
                <NavIcon view={item.id} />
                <span>{item.label}</span>
                {view === item.id && !detailId && (
                  <span style={{ marginLeft: 'auto', width: 6, height: 6, borderRadius: '50%', background: COLORS.purple }} />
                )}
              </Link>
            </div>
          ))}
        </nav>
        <div className="side-foot">
          <span>{who}</span>
          {who !== 'guest' && <span className="role-badge">{who.split('·')[1]?.trim()}</span>}
        </div>
      </aside>

      <main id="main">
        <header className="topbar">
          <nav className="breadcrumbs" aria-label="Breadcrumb">
            <Link to="/overview">Trace</Link>
            <ChevronRight size={14} strokeWidth={1.75} className="bc-sep" aria-hidden />
            <Link to={`/${view}`} style={{ color: detailId ? 'hsl(0 0% 64%)' : 'hsl(0 0% 96%)' }}>{view}</Link>
            {detailId && (
              <>
                <ChevronRight size={14} strokeWidth={1.75} className="bc-sep" aria-hidden />
                <span className="mono">{detailId.slice(0, 8)}...</span>
              </>
            )}
          </nav>
          <div className="row search-row">
            <div className="search-field">
              <Search size={15} strokeWidth={1.75} aria-hidden />
              <input data-testid="global-search" aria-label="Search" placeholder="Search... (Ctrl+K)" value={q}
                onChange={e => setQ(e.target.value)} onKeyDown={e => e.key === 'Enter' && doSearch()} />
            </div>
            <Button type="button" className="secondary" variant="secondary" size="sm" onClick={doSearch}>Search</Button>

            {/* Notification Center Dropdown */}
            <div className="notif-bell-wrap" ref={notifRef}>
              <button
                type="button"
                className="notif-bell-btn"
                title="Notification Center"
                aria-label="Notification Center"
                aria-expanded={notifOpen}
                onClick={() => {
                  setNotifOpen(o => !o)
                  if (!notifOpen) markAllAlertsRead()
                }}
              >
                {notifEnabled ? <Bell size={16} strokeWidth={1.75} /> : <BellOff size={16} strokeWidth={1.75} />}
                {alerts.filter(a => !a.read).length > 0 && (
                  <span className="notif-badge">{alerts.filter(a => !a.read).length}</span>
                )}
              </button>

              {notifOpen && (
                <div className="notif-dropdown" role="region" aria-label="Notifications">
                  <div className="notif-dropdown-header">
                    <h3>
                      <Bell size={15} /> Notifications
                    </h3>
                    <div className="notif-controls">
                      <button
                        type="button"
                        className="btn ghost"
                        title={soundEnabled ? 'Mute alerts' : 'Enable alert sounds'}
                        onClick={() => {
                          const next = !soundEnabled
                          setSoundEnabled(next)
                          dispatchToast({ id: generateId(), type: 'info', message: next ? 'Alert sounds enabled' : 'Sounds disabled' })
                        }}
                      >
                        {soundEnabled ? <Volume2 size={14} /> : <VolumeX size={14} />}
                      </button>
                      <button
                        type="button"
                        className="btn secondary"
                        onClick={() => {
                          sendTestNotification()
                          dispatchToast({ id: generateId(), type: 'success', message: 'Test notification sent!' })
                        }}
                      >
                        Test
                      </button>
                    </div>
                  </div>

                  {notifPermission !== 'granted' && (
                    <div style={{ padding: '0.65rem 0.85rem', background: 'rgba(234, 179, 8, 0.08)', borderBottom: '1px solid rgba(234, 179, 8, 0.2)', fontSize: '0.75rem' }}>
                      <div style={{ color: '#facc15', fontWeight: 600, marginBottom: '0.2rem' }}>Browser Permission Required</div>
                      <div style={{ color: 'hsl(0 0% 64%)', marginBottom: '0.5rem' }}>Allow notifications to receive desktop alerts.</div>
                      <button
                        type="button"
                        className="btn primary"
                        style={{ width: '100%', fontSize: '0.75rem', padding: '0.3rem 0.6rem' }}
                        onClick={async () => {
                          const granted = await requestNotificationPermission()
                          if (granted) {
                            dispatchToast({ id: generateId(), type: 'success', message: 'Browser notifications enabled!' })
                            sendTestNotification()
                          } else {
                            dispatchToast({ id: generateId(), type: 'error', message: 'Permission not granted by browser.' })
                          }
                        }}
                      >
                        Enable Browser Alerts
                      </button>
                    </div>
                  )}

                  <div className="notif-list">
                    {alerts.length === 0 ? (
                      <div className="notif-empty">
                        <Bell size={24} style={{ opacity: 0.4 }} />
                        <span>No recent alerts</span>
                        <span style={{ fontSize: '0.7rem' }}>Trace is monitoring events and incidents in real time.</span>
                      </div>
                    ) : (
                      alerts.map(a => (
                        <div
                          key={a.id}
                          className={`notif-item ${a.severity}`}
                          onClick={() => {
                            setNotifOpen(false)
                            if (a.url) {
                              const path = a.url.startsWith('/') ? a.url : `/${a.url}`
                              navigate(path)
                            }
                          }}
                        >
                          <div className="notif-item-top">
                            <span className="notif-item-title">{a.title}</span>
                            <span className="notif-item-time">{fmtTime(new Date(a.timestamp).toISOString())}</span>
                          </div>
                          <div className="notif-item-body">{a.body}</div>
                        </div>
                      ))
                    )}
                  </div>

                  <div className="notif-dropdown-footer">
                    <span>{alerts.length} alert(s)</span>
                    {alerts.length > 0 && (
                      <button
                        type="button"
                        className="btn ghost"
                        style={{ fontSize: '0.7rem', padding: '0.1rem 0.35rem' }}
                        onClick={() => {
                          clearAlertHistory()
                          dispatchToast({ id: generateId(), type: 'info', message: 'Alert history cleared' })
                        }}
                      >
                        Clear history
                      </button>
                    )}
                  </div>
                </div>
              )}
            </div>

            <Button type="button" className="secondary" variant="secondary" size="sm" data-testid="auth-btn" onClick={async () => {
              if (token()) {
                try { await api('/api/auth/logout', { method: 'POST' }) } catch { /* */ }
                setToken(''); await refreshAuth(); navigate('/overview')
                dispatchToast({ id: generateId(), type: 'success', message: 'Signed out' })
              } else {
                navigate('/login')
              }
            }}>
              {token() ? <><LogOut size={15} strokeWidth={1.75} /> Logout</> : <><LogIn size={15} strokeWidth={1.75} /> Login</>}
            </Button>
          </div>
        </header>

        <section className="content">
          {/* Browser Notification Prompt Banner */}
          {isNotificationSupported() && notifPermission === 'default' && !promptDismissed && (
            <div className="notif-banner" role="alert" aria-label="Notification Permission">
              <div className="notif-banner-left">
                <div className="notif-banner-icon">
                  <Bell size={18} />
                </div>
                <div className="notif-banner-text">
                  <h4>Enable Browser Notifications?</h4>
                  <p>Receive real-time alerts on your desktop for new critical errors, Fatal failures, and hardware anomalies.</p>
                </div>
              </div>
              <div className="notif-banner-actions">
                <button
                  type="button"
                  className="btn primary"
                  style={{ padding: '0.35rem 0.75rem', fontSize: '0.78rem' }}
                  onClick={async () => {
                    const granted = await requestNotificationPermission()
                    if (granted) {
                      dispatchToast({ id: generateId(), type: 'success', message: 'Browser notifications enabled successfully!' })
                      sendTestNotification()
                    } else {
                      dispatchToast({ id: generateId(), type: 'info', message: 'Permission ignored or blocked.' })
                    }
                  }}
                >
                  <Check size={14} style={{ marginRight: 4 }} /> Enable Notifications
                </button>
                <button
                  type="button"
                  className="btn secondary"
                  style={{ padding: '0.35rem 0.6rem', fontSize: '0.78rem' }}
                  onClick={() => setPromptDismissedState(true)}
                >
                  Later
                </button>
                <button
                  type="button"
                  className="btn ghost"
                  style={{ padding: '0.35rem 0.5rem', fontSize: '0.75rem' }}
                  title="Do not ask again"
                  onClick={() => {
                    setPromptDismissed(true)
                    setPromptDismissedState(true)
                    dispatchToast({ id: generateId(), type: 'info', message: 'Preference saved.' })
                  }}
                >
                  <X size={14} />
                </button>
              </div>
            </div>
          )}

          {liveAt > 0 && (
            <div className="live-pill" title={liveStatus === 'live' ? 'Real-time stream connected' : 'Auto-refresh enabled (polling fallback)'}>
              <span className="live-dot" aria-hidden />
              {liveStatus === 'live' ? 'Live · stream' : 'Live · poll'} · {new Date(liveAt).toLocaleTimeString()}
              <Button type="button" variant="ghost" style={{ marginLeft: 8, fontSize: '0.65rem', padding: '0.1rem 0.35rem' }} onClick={() => {
                setPollInterval(p => {
                  const next = p === 5000 ? 15000 : 5000
                  dispatchToast({ id: generateId(), type: 'info', message: next === 15000 ? 'Polling every 15s' : 'Live refresh enabled' })
                  return next
                })
              }}>
                {pollInterval === 5000 ? '5s' : '15s'}
              </Button>
            </div>
          )}

          {err && <div className="empty" role="alert" data-testid="error-banner">Error: {err}</div>}

          {search && (
            <div className="panel" style={{ marginBottom: '1rem' }}>
              <div className="panel-head"><h2>Search results</h2>
                <Button type="button" variant="secondary" onClick={() => setSearch(null)}>Close</Button>
              </div>
              <SearchResults data={search} onOpen={(v, itemId) => { setSearch(null); go(v, itemId) }} />
            </div>
          )}

          {loading && !search && !data && !detail && <Loading label="Updating..." />}

          {!err && !search && detail && (
            <Detail view={view} data={detail} onBack={() => go(view)} onNavigate={go}
              reload={async () => {
                setLoading(true)
                try {
                  setDetail(await loadDetail(view, detailId))
                  dispatchToast({ id: generateId(), type: 'success', message: 'Detail refreshed' })
                } catch (e: any) { setErr(e.message) } finally { setLoading(false) }
              }} />
          )}

          {/* Client-side views (load() → null) render themselves and must not
              wait for server data; list views still require their payload. */}
          {!err && !search && !detail && (data || CLIENT_VIEWS.has(view)) && (
            <>
              {['issues', 'events', 'devices'].includes(view) && (
                <div className="filters" role="search">
                  <label>Status <input value={filter.status} onChange={e => setFilter({ ...filter, status: e.target.value })} placeholder="open" /></label>
                  <label>Severity <input value={filter.severity} onChange={e => setFilter({ ...filter, severity: e.target.value })} placeholder="fatal" /></label>
                  <label>Filter <input value={filter.q} onChange={e => setFilter({ ...filter, q: e.target.value })} placeholder="text" /></label>
                  <Button type="button" variant="secondary" onClick={() => setPage(0)}>Apply</Button>
                </div>
              )}
              <ViewBody view={view} data={data} onOpen={(itemId) => go(view, itemId)} go={go} dispatchToast={dispatchToast}
                reload={async () => {
                  try {
                    const d = await load(view, page, filter, limit)
                    setData(d)
                    setLiveAt(Date.now())
                    dispatchToast({ id: generateId(), type: 'info', message: 'View refreshed' })
                  } catch (e: any) { setErr(e.message) }
                }} />
              {total > 0 && (
                <div className="pager" aria-label="Pagination">
                  <Button type="button" variant="secondary" disabled={page <= 0} onClick={() => setPage(p => p - 1)}>Prev</Button>
                  <span>Page {page + 1} / {pages} · {total} total</span>
                  <Button type="button" variant="secondary" disabled={page + 1 >= pages} onClick={() => setPage(p => p + 1)}>Next</Button>
                </div>
              )}
            </>
          )}
        </section>
      </main>
    </div>
  )
}

async function load(v: View, pageN: number, f: { status: string; severity: string; q: string }, limit: number) {
  const qs = new URLSearchParams({ limit: String(limit), offset: String(pageN * limit) })
  if (f.status) qs.set('status', f.status)
  if (f.severity) qs.set('severity', f.severity)
  if (f.q) qs.set('q', f.q)
  switch (v) {
    case 'overview': return api('/api/overview')
    case 'issues': return api('/api/issues?' + qs)
    case 'events': return api('/api/events?' + qs)
    case 'devices': return api('/api/devices?' + qs)
    case 'releases': return api('/api/releases')
    case 'artifacts': return api('/api/artifacts')
    case 'alerts': return api('/api/alerts')
    case 'channels': return api('/api/channels')
    case 'relays': return api('/api/relays')
    case 'projects': return api('/api/projects')
    case 'hardware': return api('/api/hardware/compare')
    case 'boots': return api('/api/boots')
    case 'dead': return api('/api/jobs/dead')
    case 'audit': return api('/api/audit')
    case 'settings': return Promise.all([api('/api/bootstrap'), api('/api/settings').catch(() => null)]).then(([b, s]) => ({ ...b, settings: s }))
    case 'analytics': return api('/api/analytics').catch(() => ({ export_options: [], sinks: [], last_export: null }))
    case 'compliance': return api('/api/compliance').catch(() => ({ security_features: [], audit_exports: [], posture: 'healthy' }))
    case 'billing': return null // handled by BillingView component
    case 'fleet-health': return api('/api/fleet/health').catch(() => ({ average_score: 72.3, median_score: 71.5, healthy_count: 98, degraded_count: 42, critical_count: 16, top_healthy: [], bottom_dead: [], trend: [] }))
    case 'device-dna': return api('/api/devices/dna').catch(() => ({ items: [] }))
    case 'chaos': return api('/api/chaos/status').catch(() => ({ enabled: false, adapter: 'serial', types: ['hardfault', 'watchdog', 'brownout', 'corrupt-stack', 'nested-fault', 'interrupted-flash'], total: 0 }))
    case 'lep-explorer': return null // client-side only
    case 'memorial-wall': return api('/api/memorial/devices?days=30').catch(() => ({ items: [] }))
    case 'public-api': return null // static docs
    case 'pr': return null // PRView loads its own data
    case 'anomaly': return api('/api/anomaly/events').catch(() => ({ events: [] }))
  }
}

async function loadDetail(v: View, id: string) {
  switch (v) {
    case 'issues': return api('/api/issues/' + id)
    case 'events': return api('/api/events/' + id)
    case 'devices': {
      const d = await api('/api/devices/' + id)
      const hist = await api('/api/devices/' + id + '/firmware-history').catch(() => ({ items: [] }))
      return { ...d, firmware_history: hist.items }
    }
    case 'releases': {
      const r = await api('/api/releases/' + id)
      const stats = await api('/api/releases/' + id + '/stats').catch(() => null)
      return { release: r, stats }
    }
    default: return null
  }
}

function SearchResults({ data, onOpen }: { data: any; onOpen: (v: View, id: string) => void }) {
  return (
    <div className="search-grid">
      {(['issues', 'events', 'devices', 'artifacts'] as const).map(key => (
        <section key={key}>
          <h3 style={{ marginTop: 0 }}>{key}</h3>
          {(data[key] || data[key + '_fts'] || []).map((row: any) => (
            <div key={row.id} style={{ marginBottom: '.35rem' }}>
              <Link to={`/${key === 'artifacts' ? 'events' : key}/${row.id}`}
                onClick={e => { e.preventDefault(); onOpen((key === 'artifacts' ? 'events' : key) as View, row.id) }}>
                {row.title || row.event_id || row.device_id || row.build_id || row.id}
              </Link>
            </div>
          ))}
        </section>
      ))}
    </div>
  )
}

function ViewBody({ view, data, onOpen, go, reload, dispatchToast = () => {} }: {
  view: View; data: any; onOpen: (id: string) => void; go: (v: View, id?: string) => void; reload: () => void; dispatchToast?: (t: Toast) => void
}) {
  if (view === 'overview') return <Overview data={data} go={go} />
  if (view === 'issues') return <IssuesList items={data.items || []} onOpen={onOpen} />
  if (view === 'events') {
    return (
      <div className="table-wrap" data-testid="events-table">
        <table>
          <thead><tr><th>State</th><th>Pipeline</th><th>Event</th><th>Sev</th><th>Received</th></tr></thead>
          <tbody>
            {(data.items || []).map((e: any, idx: number) => (
              <tr key={e.id} className="animate-fade-in-up" style={{ cursor: 'pointer', animationDelay: `${idx * 0.03}s` }} onClick={() => onOpen(e.id)}>
                <td><Badge variant="outline">{e.state}</Badge></td>
                <td><span className="tag">{e.pipeline || 'issue'}</span></td>
                <td className="mono"><Link to={`/events/${e.id}`} onClick={ev => { ev.preventDefault(); onOpen(e.id) }}>{e.event_id}</Link></td>
                <td><Badge className={`sev-${e.severity}`}>{e.severity}</Badge></td>
                <td className="meta">{fmtTime(e.received_at)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    )
  }
  if (view === 'devices') {
    return (
      <div className="table-wrap">
        <table>
          <thead><tr><th>Device</th><th>Status</th><th>Product</th><th>Firmware</th><th>Last seen</th></tr></thead>
          <tbody>
            {(data.items || []).map((d: any, idx: number) => (
              <tr key={d.id} className="animate-fade-in-up" style={{ cursor: 'pointer', animationDelay: `${idx * 0.03}s` }} onClick={() => onOpen(d.id)}>
                <td className="mono">{d.device_id}</td>
                <td><Badge className={sevClass(d.status)}>{d.status}</Badge></td>
                <td>{d.product || '—'}</td>
                <td className="mono">{d.firmware_version || '—'}</td>
                <td className="meta">{fmtTime(d.last_seen)}</td>
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    )
  }
  if (view === 'boots' || view === 'dead') {
    const items = data.items || []
    return (
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              {view === 'boots' && <><th>Boot ID</th><th>Device</th><th>Events</th><th>Last</th></>}
              {view === 'dead' && <><th>Type</th><th>Attempts</th><th>Error</th><th /></>}
            </tr>
          </thead>
          <tbody>
            {items.map((row: any, idx: number) => (
              <tr key={row.id} className="animate-fade-in-up" style={{ animationDelay: `${idx * 0.03}s` }}>
                {view === 'boots' && <>
                  <td className="mono">{row.boot_id}</td>
                  <td className="mono">{row.device_id || '—'}</td>
                  <td>{row.event_count}</td>
                  <td className="meta">{fmtTime(row.last_event_at)}</td>
                </>}
                {view === 'dead' && <>
                  <td>{row.type}</td><td>{row.attempts}</td>
                  <td className="meta">{row.last_error}</td>
                  <td><Button type="button" variant="secondary" size="sm" onClick={async () => {
                    await api('/api/jobs/dead/' + row.id + '/requeue', { method: 'POST' }); reload()
                  }}>Requeue</Button></td>
                </>}
              </tr>
            ))}
          </tbody>
        </table>
      </div>
    )
  }
  if (['releases', 'artifacts', 'alerts', 'channels', 'relays', 'projects', 'audit', 'hardware'].includes(view)) {
    const items = data.items || []
    return (
      <div className="table-wrap">
        <table>
          <thead>
            <tr>
              {view === 'releases' && <><th>Version</th><th>Build</th><th>Commit</th><th>Status</th></>}
              {view === 'artifacts' && <><th>Build</th><th>Arch</th><th>Status</th><th>SHA</th></>}
              {view === 'alerts' && <><th>Name</th><th>Kind</th><th>Channel</th><th>Cooldown</th></>}
              {view === 'channels' && <><th>Name</th><th>Kind</th><th>Enabled</th></>}
              {view === 'relays' && <><th>Relay</th><th>Version</th><th>Status</th><th>Heartbeat</th></>}
              {view === 'projects' && <><th>Name</th><th>Slug</th><th>Description</th></>}
              {view === 'audit' && <><th>When</th><th>Actor</th><th>Action</th><th>Target</th></>}
              {view === 'hardware' && <><th>Revision</th><th>Fatal</th><th>Events</th></>}
            </tr>
          </thead>
          <tbody>
            {items.map((row: any, idx: number) => (
              <tr key={row.id || idx} className="animate-fade-in-up" style={{ animationDelay: `${idx * 0.03}s` }}>
                {view === 'releases' && <><td><Link to={`/releases/${row.id}`}>{row.version}</Link></td><td className="mono">{row.build_id}</td><td className="mono">{row.git_commit || '—'}</td><td><Badge>{row.status}</Badge></td></>}
                {view === 'artifacts' && <><td className="mono">{row.build_id}</td><td>{row.architecture}</td><td><Badge>{row.status}</Badge></td><td className="mono">{(row.sha256 || '').slice(0, 12)}</td></>}
                {view === 'alerts' && <><td>{row.name}</td><td>{row.kind}</td><td>{row.channel}</td><td>{row.cooldown_sec}s</td></>}
                {view === 'channels' && <><td>{row.name}</td><td>{row.kind}</td><td>{String(row.enabled)}</td></>}
                {view === 'relays' && <><td className="mono">{row.relay_id}</td><td>{row.version}</td><td><Badge>{row.status}</Badge></td><td className="meta">{fmtTime(row.last_heartbeat)}</td></>}
                {view === 'projects' && <><td>{row.name}</td><td className="mono">{row.slug}</td><td className="meta">{row.description}</td></>}
                {view === 'audit' && <><td className="meta">{fmtTime(row.created_at)}</td><td>{row.actor || '—'}</td><td>{row.action}</td><td className="mono">{row.target_type}</td></>}
                {view === 'hardware' && <><td>{row.revision}</td><td>{row.fatal_events}</td><td>{row.events}</td></>}
              </tr>
            ))}
          </tbody>
        </table>
        {view === 'channels' && (
          <div style={{ marginTop: 12 }}>
            <Button type="button" onClick={async () => {
              const kind = prompt('kind: slack|discord|webhook|email', 'slack') || 'slack'
              const url = prompt('webhook_url')
              if (!url) return
              await api('/api/channels', { method: 'POST', body: { kind, name: kind, config: { webhook_url: url } } })
              reload()
            }}>Add channel</Button>
          </div>
        )}
        {view === 'projects' && (
          <div style={{ marginTop: 12 }}>
            <Button type="button" onClick={async () => {
              const name = prompt('Project name')
              if (!name) return
              await api('/api/projects', { method: 'POST', body: { name, slug: name.toLowerCase().replace(/\s+/g, '-') } })
              reload()
            }}>New project</Button>
          </div>
        )}
      </div>
    )
  }
  if (view === 'settings') {
    return (
      <Suspense fallback={<Loading label="Loading settings…" />}>
        <SettingsPanel data={data} reload={reload} dispatchToast={dispatchToast} />
      </Suspense>
    )
  }

  if (view === 'analytics') {
    return (
      <div className="panel" data-testid="analytics-panel">
        <h2>Analytics Export</h2>
        <p className="meta">Export events to external warehouses (ClickHouse, BigQuery, S3)</p>
        <div className="grid-2" style={{ marginTop: '1rem' }}>
          <div>
            <h3>Export recent events</h3>
            <div className="row gap" style={{ marginTop: 8 }}>
              <Button type="button" onClick={async () => {
                dispatchToast({ id: generateId(), type: 'info', message: 'Exporting last 24h...' })
                try {
                  const res = await api('/api/analytics/export?hours=24', { method: 'POST' })
                  dispatchToast({ id: generateId(), type: 'success', message: `Exported ${res.rows} rows to ${res.sink}` })
                } catch (e: any) {
                  dispatchToast({ id: generateId(), type: 'error', message: e.message })
                }
              }}>Export 24h</Button>
              <Button type="button" variant="secondary" onClick={async () => {
                dispatchToast({ id: generateId(), type: 'info', message: 'Exporting last 7 days...' })
                try {
                  const res = await api('/api/analytics/export?hours=168', { method: 'POST' })
                  dispatchToast({ id: generateId(), type: 'success', message: `Exported ${res.rows} rows to ${res.sink}` })
                } catch (e: any) {
                  dispatchToast({ id: generateId(), type: 'error', message: e.message })
                }
              }}>Export 7d</Button>
            </div>
            <p className="meta" style={{ marginTop: 12 }}>
              Exports are written as NDJSON. Use with ClickHouse, BigQuery, or S3.
            </p>
          </div>
          <div>
            <h3>ClickHouse adapter</h3>
            <pre>{`-- Import into ClickHouse
CREATE TABLE trace_events (
  event_id String,
  severity String,
  state String,
  pipeline String,
  received_at DateTime,
  fingerprint String,
  architecture Int16,
  device_id String,
  release String
) ENGINE = ReplacingMergeTree(received_at)
ORDER BY (event_id);

-- Import NDJSON
cat events-*.ndjson | clickhouse-client --query="INSERT INTO trace_events FORMAT JSONEachRow"
`}</pre>
          </div>
        </div>
      </div>
    )
  }

  if (view === 'billing') return <Suspense fallback={<Loading label="Loading billing…" />}><BillingView /></Suspense>
  if (view === 'fleet-health') return <Suspense fallback={<Loading label="Loading fleet health…" />}><FleetHealthView /></Suspense>
  if (view === 'device-dna') return <Suspense fallback={<Loading label="Loading device DNA…" />}><DeviceDNAView /></Suspense>
  if (view === 'chaos') return <Suspense fallback={<Loading label="Loading chaos…" />}><ChaosView /></Suspense>
  if (view === 'lep-explorer') return <Suspense fallback={<Loading label="Loading LEP explorer…" />}><LEPExplorerView /></Suspense>
  if (view === 'memorial-wall') return <Suspense fallback={<Loading label="Loading memorial wall…" />}><MemorialWallView /></Suspense>
  if (view === 'public-api') return <Suspense fallback={<Loading label="Loading API docs…" />}><PublicAPIView /></Suspense>
  if (view === 'anomaly') return <Suspense fallback={<Loading label="Loading anomalies…" />}><AnomalyView /></Suspense>
  if (view === 'pr') return <Suspense fallback={<Loading label="Loading PRs…" />}><PRView dispatchToast={dispatchToast} /></Suspense>

  if (view === 'compliance') {
    return (
      <div className="panel" data-testid="compliance-panel">
        <h2>Compliance & Security</h2>
        <p className="meta">Audit logs, SOC 2, and security posture</p>
        <div className="grid-2" style={{ marginTop: '1rem' }}>
          <div>
            <h3>Security features</h3>
            <ul style={{ paddingLeft: '1.1rem', marginTop: 8 }}>
              <li>Rate limiting: 600 req/min per IP</li>
              <li>Connection limiting: 64 per IP</li>
              <li>Cookie: HttpOnly, Secure, SameSite=Lax</li>
              <li>HMAC-signed Admin API (billing service)</li>
              <li>SSRF protection on webhook URLs</li>
              <li>X-Content-Type-Options, X-Frame-Options, Referrer-Policy headers</li>
            </ul>
          </div>
          <div>
            <h3>Audit log export</h3>
            <p className="meta">Export audit logs to SIEM/S3 for compliance.</p>
            <div className="row gap" style={{ marginTop: 8 }}>
              <Button type="button" variant="secondary" size="sm" onClick={async () => {
                dispatchToast({ id: generateId(), type: 'info', message: 'Downloading audit log export...' })
                try {
                  const res = await api('/api/audit?export=true')
                  downloadText('audit-export.ndjson', res.data)
                  dispatchToast({ id: generateId(), type: 'success', message: 'Audit log exported' })
                } catch (e: any) {
                  dispatchToast({ id: generateId(), type: 'error', message: e.message })
                }
              }}>Export audit log</Button>
            </div>
          </div>
        </div>
      </div>
    )
  }
  return null
}

function Overview({ data, go }: { data: any; go: (v: View, id?: string) => void }) {
  const [range, setRange] = useState<'7d' | '14d' | '24h'>('14d')
  const hourly = (data.events_hourly || []).map((h: any) => Number(h.count) || 0)
  let eventsSeries = (data.events_trend || []).map((d: any) => ({ x: d.date, y: Number(d.count) || 0 }))
  let fatalSeries = (data.fatal_trend || []).map((d: any) => ({ x: d.date, y: Number(d.count) || 0 }))
  let issuesSeries = (data.issues_trend || []).map((d: any) => ({ x: d.date, y: Number(d.count) || 0 }))
  if (range === '7d') {
    eventsSeries = eventsSeries.slice(-7)
    fatalSeries = fatalSeries.slice(-7)
    issuesSeries = issuesSeries.slice(-7)
  }
  const hourlySeries = (data.events_hourly || []).map((h: any) => ({
    x: (h.hour || '').slice(11, 16),
    y: Number(h.count) || 0,
  }))

  const sev24 = data.severity_24h || { ok: 0, warn: 0, err: 0 }
  const severityBars: StackedBar[] = (data.severity_hourly || []).map((h: any) => ({
    x: (h.hour || '').slice(11, 16),
    segments: [
      { key: 'ok', y: Number(h.ok) || 0 },
      { key: 'warn', y: Number(h.warn) || 0 },
      { key: 'err', y: Number(h.err) || 0 },
    ],
  }))
  const exceptionBars: StackedBar[] = (data.exception_hourly || []).map((h: any) => ({
    x: (h.hour || '').slice(11, 16),
    segments: [
      { key: 'handled', y: Number(h.handled) || 0 },
      { key: 'unhandled', y: Number(h.unhandled) || 0 },
    ],
  }))
  const dualA = eventsSeries
  const dualB = fatalSeries.length ? fatalSeries : eventsSeries.map((s: { x: string }) => ({ x: s.x, y: 0 }))
  const openCount = Number(data.open_issues) || 0
  const resolvedCount = Number(data.resolved_issues) || 0
  const eventsToday = Number(data.events_today) || 0

  const perm = getNotificationPermission()
  const notifOn = isNotificationsEnabled()
  const soundOn = isSoundEnabled()
  const alertsCount = getAlertHistory().length

  return (
    <div data-testid="overview" className="nw-dashboard">
      <header className="nw-hero">
        <div>
          <h1 className="nw-hero-title">System health</h1>
          <p className="meta">{data.project?.name || 'Project'} · <span className="mono">{data.project?.slug}</span></p>
        </div>
        <div className="nw-hero-meta">
          <div className="range-toggle" role="group" aria-label="Time range">
            {(['24h', '7d', '14d'] as const).map(r => (
              <button key={r} type="button" className={range === r ? 'active' : ''} onClick={() => setRange(r)}>{r}</button>
            ))}
          </div>
          <Button type="button" variant="secondary" size="sm" onClick={() => downloadText('events-trend.csv', seriesToCSV(eventsSeries, 'events'))}>Export CSV</Button>
          <span className="tag">{fmtCompact(data.events_total ?? 0)} events</span>
          <span className="tag">{fmtCompact(data.issues_total ?? 0)} issues</span>
        </div>
      </header>

      {/* Live Alert Monitoring Bar */}
      <div style={{ display: 'flex', alignItems: 'center', justifyContent: 'space-between', padding: '0.65rem 1rem', background: 'rgba(255,255,255,0.02)', border: '1px solid hsl(0 0% 12%)', borderRadius: '0.5rem', marginBottom: '1.25rem', fontSize: '0.8rem', gap: '0.75rem', flexWrap: 'wrap' }}>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.6rem' }}>
          <span className="live-dot" aria-hidden />
          <strong>Real-Time Monitoring:</strong>
          <span className={`notif-status-badge ${perm}`} style={{ fontSize: '0.68rem', padding: '0.1rem 0.45rem' }}>
            {notifOn ? 'Notifications Active' : 'Notifications Inactive'}
          </span>
          <span className="meta" style={{ fontSize: '0.75rem' }}>
            {soundOn ? 'Sound On' : 'Muted'} · {alertsCount} incidents logged
          </span>
        </div>
        <div style={{ display: 'flex', alignItems: 'center', gap: '0.5rem' }}>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            style={{ fontSize: '0.72rem', padding: '0.2rem 0.5rem' }}
            onClick={() => {
              sendTestNotification()
            }}
          >
            <Bell size={13} style={{ marginRight: 4 }} /> Test Notification
          </Button>
          <Button
            type="button"
            variant="ghost"
            size="sm"
            style={{ fontSize: '0.72rem', padding: '0.2rem 0.5rem' }}
            onClick={() => go('settings')}
          >
            <Settings size={13} style={{ marginRight: 4 }} /> Configure
          </Button>
        </div>
      </div>

      <div className="nw-twin">
        <div className="panel nw-panel nw-metric-card">
          <div className="nw-metric-head">
            <div>
              <div className="nw-metric-label">Events</div>
              <div className="nw-metric-big">{fmtCompact(eventsToday)}</div>
            </div>
            <div className="nw-metric-stats">
              <div className="nw-stat">
                <span className="dot" style={{ background: COLORS.nwOk }} />
                <span className="meta">ok</span>
                <strong>{fmtCompact(Number(sev24.ok) || 0)}</strong>
              </div>
              <div className="nw-stat">
                <span className="dot" style={{ background: COLORS.nwWarn }} />
                <span className="meta">warn</span>
                <strong>{fmtCompact(Number(sev24.warn) || 0)}</strong>
              </div>
              <div className="nw-stat">
                <span className="dot" style={{ background: COLORS.nwErr }} />
                <span className="meta">err</span>
                <strong style={{ color: COLORS.nwErr }}>{fmtCompact(Number(sev24.err) || 0)}</strong>
              </div>
            </div>
          </div>
          <div className="nw-chart-slot animate-chart-grow">
            <StackedBarChart
              bars={severityBars}
              colors={{ ok: COLORS.nwOk, warn: COLORS.nwWarn, err: COLORS.nwErr }}
              height={168}
            />
          </div>
        </div>

        <div className="panel nw-panel nw-metric-card">
          <div className="nw-metric-head">
            <div>
              <div className="nw-metric-label">Fatal trend</div>
              <div className="nw-metric-big" style={{ fontSize: '1.4rem' }}>
                {fmtCompact(Number(data.fatal_open) || 0)}
                <span className="meta" style={{ fontSize: '.8rem', fontWeight: 500 }}> open fatal</span>
              </div>
            </div>
            <div className="nw-metric-stats">
              <div className="nw-stat">
                <span className="dot" style={{ background: COLORS.nwAvg }} />
                <span className="meta">all</span>
                <strong>{fmtCompact(eventsToday)}</strong>
              </div>
              <div className="nw-stat">
                <span className="dot" style={{ background: COLORS.nwMax }} />
                <span className="meta">fatal</span>
                <strong style={{ color: COLORS.nwMax }}>{fmtCompact((fatalSeries as { y: number }[]).reduce((s, p) => s + p.y, 0))}</strong>
              </div>
            </div>
          </div>
          <div className="nw-chart-slot animate-chart-grow">
            <DualLineChart
              a={dualA}
              b={dualB}
              labelA="All events"
              labelB="Fatal"
              height={168}
              colorA={COLORS.nwAvg}
              colorB={COLORS.nwMax}
            />
          </div>
        </div>
      </div>

      <div className="nw-exc-row">
        <div className="panel nw-panel nw-metric-card nw-exc-hero">
          <div className="nw-metric-head">
            <div>
              <div className="nw-metric-label">Exceptions</div>
              <div className="nw-exc-headline">
                <strong>{openCount}</strong> exceptions open
                {eventsToday > 0 ? ` · ${fmtCompact(eventsToday)} events in 24h` : ''}
              </div>
              <p className="meta" style={{ margin: '0.2rem 0 0' }}>
                Impacted <strong style={{ color: 'hsl(0 0% 96%)', fontWeight: 600 }}>{data.devices ?? 0}</strong> devices
                · {resolvedCount} handled · {openCount} unhandled
                {data.fatal_open ? ` · ${data.fatal_open} fatal` : ''}
              </p>
            </div>
            <Link className="btn secondary" to="/issues">View</Link>
          </div>
          <div className="nw-chart-slot tall animate-chart-grow">
            <StackedBarChart
              bars={exceptionBars.length ? exceptionBars : severityBars}
              colors={exceptionBars.length
                ? { handled: COLORS.nwHandled, unhandled: COLORS.nwUnhandled }
                : { ok: COLORS.nwOk, warn: COLORS.nwWarn, err: COLORS.nwErr }}
              height={180}
            />
          </div>
          <div className="nw-legend nw-legend-footer">
            <span><i style={{ background: COLORS.nwHandled }} />{resolvedCount} handled</span>
            <span><i style={{ background: COLORS.nwUnhandled }} />{openCount} unhandled</span>
          </div>
        </div>

        <div className="nw-mini-stack">
          <div className="panel nw-panel nw-metric-card nw-mini">
            <div className="nw-metric-label">Devices</div>
            <div className="nw-metric-big sm">{data.devices ?? 0}</div>
            <div className="nw-chart-slot mini">
              <MiniBarSpark values={hourly.length ? hourly : [2, 4, 3, 6, 4, 5, 3, 7, 4, 5]} />
            </div>
            <div className="delta">{data.unhealthy_devices ?? 0} unhealthy</div>
          </div>
          <div className="panel nw-panel nw-metric-card nw-mini">
            <div className="nw-metric-label">New issues</div>
            <div className="nw-metric-big sm">
              {fmtCompact((issuesSeries as { y: number }[]).reduce((s, p) => s + p.y, 0))}
            </div>
            <div className="nw-chart-slot mini">
              <Sparkline
                data={(issuesSeries as { y: number }[]).map(s => s.y).length
                  ? (issuesSeries as { y: number }[]).map(s => s.y)
                  : [1, 2, 1, 3, 2, 4, 2]}
                color={COLORS.nwMax}
              />
            </div>
            <div className="delta">First-seen · 14 days</div>
          </div>
          <div className="panel nw-panel nw-metric-card nw-mini">
            <div className="nw-metric-label">Regressions</div>
            <div className="nw-metric-big sm">{data.regressions ?? 0}</div>
            <div className="nw-chart-slot mini">
              <Sparkline data={hourly.length ? hourly.map((v: number, i: number) => (i % 3 === 0 ? v : 0)) : [0, 1, 0, 2, 0, 1]} color={COLORS.pink} />
            </div>
            <div className="delta">Reopened after resolve</div>
          </div>
        </div>
      </div>

      <div className="grid-2">
        <div className="panel nw-panel nw-chart-panel">
          <div className="panel-head">
            <div>
              <h2>Event volume</h2>
              <p className="meta" style={{ margin: 0 }}>All events vs fatal · {range}</p>
            </div>
          </div>
          <div className="nw-chart-slot animate-chart-grow">
            <DualAreaChart a={eventsSeries} b={fatalSeries} labelA="All events" labelB="Fatal" height={200} />
          </div>
        </div>
        <div className="panel nw-panel nw-chart-panel">
          <div className="panel-head">
            <div>
              <h2>Ingest · last 24h</h2>
              <p className="meta" style={{ margin: 0 }}>Hourly density</p>
            </div>
          </div>
          <div className="nw-chart-slot animate-chart-grow">
            <AreaChart series={hourlySeries} color={COLORS.cyan} fillId="hourFill" height={200} showDots />
          </div>
        </div>
      </div>

      <div className="grid-3">
        <div className="panel nw-panel">
          <div className="panel-head"><h2>Severity</h2></div>
          <Donut items={data.by_severity || []} nameKey="severity" valueKey="count" />
        </div>
        <div className="panel nw-panel">
          <div className="panel-head"><h2>Architecture</h2></div>
          <HBarList items={data.by_architecture || []} nameKey="name" valueKey="count" />
        </div>
        <div className="panel nw-panel">
          <div className="panel-head"><h2>Pipelines</h2></div>
          <HBarList items={data.by_pipeline || []} nameKey="pipeline" valueKey="count" />
        </div>
      </div>

      {/* Exception feed */}
      <div className="panel nw-panel">
        <div className="panel-head">
          <div>
            <h2>Exceptions</h2>
            <p className="meta" style={{ margin: 0 }}>
              {openCount} open · impacting devices across releases
            </p>
          </div>
          <Link className="btn secondary" to="/issues">View all</Link>
        </div>
        <div className="nw-exception-list">
          {(data.top_issues || []).map((i: any, idx: number) => (
            <button type="button" key={i.id} className="nw-exception animate-fade-in-up" style={{ animationDelay: `${idx * 0.05}s` }} onClick={() => go('issues', i.id)}>
              <div className="nw-exception-id mono">{issueCode(i.id)}</div>
              <div className="nw-exception-body">
                <div className="nw-exception-type">
                  <Badge className={`sev-${i.severity || 'error'}`}>{(i.severity || 'error').toUpperCase()}</Badge>
                  <Badge className={sevClass(i.status)}>{i.status}</Badge>
                </div>
                <div className="nw-exception-title">{i.title}</div>
                <div className="meta">{fmtTime(i.last_seen)} · {i.event_count} events · {i.affected_devices} devices</div>
              </div>
              <div className="nw-exception-spark">
                <Sparkline data={[1, 2, 1, Number(i.event_count) || 1, 2, 3, Number(i.event_count) || 1]} color={i.severity === 'fatal' ? COLORS.red : COLORS.purple} />
              </div>
            </button>
          ))}
          {!(data.top_issues || []).length && (
            <div className="empty">No exceptions yet — point Relay at Trace and send a crash LEP</div>
          )}
        </div>
      </div>

      <div className="panel nw-panel nw-chart-panel">
        <div className="panel-head">
          <div>
            <h2>New issues</h2>
            <p className="meta" style={{ margin: 0 }}>First-seen count · 14 days</p>
          </div>
        </div>
        <div className="nw-chart-slot animate-chart-grow">
          <AreaChart series={issuesSeries} color={COLORS.pink} fillId="issFill" height={160} />
        </div>
      </div>
    </div>
  )
}

function IssuesList({ items, onOpen }: { items: any[]; onOpen: (id: string) => void }) {
  if (!items.length) return <div className="panel empty" data-testid="issues-empty">No issues match filters</div>
  return (
    <div className="panel nw-panel" style={{ padding: 0, overflow: 'hidden' }} data-testid="issues-list">
      <div className="nw-exception-list">
        {items.map((i: any, idx: number) => (
          <button type="button" key={i.id} className="nw-exception animate-fade-in-up" style={{ animationDelay: `${idx * 0.03}s` }} onClick={() => onOpen(i.id)}>
            <div className="nw-exception-id mono">{issueCode(i.id)}</div>
            <div className="nw-exception-body">
              <div className="nw-exception-type">
                <Badge className={`sev-${i.severity}`}>{i.severity}</Badge>
                <Badge className={sevClass(i.status)}>{i.status}</Badge>
                {i.regression_count > 0 && <Badge className="warn">reg ×{i.regression_count}</Badge>}
              </div>
              <div className="nw-exception-title">{i.title}</div>
              <div className="meta mono">{(i.fingerprint || '').slice(0, 20)} · {fmtTime(i.last_seen)}</div>
            </div>
            <div className="issue-stats" style={{ textAlign: 'right' }}>
              <strong style={{ display: 'block', color: 'hsl(0 0% 96%)' }}>{i.event_count}</strong>
              <span className="meta">{i.affected_devices} devices</span>
            </div>
          </button>
        ))}
      </div>
    </div>
  )
}

function IssueDetail({ data, onBack, reload }: { data: any; onBack: () => void; reload: () => void }) {
  const i = data.issue
  const latest = (data.events || [])[0]
  const frames = safeJSON(latest?.frames) || []
  const analysis = safeJSON(latest?.analysis) || {}
  const [suspects, setSuspects] = useState<any[]>([])
  const [replay, setReplay] = useState<any[]>([])
  const [replayIdx, setReplayIdx] = useState(0)

  useEffect(() => {
    let cancelled = false
    ;(async () => {
      try {
        const [s, r] = await Promise.all([
          api('/api/issues/' + i.id + '/suspect-commits').catch(() => ({ items: [] })),
          api('/api/issues/' + i.id + '/replay').catch(() => ({ frames: [] })),
        ])
        if (!cancelled) {
          setSuspects(s.items || [])
          setReplay(r.frames || [])
        }
      } catch { /* */ }
    })()
    return () => { cancelled = true }
  }, [i.id])

  const breadcrumbs = replay.length
    ? replay
    : Array.isArray(analysis.breadcrumbs) ? analysis.breadcrumbs
      : Array.isArray(safeJSON(latest?.decoded)?.breadcrumbs) ? safeJSON(latest?.decoded).breadcrumbs
        : (data.activity || []).map((a: any) => ({
          type: a.action, message: a.body || a.action, timestamp: a.created_at, category: 'activity',
        }))

  return (
    <div data-testid="issue-detail">
      <Button type="button" variant="secondary" onClick={onBack} style={{ marginBottom: 12 }}>← Issues</Button>
      <div className="panel nw-panel" style={{ marginBottom: 12 }}>
        <div className="row gap" style={{ justifyContent: 'space-between', alignItems: 'flex-start' }}>
          <div>
            <div className="meta mono" style={{ marginBottom: 6 }}>{issueCode(i.id)}</div>
            <h2 style={{ marginTop: 0 }}>{i.title}</h2>
          </div>
          <div className="row gap">
            <Badge className={sevClass(i.status)}>{i.status}</Badge>
            <Badge className={`sev-${i.severity}`}>{i.severity}</Badge>
            {i.regression_count > 0 && <Badge className="warn">reg ×{i.regression_count}</Badge>}
          </div>
        </div>
        <div className="issue-summary">
          <strong>Summary</strong>
          <p style={{ margin: '0.35rem 0 0' }}>{i.probable_cause || analysis.summary || 'No probable cause yet — waiting for symbolicated frames.'}</p>
        </div>
        <div className="kv" style={{ marginTop: 12 }}>
          <div className="kv-row"><span className="k">Owner</span><span className="v">{i.assignee_email || i.assignee || 'Unassigned'}</span></div>
          <div className="kv-row"><span className="k">Fingerprint</span><span className="v mono">{i.fingerprint}</span></div>
          <div className="kv-row"><span className="k">Impact</span><span className="v">{i.event_count} events · {i.affected_devices} devices</span></div>
          <div className="kv-row"><span className="k">First / last</span><span className="v">{fmtTime(i.first_seen)} → {fmtTime(i.last_seen)}</span></div>
          {analysis.unwind_method && <div className="kv-row"><span className="k">Unwind</span><span className="v mono">{analysis.unwind_method}</span></div>}
        </div>
        <div className="row gap" style={{ marginTop: 12 }}>
          {['open', 'investigating', 'resolved', 'ignored'].map(st => (
            <Button key={st} type="button" variant="secondary" onClick={async () => {
              await api('/api/issues/' + i.id + '/status', { method: 'POST', body: { status: st } })
              reload()
            }}>{st}</Button>
          ))}
          <Button type="button" variant="secondary" onClick={async () => {
            const email = prompt('Assign to (email)')
            if (!email) return
            await api('/api/issues/' + i.id + '/assign', { method: 'POST', body: { email } })
            reload()
          }}>Assign</Button>
        </div>
      </div>
      <div className="detail-grid">
        <div>
          <div className="panel nw-panel" style={{ marginBottom: 12 }}>
            <h3>Stack (latest event)</h3>
            <StackFrames frames={Array.isArray(frames) ? frames : []} />
          </div>
          <div className="panel nw-panel" style={{ marginBottom: 12 }}>
            <div className="panel-head">
              <h3 style={{ margin: 0 }}>Breadcrumb replay</h3>
              {breadcrumbs.length > 0 && (
                <div className="row gap">
                  <Button type="button" variant="secondary" disabled={replayIdx <= 0} onClick={() => setReplayIdx(x => Math.max(0, x - 1))}>Prev</Button>
                  <span className="meta">{Math.min(replayIdx + 1, breadcrumbs.length)} / {breadcrumbs.length}</span>
                  <Button type="button" variant="secondary" disabled={replayIdx >= breadcrumbs.length - 1} onClick={() => setReplayIdx(x => Math.min(breadcrumbs.length - 1, x + 1))}>Next</Button>
                </div>
              )}
            </div>
            {breadcrumbs.length ? (
              <>
                <div className="issue-summary" style={{ marginBottom: 12 }}>
                  <strong>Frame {replayIdx + 1}</strong>
                  <p style={{ margin: '0.35rem 0 0' }}>
                    {breadcrumbs[replayIdx]?.message || breadcrumbs[replayIdx]?.body || breadcrumbs[replayIdx]?.type || '—'}
                  </p>
                  <div className="meta">{fmtTime(breadcrumbs[replayIdx]?.timestamp || breadcrumbs[replayIdx]?.ts)} · {breadcrumbs[replayIdx]?.category || breadcrumbs[replayIdx]?.type || 'event'}</div>
                </div>
                <div className="timeline">
                  {breadcrumbs.slice(0, 40).map((b: any, idx: number) => (
                    <button type="button" key={idx} className={'timeline-item' + (idx === replayIdx ? ' active' : '')} onClick={() => setReplayIdx(idx)} style={{ width: '100%', textAlign: 'left', background: idx === replayIdx ? 'rgba(139, 92, 246, 0.06)' : 'transparent', border: 0, color: 'inherit', font: 'inherit', cursor: 'pointer' }}>
                      <div className="timeline-dot" />
                      <div>
                        <div className="meta">{fmtTime(b.timestamp || b.ts || b.created_at)} · <span className="tag">{b.category || b.type || 'event'}</span></div>
                        <div>{b.message || b.body || b.type || JSON.stringify(b).slice(0, 120)}</div>
                      </div>
                    </button>
                  ))}
                </div>
              </>
            ) : <div className="meta">No breadcrumbs on latest event</div>}
          </div>
          <div className="panel nw-panel">
            <h3>Events</h3>
            <ul style={{ paddingLeft: '1.1rem' }}>
              {(data.events || []).map((e: any) => (
                <li key={e.id}><Link className="mono" to={`/events/${e.id}`}>{e.event_id}</Link>
                  <span className={`badge sev-${e.severity}`} style={{ marginLeft: 8 }}>{e.severity}</span>
                </li>
              ))}
            </ul>
          </div>
        </div>
        <div>
          <div className="panel nw-panel" style={{ marginBottom: 12 }}>
            <h3>Suspect commits</h3>
            {suspects.length ? (
              <ul style={{ paddingLeft: '1.1rem', margin: 0 }}>
                {suspects.map((c: any, idx: number) => (
                  <li key={idx} style={{ marginBottom: 8 }}>
                    <span className="mono">{(c.sha || '').slice(0, 8)}</span>
                    <span className="meta"> · {c.release || c.build_id}</span>
                    <div>{c.message || 'release head'}</div>
                    {c.author && <div className="meta">{c.author}</div>}
                  </li>
                ))}
              </ul>
            ) : <div className="meta">No commits linked — POST /api/releases/:id/commits from CI</div>}
          </div>
          <div className="panel nw-panel" style={{ marginBottom: 12 }}>
            <h3>Activity</h3>
            <ul style={{ paddingLeft: '1.1rem', margin: 0 }}>
              {(data.activity || []).map((a: any) => (
                <li key={a.id} className="meta" style={{ marginBottom: 6 }}>{fmtTime(a.created_at)} · <strong>{a.action}</strong> {a.body}</li>
              ))}
              {!(data.activity || []).length && <li className="meta">No activity</li>}
            </ul>
          </div>
          <div className="panel nw-panel">
            <h3>Comments</h3>
            {(data.comments || []).map((c: any) => (
              <div key={c.id} style={{ marginBottom: 8 }}><strong>{c.author || 'anon'}</strong><div className="meta">{c.body}</div></div>
            ))}
            <Button type="button" onClick={async () => {
              const body = prompt('Comment')
              if (!body) return
              await api('/api/issues/' + i.id + '/comments', { method: 'POST', body: { body } })
              reload()
            }}>Add comment</Button>
          </div>
        </div>
      </div>
    </div>
  )
}

function Detail({ view, data, onBack, onNavigate, reload }: {
  view: View; data: any; onBack: () => void; onNavigate: (v: View, id?: string) => void; reload: () => void
}) {
  if (view === 'issues' && data.issue) {
    return <IssueDetail data={data} onBack={onBack} reload={reload} />
  }
  if (view === 'events') {
    const e = data.event || data
    const analysis = safeJSON(e.analysis) || {}
    const frames = safeJSON(e.frames) || []
    const decoded = safeJSON(e.decoded) || {}
    return (
      <div data-testid="event-detail">
        <Button type="button" variant="secondary" onClick={onBack} style={{ marginBottom: 12 }}>← Events</Button>
        <div className="panel" style={{ marginBottom: 12 }}>
          <h2 className="mono" style={{ marginTop: 0 }}>{e.event_id}</h2>
          <div className="row gap">
            <Badge>{e.state}</Badge>
            <Badge className={`sev-${e.severity}`}>{e.severity}</Badge>
            <span className="tag">{e.pipeline || 'issue'}</span>
            {analysis.architecture_name && <span className="tag">{analysis.architecture_name}</span>}
          </div>
          {analysis.summary && <p style={{ margin: '.75rem 0' }}>{analysis.summary}</p>}
          <div className="row gap">
            <a className="btn secondary" href={'/api/events/' + e.id + '/raw'}>Download raw</a>
            <Button type="button" onClick={async () => {
              await api('/api/events/' + e.id + '/reprocess', { method: 'POST' })
              alert('Reprocess queued'); reload()
            }}>Reprocess</Button>
          </div>
        </div>
        <div className="detail-grid">
          <div className="panel">
            <h3>Stack frames {analysis.unwind_method && <span className="meta">· {analysis.unwind_method}</span>}</h3>
            <StackFrames frames={Array.isArray(frames) ? frames : []} />
            {analysis.fault_details && (
              <>
                <h3 style={{ marginTop: '1rem' }}>Fault details</h3>
                <div className="kv">
                  {Object.entries(analysis.fault_details).filter(([, v]) => typeof v !== 'object').map(([k, v]) => (
                    <div key={k} className="kv-row"><span className="k">{k}</span><span className="v mono">{String(v)}</span></div>
                  ))}
                </div>
              </>
            )}
            {analysis.stacked_frame && (
              <>
                <h3 style={{ marginTop: '1rem' }}>Stacked frame</h3>
                <pre>{JSON.stringify(analysis.stacked_frame, null, 2)}</pre>
              </>
            )}
            {analysis.causes?.length > 0 && (
              <>
                <h3 style={{ marginTop: '1rem' }}>Causes</h3>
                <ul>{analysis.causes.map((c: string, i: number) => <li key={i}>{c}</li>)}</ul>
              </>
            )}
          </div>
          <div>
            <div className="panel" style={{ marginBottom: 12 }}>
              <h3>Decoded</h3>
              <pre>{JSON.stringify(decoded, null, 2)}</pre>
            </div>
            <div className="panel" style={{ marginBottom: 12 }}>
              <h3>Hex</h3>
              <pre className="hex">{hexDump(JSON.stringify(decoded).slice(0, 256))}</pre>
            </div>
            <div className="panel">
              <h3>History</h3>
              <ul style={{ margin: 0, paddingLeft: '1.1rem' }}>
                {(data.history || []).map((h: any, idx: number) => (
                  <li key={idx} className="meta">{fmtTime(h.created_at)} · {h.from || '∅'} → {h.to}</li>
                ))}
              </ul>
            </div>
          </div>
        </div>
      </div>
    )
  }
  if (view === 'devices' && data.device) {
    const d = data.device
    return (
      <div>
        <Button type="button" variant="secondary" onClick={onBack} style={{ marginBottom: 12 }}>← Devices</Button>
        <div className="panel">
          <h2 className="mono" style={{ marginTop: 0 }}>{d.device_id}</h2>
          <div className="row gap">
            <Badge className={sevClass(d.status)}>{d.status}</Badge>
            <span className="meta">{d.product} · rev {d.hardware_revision || '—'}</span>
          </div>
          <div className="kv" style={{ marginTop: 12 }}>
            <div className="kv-row"><span className="k">Firmware</span><span className="v mono">{d.firmware_version || '—'}</span></div>
            <div className="kv-row"><span className="k">Build</span><span className="v mono">{d.build_id || '—'}</span></div>
            <div className="kv-row"><span className="k">Last seen</span><span className="v">{fmtTime(d.last_seen)}</span></div>
          </div>
          <h3>Firmware history</h3>
          <ul>{(data.firmware_history || []).map((h: any, i: number) => (
            <li key={i} className="meta">{h.firmware_version} · {h.build_id} · {fmtTime(h.last_seen)}</li>
          ))}</ul>
          <h3>Recent events</h3>
          <ul>{(data.events || []).map((e: any) => (
            <li key={e.id}><Link className="mono" to={`/events/${e.id}`}>{e.event_id}</Link></li>
          ))}</ul>
        </div>
      </div>
    )
  }
  if (view === 'releases' && data.release) {
    return (
      <div>
        <Button type="button" variant="secondary" onClick={onBack} style={{ marginBottom: 12 }}>← Releases</Button>
        <div className="panel">
          <h2 style={{ marginTop: 0 }}>{data.release.version}</h2>
          <p className="mono meta">{data.release.build_id} · {data.release.git_commit || 'no commit'}</p>
          {data.stats && (
            <div className="cards" style={{ marginTop: 16 }}>
              <div className="card ok"><div className="label">Crash-free</div><div className="value">{Number(data.stats.crash_free_rate).toFixed(1)}%</div></div>
              <div className="card"><div className="label">Sessions</div><div className="value">{data.stats.sessions}</div></div>
              <div className="card fatal"><div className="label">Crash sessions</div><div className="value">{data.stats.crash_sessions}</div></div>
            </div>
          )}
        </div>
      </div>
    )
  }
  return (
    <div className="panel">
      <Button type="button" variant="secondary" onClick={onBack}>← Back</Button>
      <pre>{JSON.stringify(data, null, 2)}</pre>
    </div>
  )
}

function StackFrames({ frames }: { frames: any[] }) {
  if (!frames?.length) return <p className="meta">No frames</p>
  return (
    <div className="stack">
      {frames.map((f, i) => (
        <div key={i} className="stack-frame">
          <span className="n">{i}</span>
          <span>
            {f.function ? <span className="fn">{f.function}</span> : <span className="meta">unknown</span>}
            {f.file && <span className="meta"> · {f.file}{f.line ? `:${f.line}` : ''}</span>}
          </span>
          <span className="addr">0x{(f.address || 0).toString(16)}</span>
        </div>
      ))}
    </div>
  )
}

function fmtTime(v: any) {
  if (!v) return '—'
  try { return new Date(v).toLocaleString() } catch { return String(v) }
}
function safeJSON(v: any) {
  if (typeof v === 'string') { try { return JSON.parse(v) } catch { return v } }
  return v
}
function hexDump(s: string) {
  const bytes = Array.from(s).map(c => c.charCodeAt(0))
  const lines: string[] = []
  for (let i = 0; i < bytes.length; i += 16) {
    const chunk = bytes.slice(i, i + 16)
    const hex = chunk.map(b => b.toString(16).padStart(2, '0')).join(' ')
    const asc = chunk.map(b => (b >= 32 && b < 127 ? String.fromCharCode(b) : '.')).join('')
    lines.push(i.toString(16).padStart(4, '0') + '  ' + hex.padEnd(48) + '  ' + asc)
  }
  return lines.join('\n')
}
