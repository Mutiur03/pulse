import { expect, test, type APIRequestContext, type Page } from '@playwright/test';

const email = process.env.PULSE_EMAIL ?? 'demo@pulse.local';
const password = process.env.PULSE_PASSWORD ?? 'pulse-demo';
// Set SCREENSHOT_DIR to save a screenshot of each main page.
const shots = process.env.SCREENSHOT_DIR;

async function shot(page: Page, name: string, idle = true) {
  if (!shots) return;
  if (idle) await page.waitForLoadState('networkidle');
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
  // Pick a query span; connection spans (sql.connector.connect) carry no statement.
  await wf
    .locator('[data-testid=span-row][data-source=database]')
    .filter({ hasNotText: /connect/i })
    .first()
    .click();
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

test('sign up, create a project and manage its keys', async ({ page }) => {
  const newEmail = `e2e-${Date.now()}@example.com`;
  await page.goto('/login');
  await page.getByRole('link', { name: 'Create an account' }).click();
  await expect(page).toHaveURL(/\/signup/);
  await shot(page, '09-signup');
  await page.getByLabel('Name').fill('E2E User');
  await page.getByLabel('Email').fill(newEmail);
  await page.getByLabel('Password').fill('short');
  await page.getByRole('button', { name: 'Create account' }).click();
  await expect(page.getByText('Password must be at least 10 characters.')).toBeVisible();
  await page.getByLabel('Password').fill('a-long-e2e-password');
  await page.getByRole('button', { name: 'Create account' }).click();

  await expect(page).toHaveURL(/\/projects\/new/);
  await expect(page.getByTestId('user-email')).toHaveText(newEmail);
  await page.getByLabel('Project name').fill('E2E Shop');
  await shot(page, '10-new-project');
  await page.getByRole('button', { name: 'Create project' }).click();
  const key = page.getByTestId('new-project-key');
  await expect(key).toHaveText(/^pk_[0-9a-f]{32}$/);
  const value = await key.innerText();
  await expect(page.getByTestId('new-project-snippet')).toContainText(value);
  await shot(page, '11-project-key');

  await page.goto('/settings');
  await expect(page.getByTestId('project-switcher').locator('option:checked')).toHaveText('E2E Shop');
  await expect(page.getByTestId('active-key')).toHaveText(value);
  page.on('dialog', (d) => void d.accept());
  await page.getByTestId('add-key').click();
  await expect(page.getByTestId('active-key')).toHaveCount(2);
  await page.getByTestId('revoke-key').last().click();
  await expect(page.getByTestId('active-key')).toHaveCount(1);
  await expect(page.getByText('revoked', { exact: true })).toBeVisible();
  await shot(page, '12-key-management');

  // The same email cannot sign up twice.
  await page.getByRole('button', { name: 'Log out' }).click();
  await expect(page).toHaveURL(/\/login$/);
  await page.goto('/signup');
  await page.getByLabel('Name').fill('Again');
  await page.getByLabel('Email').fill(newEmail);
  await page.getByLabel('Password').fill('a-long-e2e-password');
  await page.getByRole('button', { name: 'Create account' }).click();
  await expect(page.getByText('An account with this email already exists.')).toBeVisible();
});

// Emails from the local stack land in Mailpit; read them through its API.
const mailpit = process.env.MAILPIT_URL ?? 'http://localhost:8025';

async function mailLink(request: APIRequestContext, to: string, path: string): Promise<string> {
  let link = '';
  await expect
    .poll(
      async () => {
        const res = await request.get(`${mailpit}/api/v1/search?query=${encodeURIComponent(`to:"${to}"`)}`);
        const { messages } = (await res.json()) as { messages: { ID: string }[] };
        for (const m of messages ?? []) {
          const msg = (await (await request.get(`${mailpit}/api/v1/message/${m.ID}`)).json()) as { Text: string };
          const found = msg.Text.match(new RegExp(`https?://\\S+${path}\\?token=[\\w-]+`));
          if (found) {
            link = found[0];
            return true;
          }
        }
        return false;
      },
      { timeout: 20_000 },
    )
    .toBe(true);
  // Links point at APP_URL; keep the path so tests work against any base URL.
  const u = new URL(link);
  return u.pathname + u.search;
}

async function apiSignup(page: Page, who: string) {
  const res = await page.request.post('/api/v1/auth/signup', {
    data: { email: who, password: 'first-password-1', name: who.split('@')[0] },
  });
  expect(res.status()).toBe(201);
}

test('verify email, then reset a forgotten password', async ({ page, playwright }) => {
  const who = `reset-${Date.now()}@example.com`;
  await apiSignup(page, who);
  const oldSession = (await page.context().cookies()).find((c) => c.name === 'pulse_session')!.value;
  await page.goto('/settings');
  await expect(page.getByTestId('verify-banner')).toBeVisible();
  await shot(page, '13-verify-banner');
  await page.goto(await mailLink(page.request, who, '/verify-email'));
  await expect(page.getByTestId('verify-result')).toHaveText('Your email address is confirmed.');
  await page.goto('/settings');
  await expect(page.getByTestId('org-switcher')).toBeVisible();
  await expect(page.getByTestId('verify-banner')).toHaveCount(0);

  await page.context().clearCookies();
  await page.goto('/login');
  await page.getByRole('link', { name: 'Forgot password?' }).click();
  await page.getByLabel('Email').fill(who);
  await shot(page, '14-forgot-password');
  await page.getByRole('button', { name: 'Send reset link' }).click();
  await expect(page.getByTestId('reset-sent')).toBeVisible();
  // Unknown emails get exactly the same answer.
  const unknown = await page.request.post('/api/v1/auth/password-reset/request', { data: { email: `nobody-${Date.now()}@example.com` } });
  expect(unknown.status()).toBe(204);

  const resetLink = await mailLink(page.request, who, '/reset-password');
  await page.goto(resetLink);
  await page.getByLabel('New password').fill('second-password-2');
  await shot(page, '15-new-password');
  await page.getByRole('button', { name: 'Set new password' }).click();
  await expect(page.getByTestId('reset-done')).toBeVisible();

  // The link is single use and the old session is gone.
  const again = await page.request.post('/api/v1/auth/password-reset/confirm', { data: { token: new URL(resetLink, 'http://x').searchParams.get('token'), password: 'third-password-3' } });
  expect(again.status()).toBe(400);
  const other = await playwright.request.newContext({ baseURL: page.url(), extraHTTPHeaders: { cookie: `pulse_session=${oldSession}` } });
  expect((await other.get('/api/v1/auth/me')).status()).toBe(401);
  await other.dispose();

  await page.getByRole('button', { name: 'Sign in' }).click();
  await page.getByLabel('Email').fill(who);
  await page.getByLabel('Password').fill('first-password-1');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByText('Wrong email or password.')).toBeVisible();
  await page.getByLabel('Password').fill('second-password-2');
  await page.getByRole('button', { name: 'Sign in' }).click();
  await expect(page.getByTestId('user-email')).toHaveText(who);
});

test('owners invite new and existing users into their organization', async ({ page, browser }) => {
  const stamp = Date.now();
  const owner = `owner-${stamp}@example.com`;
  const fresh = `fresh-${stamp}@example.com`;
  const existing = `existing-${stamp}@example.com`;
  await apiSignup(page, owner);
  const created = await page.request.post('/api/v1/projects', { data: { name: 'Team App' } });
  expect(created.status()).toBe(201);

  await page.goto('/settings');
  const orgName = await page.getByTestId('org-switcher').locator('option:checked').innerText();
  for (const [to, role] of [
    [fresh, 'member'],
    [existing, 'owner'],
    [`revoked-${stamp}@example.com`, 'member'],
  ]) {
    await page.getByLabel('Invite email').fill(to);
    await page.getByLabel('Invite role').selectOption(role);
    await page.getByRole('button', { name: 'Send invite' }).click();
    await expect(page.getByText(`Invite sent to ${to}.`)).toBeVisible();
  }
  await expect(page.getByTestId('invite-row')).toHaveCount(3);
  await page.getByTestId('invite-row').filter({ hasText: 'revoked-' }).getByRole('button', { name: 'Revoke' }).click();
  await expect(page.getByTestId('invite-row')).toHaveCount(2);
  await shot(page, '16-members-invites');

  // A new user signs up from the invite link and lands in the organization.
  const freshCtx = await browser.newContext();
  const freshPage = await freshCtx.newPage();
  await freshPage.goto(await mailLink(page.request, fresh, '/invite'));
  await expect(freshPage.getByTestId('invite-card')).toBeVisible();
  await freshPage.getByLabel('Name').fill('Fresh');
  await freshPage.getByLabel('Password').fill('fresh-password-1');
  await shot(freshPage, '17-invite-signup');
  await freshPage.getByRole('button', { name: 'Create account and join' }).click();
  await expect(freshPage).toHaveURL(/\/traces/);
  await expect(freshPage.getByTestId('org-switcher').locator('option:checked')).toHaveText(orgName);
  await expect(freshPage.getByTestId('project-switcher').locator('option:checked')).toHaveText('Team App');
  await expect(freshPage.getByTestId('verify-banner')).toHaveCount(0);
  await freshCtx.close();

  // An existing user accepts while signed in and can switch organizations.
  const exCtx = await browser.newContext();
  const exPage = await exCtx.newPage();
  await apiSignup(exPage, existing);
  await exPage.goto(await mailLink(page.request, existing, '/invite'));
  await expect(exPage.getByText(`Signed in as ${existing}`)).toBeVisible();
  await shot(exPage, '18-invite-accept');
  await exPage.getByRole('button', { name: `Accept and join ${orgName}` }).click();
  await expect(exPage).toHaveURL(/\/traces/);
  const orgs = exPage.getByTestId('org-switcher');
  await expect(orgs.locator('option')).toHaveCount(2);
  await expect(orgs.locator('option:checked')).toHaveText(orgName);
  await shot(exPage, '19-org-switcher');
  await exCtx.close();

  await page.reload();
  await expect(page.getByTestId('member-row')).toHaveCount(3);
  await expect(page.getByTestId('invite-row')).toHaveCount(0);
  page.on('dialog', (d) => void d.accept());
  await page.getByTestId('member-row').filter({ hasText: fresh }).getByRole('button', { name: 'Remove' }).click();
  await expect(page.getByTestId('member-row')).toHaveCount(2);
  await shot(page, '20-members');
});

const demo = process.env.DEMO_URL ?? 'http://localhost:5173';

test('a recorded demo session with an error replays and is linked from its issue', async ({ page, browser }) => {
  test.setTimeout(150_000);
  // Record a session in the browser demo: type into a masked input, then throw.
  const ctx = await browser.newContext();
  const app = await ctx.newPage();
  await app.goto(`${demo}/?replay=1`);
  const session = await app.waitForFunction(() => (window as unknown as { pulseSession?: string }).pulseSession).then((h) => h.jsonValue());
  await app.locator('#email').pressSequentially('jane@private.example', { delay: 60 });
  await app.locator('a[data-route]').first().click();
  await app.waitForTimeout(3000);
  await app.locator('#boom').click();
  await app.waitForResponse((r) => r.url().includes('/v1/replay') && r.status() === 202, { timeout: 20_000 });
  await app.waitForTimeout(1000);
  await ctx.close();

  await login(page);
  await expect(async () => {
    await page.goto('/replays');
    await expect(page.locator(`[data-testid=replay-row][data-session="${session}"]`)).toBeVisible({ timeout: 3000 });
    await expect(page.locator(`[data-testid=replay-row][data-session="${session}"]`)).not.toHaveAttribute('data-errors', '0', { timeout: 1000 });
  }).toPass({ timeout: 45_000 });
  await shot(page, '21-replays');

  // Nothing typed, masked or blocked was stored.
  const stored = await page.request.get(`/api/v1/replays/${session}/events`);
  expect(stored.ok()).toBeTruthy();
  const raw = await stored.text();
  expect(raw).toContain('"pulse"');
  expect(raw).not.toContain('jane@private.example');
  expect(raw).not.toContain('Jane Example');
  expect(raw).not.toContain('not recorded');

  await page.locator(`[data-testid=replay-row][data-session="${session}"]`).getByRole('link').first().click();
  await expect(page.locator('.replayer-wrapper iframe')).toBeAttached();
  await expect(page.getByTestId('replay-timeline').getByTestId('marker-error').first()).toBeAttached();
  await page.getByTestId('tab-errors').click();
  await expect(page.getByTestId('replay-error').first()).toContainText('cannot read properties');
  const before = await page.getByTestId('replay-time').textContent();
  await page.getByTestId('replay-play').click();
  await expect(page.getByTestId('replay-time')).not.toHaveText(before ?? '', { timeout: 10_000 });
  await shot(page, '22-replay-player', false);

  // The issue for the thrown error links to this session at the error time.
  await expect(async () => {
    await page.goto('/issues');
    await page.getByTestId('issue-row').filter({ hasText: 'cannot read properties' }).first().getByRole('link').first().click();
    await expect(page.locator(`[data-testid=occurrence-replay-link][href^="/replays/${session}?t="]`)).toBeVisible({ timeout: 3000 });
  }).toPass({ timeout: 45_000 });
  await shot(page, '23-issue-replay-link');
  await page.locator(`[data-testid=occurrence-replay-link][href^="/replays/${session}?t="]`).click();
  await expect(page).toHaveURL(new RegExp(`/replays/${session}\\?t=\\d+`));
  await expect(page.locator('.replayer-wrapper iframe')).toBeAttached();
  await expect(page.getByTestId('replay-error').first()).toBeVisible();
  await shot(page, '24-replay-from-issue', false);

  // Traces from the session link back to the replay at their start.
  const detail = await page.request.get(`/api/v1/replays/${session}`);
  const { traces } = (await detail.json()) as { traces: { trace_id: string }[] };
  expect(traces.length).toBeGreaterThan(0);
  await page.goto(`/traces/${traces[0].trace_id}`);
  await expect(page.getByTestId('trace-replay-link')).toBeVisible();
  await shot(page, '25-trace-replay-link');
  await page.getByTestId('trace-replay-link').click();
  await expect(page).toHaveURL(new RegExp(`/replays/${session}\\?t=\\d+`));
  await expect(page.locator('.replayer-wrapper iframe')).toBeAttached();
});
