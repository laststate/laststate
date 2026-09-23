import React from 'react'
import ReactDOM from 'react-dom/client'
import { BrowserRouter } from 'react-router-dom'
import App from './App'
import { ErrorBoundary } from './ErrorBoundary'
import './styles.css'

// OIDC / legacy hash token
if (location.hash.startsWith('#token=')) {
  const t = decodeURIComponent(location.hash.slice(7).split('&')[0])
  localStorage.setItem('trace_session', t)
  history.replaceState(null, '', location.pathname + location.search)
}

// Migrate old hash routes → path routes once
if (location.hash.startsWith('#/')) {
  const path = location.hash.slice(1)
  history.replaceState(null, '', path)
}

ReactDOM.createRoot(document.getElementById('root')!).render(
  <React.StrictMode>
    <ErrorBoundary>
      <BrowserRouter>
        <App />
      </BrowserRouter>
    </ErrorBoundary>
  </React.StrictMode>,
)
