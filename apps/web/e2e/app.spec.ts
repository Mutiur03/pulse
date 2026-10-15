import { expect, test, type Page } from '@playwright/test';

const email = process.env.PULSE_EMAIL ?? 'demo@pulse.local';
const password = process.env.PULSE_PASSWORD ?? 'pulse-demo';
// Set SCREENSHOT_DIR to save a screenshot of each main page.
const shots = process.env.SCREENSHOT_DIR;

async function shot(page: Page, name: string) {
  if (!shots) return;
  await page.waitForLoadState('networkidle');
  await page.screenshot({ path: `${shots}/${name}.png`, fullPage: true });
}

async function login(page: Page) {
  await page.goto('/login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/traces/);
}

test('login rejects a wrong password and protects pages', async ({ page }) => {
  await page.goto('/issues');
  await expect(page).toHaveURL(/\/login\?next=%2Fissues/);
  await shot(page, '01-login');
  await page.getByLabel('Email').fill(email);
  await page.getByLabel('Password').fill('wrong-password');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByText('Wrong email or password.')).toBeVisible();
  await page.getByLabel('Password').fill(password);
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page).toHaveURL(/\/issues/);
  await expect(page.getByTestId('user-email')).toHaveText(email);
});

test('trace waterfall spans browser, backend and database', async ({ page }) => {
  await login(page);
  await expect(page.getByTestId('trace-row').first()).toBeVisible();
  await shot(page, '02-traces');

  // A seeded checkout: browser click -> fetch -> Go API -> SQL.
  const res = await page.request.get('/api/v1/traces?service=go-demo-api&limit=200');
  expect(res.ok()).toBeTruthy();
  const { traces } = (await res.json()) as { traces: { trace_id: string; has_browser: boolean; error_count: number; span_count: number }[] };
  const full = traces.filter((t) => t.has_browser && t.span_count >= 4);
  expect(full.length, 'seeded traces with browser and backend spans').toBeGreaterThan(0);
  const failing = full.find((t) => t.error_count > 0) ?? full[0];

  await page.goto(`/traces/${failing.trace_id}`);
  const wf = page.getByTestId('waterfall');
  await expect(wf).toBeVisible();
  for (const source of ['browser', 'backend', 'database']) {
    await expect(wf.locator(`[data-testid=span-row][data-source=${source}]`).first()).toBeVisible();
  }
  await wf.locator('[data-testid=span-row][data-source=database]').first().click();
  const panel = page.getByTestId('span-panel');
  await expect(panel).toContainText('db.statement');
  if (failing.error_count > 0) await expect(wf.getByTestId('marker-error').first()).toBeAttached();
  await shot(page, '03-trace-waterfall');
});

test('issue detail shows the stack, links to its trace and can be resolved', async ({ page }) => {
  await login(page);
  await page.goto('/issues');
  const row = page.getByTestId('issue-row').filter({ hasText: 'chargeCard' }).first();
  await expect(row).toBeVisible();
  await expect(row.locator('svg[aria-label="occurrences over the last 24 hours"]')).toBeVisible();
  await shot(page, '04-issues');
  await row.getByRole('link').click();

  await expect(page.getByTestId('stack')).toContainText('chargeCard');
  await expect(page.getByTestId('issue-trace-link')).toBeVisible();
  await shot(page, '05-issue-detail');
  await page.getByRole('button', { name: 'Resolve' }).click();
  await expect(page.getByTestId('issue-status')).toHaveText('resolved');
  await page.getByRole('button', { name: 'Reopen' }).click();
  await expect(page.getByTestId('issue-status')).toHaveText('unresolved');

  await page.getByTestId('issue-trace-link').click();
  await expect(page.getByTestId('waterfall')).toBeVisible();
});

test('funnel shows drop-off with errors and latency for users who dropped', async ({ page }) => {
  await login(page);
  await page.goto('/funnels');
  await expect(page.getByLabel('Step 1', { exact: true })).toHaveValue('$pageview');
  await expect(page.getByLabel('Step 2', { exact: true })).toHaveValue('checkout_started');
  await expect(page.getByLabel('Step 3', { exact: true })).toHaveValue('payment_succeeded');
  const runBtn = page.getByTestId('run-funnel');
  await expect(runBtn).toHaveAttribute('data-busy', 'false');
  await runBtn.click();
  await expect(runBtn).toHaveAttribute('data-busy', 'false');
  await expect(page.getByTestId('funnel-chart')).toContainText('users entered');

  // Look at checkout -> payment, where failed payments drop users.
  await page.getByLabel('Drop-off step').selectOption({ label: 'Step 2 → 3' });
  const panel = page.getByTestId('dropoff-panel');
  await expect(panel).toContainText('checkout_started');
  await expect(panel.getByTestId('cohort-dropped')).toContainText('Backend p95');
  await expect(panel.getByTestId('dropoff-errors')).toContainText(/payment declined|Payment failed/i);
  await shot(page, '06-funnels');
});

test('events explorer filters by event and user', async ({ page }) => {
  await login(page);
  await page.goto('/events');
  await page.getByLabel('Event').selectOption('checkout_started');
  const first = page.getByTestId('event-row').first();
  await expect(first).toContainText('checkout_started');
  const user = (await first.locator('td').nth(1).innerText()).trim();
  await first.locator('td').nth(1).getByRole('button').click();
  await expect(page).toHaveURL(/distinct_id=/);
  await page.waitForLoadState('networkidle');
  await expect
    .poll(async () => new Set(await page.getByTestId('event-row').locator('td:nth-child(2)').allInnerTexts()))
    .toEqual(new Set([user]));
  await shot(page, '07-events');
});

test('settings shows the ingest key and SDK snippet', async ({ page }) => {
  await login(page);
  await page.goto('/settings');
  const key = page.getByTestId('active-key');
  await expect(key).toHaveText(/^pk_/);
  await expect(page.getByTestId('sdk-snippet')).toContainText(await key.innerText());
  await shot(page, '08-settings');
});
