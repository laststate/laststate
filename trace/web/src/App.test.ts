// App-level tests for Trace frontend
// Run with: npx tsx src/App.test.ts

import { isView } from './nav'

// Test that all views in nav.ts are valid
const viewsToTest = [
  'overview', 'issues', 'events', 'devices', 'releases',
  'artifacts', 'alerts', 'channels', 'relays', 'projects', 'hardware',
  'boots', 'dead', 'audit', 'settings', 'analytics', 'compliance',
  'billing', 'blog', 'faq', 'docs', 'pricing',
  'fleet-health', 'device-dna', 'pr', 'chaos', 'lep-explorer',
  'memorial-wall', 'public-api', 'anomaly',
]

for (const view of viewsToTest) {
  if (!isView(view)) {
    throw new Error(`View ${view} is not valid: isView returned false`)
  }
}

// Test that invalid views are rejected
const invalidViews = ['xyz', '', ' ', 'overview-extra', 'issues-detail', 'events-list']
for (const view of invalidViews) {
  if (isView(view)) {
    throw new Error(`View ${view} should not be valid: isView returned true`)
  }
}

// Test navigation structure
const { NAV } = await import('./nav')

// Check that all NAV entries have required fields
for (const entry of NAV) {
  if (!entry.id) throw new Error('NAV entry missing id')
  if (!entry.label) throw new Error(`NAV entry ${entry.id} missing label`)
  if (!isView(entry.id)) throw new Error(`NAV entry ${entry.id} is not a valid view`)
}

// Check that required views are present
const requiredViews = ['overview', 'issues', 'events', 'devices', 'settings']
for (const rv of requiredViews) {
  const found = NAV.find(n => n.id === rv)
  if (!found) throw new Error(`Required view ${rv} not found in NAV`)
}

// Check that new feature views are present
const featureViews = ['fleet-health', 'device-dna', 'chaos', 'lep-explorer', 'memorial-wall', 'public-api', 'anomaly']
for (const fv of featureViews) {
  const found = NAV.find(n => n.id === fv)
  if (!found) throw new Error(`Feature view ${fv} not found in NAV`)
}

// Check that sections are defined
const sections = [...new Set(NAV.filter(n => n.section).map(n => n.section))]
if (!sections.includes('Monitor')) throw new Error('Missing Monitor section')
if (!sections.includes('Debug')) throw new Error('Missing Debug section')
if (!sections.includes('Test')) throw new Error('Missing Test section')
if (!sections.includes('Integrate')) throw new Error('Missing Integrate section')
if (!sections.includes('Manage')) throw new Error('Missing Manage section')
if (!sections.includes('Community')) throw new Error('Missing Community section')

console.log('App.test.ts: all tests passed')
