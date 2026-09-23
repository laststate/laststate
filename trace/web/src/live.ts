import { useEffect, useRef, useState } from 'react'

export type LiveStatus = 'connecting' | 'live' | 'polling'

interface LiveStreamOptions {
  onOverview: (data: any) => void
}

/**
 * Subscribes to the backend SSE feed (/api/stream) for real-time overview
 * updates. Falls back gracefully: callers keep their polling loop and this
 * hook reports status so the UI can indicate the active transport.
 * EventSource reconnects on its own; we cap the retry cadence so a broken
 * backend is not hammered (after repeated failures it retries once a minute).
 */
export function useLiveStream({ onOverview }: LiveStreamOptions) {
  const [status, setStatus] = useState<LiveStatus>('connecting')
  const cbRef = useRef(onOverview)
  cbRef.current = onOverview

  useEffect(() => {
    if (typeof EventSource === 'undefined') {
      setStatus('polling')
      return
    }
    let stopped = false
    let es: EventSource | null = null
    let retryTimer: number | undefined
    let failures = 0

    function connect() {
      if (stopped) return
      es = new EventSource('/api/stream')
      es.addEventListener('hello', () => {
        if (stopped) return
        failures = 0
        setStatus('live')
      })
      es.addEventListener('overview', (ev: MessageEvent) => {
        if (stopped) return
        try {
          cbRef.current(JSON.parse(ev.data))
        } catch { /* malformed frame — skip */ }
      })
      es.onerror = () => {
        if (stopped) return
        setStatus('polling')
        es?.close()
        failures++
        const delay = failures <= 3 ? 2000 * failures : 60_000
        retryTimer = window.setTimeout(connect, delay)
      }
    }
    connect()
    return () => {
      stopped = true
      if (retryTimer) window.clearTimeout(retryTimer)
      es?.close()
    }
  }, [])

  return status
}
