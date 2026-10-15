// Generates realistic demo traffic: real browsers drive the browser demo
// (examples/browser-demo), which sends events, spans and errors through the
// SDK and calls the instrumented Go demo API. Each simulated user gets a
// fresh browser context, so a new anonymous id and session.
//
//   node scripts/seed-demo.mjs            # 30 users
//   SEED_USERS=60 DEMO_URL=http://localhost:5173 node scripts/seed-demo.mjs
//
// Funnel shape: $pageview -> checkout_started -> payment_succeeded. The Go
// API fails about 40% of payments (slowly), so users who drop after
// checkout line up with errors and high backend latency.
import { chromium } from '@playwright/test';

const demo = process.env.DEMO_URL ?? 'http://localhost:5173';
const api = process.env.DEMO_API_URL ?? 'http://localhost:8090';
const ingest = process.env.INGEST_URL ?? 'http://localhost:8080';
const users = Number(process.env.SEED_USERS ?? 30);
const concurrency = Number(process.env.SEED_CONCURRENCY ?? 5);
// Use an installed Chrome when Playwright's own browser is not downloaded.
const channel = process.env.PW_CHANNEL || undefined;

// Deterministic per-user randomness so runs are comparable.
function rng(seed) {
  let s = seed * 9301 + 49297;
  return () => ((s = (s * 9301 + 49297) % 233280) / 233280);
}

const stats = { users: 0, checkout: 0, paid: 0, failed: 0, abandoned: 0 };

async function simulate(browser, i) {
  const r = rng(i + Date.now() % 1000);
  const ctx = await browser.newContext({ viewport: { width: 1000, height: 700 } });
  const page = await ctx.newPage();
  const q = new URLSearchParams({ api, ingest }).toString();
  try {
    await page.goto(`${demo}/?${q}`, { waitUntil: 'load' });
    stats.users++;
    await page.waitForTimeout(300 + r() * 700);
    if (r() < 0.8) {
      await page.click('a[href="/pricing"]');
      await page.waitForTimeout(200 + r() * 500);
    }
    if (r() < 0.25) {
      await page.click('#orders'); // browse orders (Go API + SQL)
      await page.waitForTimeout(300);
    }
    if (r() < 0.75) {
      await page.click('a[href="/checkout"]');
      await page.click('#checkout');
      stats.checkout++;
      await page.waitForTimeout(400 + r() * 800);
      if (r() < 0.85) {
        const res = page.waitForResponse((x) => x.url().includes('/api/pay'), { timeout: 15000 });
        await page.click('#buy');
        const status = (await res).status();
        if (status === 200) stats.paid++;
        else stats.failed++;
        await page.waitForTimeout(500);
      } else {
        stats.abandoned++;
      }
    }
    // Let the SDK flush its batches (2 s interval) before the page goes away.
    await page.waitForTimeout(2600);
    await page.evaluate(() => window.dispatchEvent(new Event('pagehide')));
    await page.waitForTimeout(400);
  } finally {
    await ctx.close();
  }
}

const browser = await chromium.launch({ channel, args: ['--no-sandbox'] });
let next = 0;
await Promise.all(
  Array.from({ length: concurrency }, async () => {
    while (next < users) {
      const i = next++;
      await simulate(browser, i).catch((e) => console.error(`user ${i}: ${e.message}`));
    }
  }),
);
await browser.close();
console.log(
  `seeded ${stats.users} users: ${stats.checkout} started checkout, ${stats.paid} paid, ${stats.failed} payments failed, ${stats.abandoned} abandoned`,
);
if (stats.users === 0) process.exit(1);
