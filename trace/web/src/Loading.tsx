/** Brand assets are served from /assets/brand (web/public/assets/brand). */

export const BRAND_LOGO = '/assets/brand/logo.svg'
export const BRAND_LOGO_LARGE = '/assets/brand/logo-large.svg'
export const BRAND_LOADING = '/assets/brand/loading.gif'

/** Full-screen boot splash: loading GIF only until the first payload is ready. */
export function BootSplash({ label = 'Loading Trace…' }: { label?: string }) {
  return (
    <div className="boot-splash" role="status" aria-live="polite" aria-busy="true" data-testid="boot-splash">
      <div className="boot-splash-inner">
        <img
          src={BRAND_LOADING}
          alt="Last State"
          className="boot-splash-gif"
          width={160}
          height={160}
          decoding="async"
        />
        <span className="boot-splash-label">{label}</span>
      </div>
    </div>
  )
}

export function Loading({ label = 'Loading…' }: { label?: string }) {
  return (
    <div className="loading-state" role="status" aria-live="polite">
      <img src={BRAND_LOADING} alt="" className="loading-gif" width={72} height={72} aria-hidden />
      <span className="meta">{label}</span>
    </div>
  )
}

export function SkeletonCards({ n = 4 }: { n?: number }) {
  return (
    <div className="cards">
      {Array.from({ length: n }).map((_, i) => (
        <div key={i} className="card skeleton-card">
          <div className="sk sk-label" />
          <div className="sk sk-value" />
        </div>
      ))}
    </div>
  )
}

export function BrandLogo({ size = 28, large = false }: { size?: number; large?: boolean }) {
  return (
    <img
      src={large ? BRAND_LOGO_LARGE : BRAND_LOGO}
      alt="Last State"
      className="brand-logo-img"
      width={size}
      height={size}
      decoding="async"
    />
  )
}
