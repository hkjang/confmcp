// Rasterise the confmcp logo into the PNG sizes browsers expect, and the
// social card SVG into the PNG used for link previews. Runs in the Playwright
// image (hack/screenshots.sh icons) so no local browser is needed.
//
//   node hack/icons.mjs

import { chromium } from '../web/node_modules/playwright/index.mjs'
import { copyFile, mkdir, readFile } from 'node:fs/promises'

const SVG = 'web/public/favicon.svg'
const TARGETS = [
  { file: 'web/public/icon-32.png', size: 32 },
  { file: 'web/public/icon-180.png', size: 180 },
  { file: 'web/public/icon-192.png', size: 192 },
  { file: 'web/public/icon-512.png', size: 512 },
  { file: 'docs/assets/logo-512.png', size: 512 },
  { file: 'docs/assets/logo-192.png', size: 192 },
]

const svg = (await readFile(SVG, 'utf8')).replace(/<\?xml[^>]*\?>/, '')
const browser = await chromium.launch({ args: ['--no-sandbox', '--disable-dev-shm-usage'] })
await mkdir('docs/assets', { recursive: true })

for (const target of TARGETS) {
  const page = await browser.newPage({ viewport: { width: target.size, height: target.size } })
  await page.setContent(
    `<!doctype html><html><head><style>html,body{margin:0;background:transparent}svg{display:block;width:${target.size}px;height:${target.size}px}</style></head><body>${svg}</body></html>`,
  )
  await page.screenshot({ path: target.file, omitBackground: true })
  await page.close()
  console.log(`생성: ${target.file}`)
}

const social = await readFile('docs/assets/social-card.svg', 'utf8').catch(() => '')
if (social) {
  const page = await browser.newPage({ viewport: { width: 1200, height: 630 } })
  await page.setContent(`<!doctype html><html><head><style>html,body{margin:0}svg{display:block;width:1200px;height:630px}</style></head><body>${social}</body></html>`)
  await page.waitForTimeout(300)
  await page.screenshot({ path: 'docs/assets/social-card.png' })
  await page.close()
  console.log('생성: docs/assets/social-card.png')
}
await browser.close()
await copyFile(SVG, 'docs/assets/favicon.svg')
