export type View =
  | 'overview' | 'issues' | 'events' | 'devices' | 'releases'
  | 'artifacts' | 'alerts' | 'channels' | 'relays' | 'projects' | 'hardware'
  | 'boots' | 'dead' | 'audit' | 'settings' | 'analytics' | 'compliance'
  | 'billing' | 'blog' | 'faq' | 'docs' | 'pricing'
  | 'fleet-health' | 'device-dna' | 'pr' | 'chaos' | 'lep-explorer'
  | 'memorial-wall' | 'public-api' | 'anomaly'

export const NAV: { id: View; label: string; section?: string }[] = [
  { id: 'overview', label: 'Overview', section: 'Monitor' },
  { id: 'issues', label: 'Issues' },
  { id: 'events', label: 'Events' },
  { id: 'devices', label: 'Devices' },
  { id: 'releases', label: 'Releases' },
  { id: 'artifacts', label: 'Artifacts', section: 'Debug' },
  { id: 'hardware', label: 'Hardware' },
  { id: 'boots', label: 'Boots' },
  { id: 'fleet-health', label: 'Fleet Health' },
  { id: 'device-dna', label: 'Device DNA' },
  { id: 'pr', label: 'PRs' },
  { id: 'chaos', label: 'Chaos', section: 'Test' },
  { id: 'anomaly', label: 'Anomaly' },
  { id: 'alerts', label: 'Alerts', section: 'Integrate' },
  { id: 'channels', label: 'Channels' },
  { id: 'relays', label: 'Relays' },
  { id: 'lep-explorer', label: 'LEP Explorer' },
  { id: 'projects', label: 'Projects', section: 'Manage' },
  { id: 'billing', label: 'Billing', section: 'Manage' },
  { id: 'dead', label: 'Dead jobs' },
  { id: 'memorial-wall', label: 'Memorial Wall', section: 'Community' },
  { id: 'public-api', label: 'Public API', section: 'Community' },
  { id: 'blog', label: 'Blog', section: 'Community' },
  { id: 'faq', label: 'FAQ', section: 'Community' },
  { id: 'docs', label: 'Docs', section: 'Community' },
  { id: 'pricing', label: 'Pricing', section: 'Community' },
  { id: 'audit', label: 'Audit' },
  { id: 'analytics', label: 'Analytics' },
  { id: 'compliance', label: 'Compliance' },
  { id: 'settings', label: 'Settings' },
]

export function isView(s: string | undefined): s is View {
  return !!s && NAV.some(n => n.id === s)
}
