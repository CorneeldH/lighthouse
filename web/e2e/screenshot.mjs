// Screenshot of the running UI for the README and the DoD.
// Usage: node e2e/screenshot.mjs [url] [out] [tab]
// tab is controller (the default) or telemetry; telemetry opens #telemetry and waits for its regions.
import { mkdirSync } from 'node:fs'
import { dirname } from 'node:path'
import { chromium } from '@playwright/test'

const url = process.argv[2] ?? 'http://127.0.0.1:8127'
const out = process.argv[3] ?? '../docs/images/ui-m2.png'
const tab = process.argv[4] ?? 'controller'
const REGIONS = {
  controller: ['Usage', 'Running runs', 'Open questions', 'Returned'],
  telemetry: ['Components', 'Timeline', 'Trajectories', 'Coverage'],
}
if (!(tab in REGIONS)) {
  console.error(`unknown tab ${tab}: want controller or telemetry`)
  process.exit(2)
}
const target = tab === 'controller' ? url : `${url.replace(/#.*$/, '')}#${tab}`

const browser = await chromium.launch()
try {
  const page = await browser.newPage({ viewport: { width: 1280, height: 800 } })
  await page.goto(target, { timeout: 30_000 })
  for (const name of REGIONS[tab]) {
    await page.getByRole('region', { name, exact: true }).waitFor({ state: 'visible', timeout: 30_000 })
  }
  await page.getByText('Loading…').first().waitFor({ state: 'detached', timeout: 30_000 }).catch(() => {})
  if ((await page.getByText('Loading…').count()) > 0) throw new Error('still loading')
  mkdirSync(dirname(out), { recursive: true })
  await page.screenshot({ path: out, fullPage: true })
  console.log(`wrote ${out}`)
} catch (err) {
  console.error(`screenshot failed for ${target}: ${err instanceof Error ? err.message : err}`)
  process.exitCode = 1
} finally {
  await browser.close()
}
