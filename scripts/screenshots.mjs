// README screenshots and a browser smoke test, against a demo instance (DEMO_DATA=true seeds five decks and four months of studying):
//
//   DATA_DIR=/tmp/learn-demo DEMO_DATA=true ADMIN_EMAIL=demo@example.com ADMIN_PASSWORD=demodemo1 ADMIN_NAME=Maya PORT=18420 ./pocket-learn
//   docker run -d --name plchrome --network host --shm-size=1g zenika/alpine-chrome:124 --no-sandbox --headless --disable-gpu --disable-dev-shm-usage \
//     --hide-scrollbars --remote-debugging-port=19271 --remote-debugging-address=127.0.0.1 --remote-allow-origins=* about:blank
//   CDP=http://127.0.0.1:19271 BASE=http://127.0.0.1:18420 node scripts/screenshots.mjs   # any Chrome with remote debugging
//   SHOTS=0 … node scripts/screenshots.mjs                                                  # checks only
// playwright-core comes from web/node_modules, or set PW_FROM to a package.json next to an install of it.
import { createRequire } from 'node:module'
import assert from 'node:assert/strict'

const require = createRequire(process.env.PW_FROM || new URL('../web/package.json', import.meta.url))
let pw
try { pw = require('playwright') } catch { pw = require('playwright-core') }
const { chromium } = pw
const base = process.env.BASE || 'http://127.0.0.1:18420'
const shots = process.env.SHOTS !== '0'
const only = process.env.ONLY ? new Set(process.env.ONLY.split(',')) : null
const dir = process.env.OUT || new URL('../docs/screenshots/', import.meta.url).pathname
const browser = process.env.CDP ? await chromium.connectOverCDP(process.env.CDP) : await chromium.launch()
const errors = []

async function session({ phone = false, dark = false } = {}) {
  const ctx = await browser.newContext({
    viewport: phone ? { width: 390, height: 844 } : { width: 1440, height: 900 },
    deviceScaleFactor: phone ? 3 : 2, isMobile: phone, hasTouch: phone, colorScheme: dark ? 'dark' : 'light',
    userAgent: phone ? 'Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1' : undefined,
  })
  const page = await ctx.newPage()
  page.on('pageerror', e => errors.push(`pageerror: ${e}`))
  page.on('console', m => { if (m.type() === 'error' && !/Failed to load resource/.test(m.text())) errors.push(m.text()) })
  await page.goto(base + '/login')
  const res = await page.evaluate(() => fetch('/api/v1/auth/login', { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify({ email: 'demo@example.com', password: 'demodemo1' }) }).then(r => r.status))
  assert.equal(res, 200, 'demo sign-in')
  return { ctx, page }
}
async function go(page, path, wait = 1200) {
  await page.goto(base + path, { waitUntil: 'networkidle' })
  // headless Chrome freezes CSS fade-ins: show everything at once
  await page.addStyleTag({ content: '*, *::before, *::after { transition: none !important; animation: none !important; }' })
  await page.evaluate(() => document.fonts.ready)
  await page.waitForTimeout(wait)
}
async function shot(page, name) { if (shots && (!only || only.has(name))) await page.screenshot({ path: dir + name + '.png' }) }

// ---- desktop ----
{
  const { ctx, page } = await session()
  await go(page, '/')
  assert.ok((await page.content()).includes('to study'), 'Today shows what is due')
  await shot(page, 'desktop-home')
  await go(page, '/decks/1')
  await shot(page, 'desktop-deck')
  await go(page, '/browse')
  await shot(page, 'desktop-browse')
  await go(page, '/stats', 1800)
  await shot(page, 'desktop-stats')
  await go(page, '/create')
  await shot(page, 'desktop-create')
  await go(page, '/import')
  await shot(page, 'desktop-import')
  for (const p of ['/decks', '/settings/study', '/settings/reminders', '/settings/apps', '/settings/ai', '/settings/data', '/settings/about']) await go(page, p, 500)
  await go(page, '/study?deck=3')
  await shot(page, 'desktop-study')
  await page.keyboard.press('Space')
  await page.waitForTimeout(500)
  await shot(page, 'desktop-study-answer')
  await page.keyboard.press('z') // nothing answered yet: undo stays quiet
  await go(page, '/')
  await page.keyboard.press('n')
  await page.waitForTimeout(600)
  await page.locator('textarea').first().fill('¿Qué hora es?')
  await page.locator('textarea').nth(1).fill('What time is it?')
  await page.waitForTimeout(300)
  await shot(page, 'desktop-add')
  await ctx.close()
}

// ---- phone ----
{
  const { ctx, page } = await session({ phone: true, dark: true })
  await go(page, '/')
  await shot(page, 'phone-home')
  await go(page, '/study?deck=1')
  await page.getByRole('button', { name: /show answer/i }).click()
  await page.waitForTimeout(500)
  await shot(page, 'phone-study')
  await ctx.close()
}

await browser.close()
if (errors.length) {
  console.error('browser errors:\n' + errors.join('\n'))
  process.exit(1)
}
console.log('ok')
