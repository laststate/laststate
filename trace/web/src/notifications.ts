// Web Browser Notifications and Audio Alerts Manager for LastState Trace

export type NotificationPermissionState = 'granted' | 'denied' | 'default' | 'unsupported'

export interface SystemAlert {
  id: string
  title: string
  body: string
  severity: 'fatal' | 'error' | 'warning' | 'info'
  timestamp: number
  url?: string
  read?: boolean
}

const STORAGE_KEY_ENABLED = 'trace_notifications_enabled'
const STORAGE_KEY_SOUND = 'trace_notifications_sound'
const STORAGE_KEY_DISMISSED = 'trace_notifications_prompt_dismissed'
const STORAGE_KEY_HISTORY = 'trace_notifications_history'
const STORAGE_KEY_LAST_SEEN_EVENT = 'trace_notifications_last_seen_event'

export function isNotificationSupported(): boolean {
  return typeof window !== 'undefined' && 'Notification' in window
}

export function getNotificationPermission(): NotificationPermissionState {
  if (!isNotificationSupported()) return 'unsupported'
  return Notification.permission
}

export async function requestNotificationPermission(): Promise<boolean> {
  if (!isNotificationSupported()) return false
  try {
    const result = await Notification.requestPermission()
    if (result === 'granted') {
      setNotificationsEnabled(true)
      notifyListeners()
      return true
    }
    return false
  } catch (err) {
    console.error('Failed to request notification permission', err)
    return false
  }
}

export function isNotificationsEnabled(): boolean {
  if (typeof window === 'undefined') return false
  const raw = localStorage.getItem(STORAGE_KEY_ENABLED)
  if (raw === null) {
    // Default to true if browser permission is already granted
    return getNotificationPermission() === 'granted'
  }
  return raw === 'true' && getNotificationPermission() === 'granted'
}

export function setNotificationsEnabled(enabled: boolean): void {
  if (typeof window === 'undefined') return
  localStorage.setItem(STORAGE_KEY_ENABLED, String(enabled))
  notifyListeners()
}

export function isSoundEnabled(): boolean {
  if (typeof window === 'undefined') return true
  const raw = localStorage.getItem(STORAGE_KEY_SOUND)
  return raw === null ? true : raw === 'true'
}

export function setSoundEnabled(enabled: boolean): void {
  if (typeof window === 'undefined') return
  localStorage.setItem(STORAGE_KEY_SOUND, String(enabled))
  notifyListeners()
}

export function isPromptDismissed(): boolean {
  if (typeof window === 'undefined') return false
  return localStorage.getItem(STORAGE_KEY_DISMISSED) === 'true'
}

export function setPromptDismissed(dismissed: boolean): void {
  if (typeof window === 'undefined') return
  localStorage.setItem(STORAGE_KEY_DISMISSED, String(dismissed))
  notifyListeners()
}

export function getAlertHistory(): SystemAlert[] {
  if (typeof window === 'undefined') return []
  try {
    const raw = localStorage.getItem(STORAGE_KEY_HISTORY)
    return raw ? JSON.parse(raw) : []
  } catch {
    return []
  }
}

export function saveAlertHistory(alerts: SystemAlert[]): void {
  if (typeof window === 'undefined') return
  try {
    localStorage.setItem(STORAGE_KEY_HISTORY, JSON.stringify(alerts.slice(0, 50)))
    notifyListeners()
  } catch {}
}

export function addAlert(alert: Omit<SystemAlert, 'id' | 'timestamp' | 'read'>): SystemAlert {
  const newAlert: SystemAlert = {
    ...alert,
    id: Math.random().toString(36).slice(2, 10) + Date.now().toString(36),
    timestamp: Date.now(),
    read: false,
  }
  const history = getAlertHistory()
  saveAlertHistory([newAlert, ...history])
  return newAlert
}

export function markAllAlertsRead(): void {
  const history = getAlertHistory().map(a => ({ ...a, read: true }))
  saveAlertHistory(history)
}

export function clearAlertHistory(): void {
  saveAlertHistory([])
}

// Synthesize pleasant sound chimes using Web Audio API
export function playChime(type: 'fatal' | 'error' | 'warning' | 'info' | 'test' = 'info'): void {
  if (!isSoundEnabled() || typeof window === 'undefined') return
  try {
    const AudioCtx = window.AudioContext || (window as any).webkitAudioContext
    if (!AudioCtx) return
    const ctx = new AudioCtx()

    const now = ctx.currentTime
    const osc = ctx.createOscillator()
    const gain = ctx.createGain()

    osc.connect(gain)
    gain.connect(ctx.destination)

    if (type === 'fatal' || type === 'error') {
      // 2-tone urgent ping (high to mid)
      osc.type = 'sawtooth'
      osc.frequency.setValueAtTime(880, now) // A5
      osc.frequency.exponentialRampToValueAtTime(587.33, now + 0.15) // D5
      gain.gain.setValueAtTime(0.2, now)
      gain.gain.exponentialRampToValueAtTime(0.001, now + 0.35)
      osc.start(now)
      osc.stop(now + 0.35)
    } else if (type === 'warning') {
      // Warm alert tone
      osc.type = 'triangle'
      osc.frequency.setValueAtTime(659.25, now) // E5
      osc.frequency.setValueAtTime(523.25, now + 0.1) // C5
      gain.gain.setValueAtTime(0.2, now)
      gain.gain.exponentialRampToValueAtTime(0.001, now + 0.3)
      osc.start(now)
      osc.stop(now + 0.3)
    } else {
      // Soft modern glass chime
      osc.type = 'sine'
      osc.frequency.setValueAtTime(1046.50, now) // C6
      osc.frequency.exponentialRampToValueAtTime(1318.51, now + 0.08) // E6
      gain.gain.setValueAtTime(0.15, now)
      gain.gain.exponentialRampToValueAtTime(0.001, now + 0.28)
      osc.start(now)
      osc.stop(now + 0.28)
    }
  } catch (err) {
    // AudioContext blocked by browser policy until user gesture
  }
}

export function sendBrowserNotification(options: {
  title: string
  body: string
  severity?: 'fatal' | 'error' | 'warning' | 'info'
  url?: string
  tag?: string
}): void {
  const severity = options.severity || 'info'
  
  // 1. Always record in alert history
  addAlert({
    title: options.title,
    body: options.body,
    severity,
    url: options.url,
  })

  // 2. Play sound if configured
  playChime(severity)

  // 3. Dispatch native browser notification if enabled
  if (isNotificationSupported() && getNotificationPermission() === 'granted' && isNotificationsEnabled()) {
    try {
      const notif = new Notification(options.title, {
        body: options.body,
        icon: '/assets/brand/logo.svg',
        badge: '/assets/brand/logo.svg',
        tag: options.tag || options.title,
      })

      notif.onclick = () => {
        window.focus()
        if (options.url) {
          if (options.url.startsWith('http')) {
            window.location.href = options.url
          } else {
            // Local SPA path
            const path = options.url.startsWith('/') ? options.url : `/${options.url}`
            window.history.pushState(null, '', path)
            window.dispatchEvent(new PopStateEvent('popstate'))
          }
        }
        notif.close()
      }
    } catch (err) {
      console.warn('Native notification failed:', err)
    }
  }
}

export function sendTestNotification(): void {
  sendBrowserNotification({
    title: 'Test Alert — LastState Trace',
    body: 'Browser notifications and sound alerts are configured and working correctly!',
    severity: 'info',
    url: '/overview',
    tag: 'test-notification-' + Date.now(),
  })
}

// React Listener Registry for real-time reactivity
type Listener = () => void
const listeners = new Set<Listener>()

function notifyListeners() {
  listeners.forEach(l => {
    try { l() } catch {}
  })
}

export function subscribeToNotifications(listener: Listener): () => void {
  listeners.add(listener)
  return () => {
    listeners.delete(listener)
  }
}
