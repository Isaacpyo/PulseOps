import { expect, test } from '@playwright/test';

/**
 * Main flow: demo login → overview → create a check → acknowledge, annotate
 * and resolve an incident → public status page with 90-day uptime bars.
 *
 * The API is booted with DEMO_RESET_ON_BOOT and the scheduler disabled, so the
 * seeded active incident on "GitHub status API" is guaranteed to be open.
 * Projects run serially (workers: 1) and the desktop project resolves the
 * incident, so the mobile project covers the read-only parts of the flow.
 */
test('demo operator can triage an incident end to end', async ({ page }, testInfo) => {
  const mobile = testInfo.project.name === 'mobile';

  await page.goto('/');
  await expect(page.getByRole('heading', { name: /know it’s down/i })).toBeVisible();
  await page.getByRole('button', { name: 'Try the demo' }).click();

  await expect(page).toHaveURL(/\/app$/);
  await expect(page.getByRole('heading', { name: 'Overview' })).toBeVisible();
  const checks = page.getByRole('list', { name: 'Checks' });
  await expect(checks.getByRole('link')).toHaveCount(mobile ? 7 : 6);
  await expect(checks.getByText('Marketing website')).toBeVisible();

  // No horizontal page scroll at phone width.
  const overflows = await page.evaluate(() => document.documentElement.scrollWidth > window.innerWidth);
  expect(overflows).toBe(false);

  if (!mobile) {
    // Create a check (validation runs client- and server-side).
    await page.getByRole('link', { name: 'New check' }).click();
    await page.getByRole('button', { name: 'Create check' }).click();
    await expect(page.getByText('Name is required')).toBeVisible();
    await page.getByLabel('Name').fill('E2E storefront');
    await page.getByLabel('URL').fill('https://example.org/');
    await page.getByRole('button', { name: 'Create check' }).click();
    await expect(page.getByRole('heading', { name: 'E2E storefront' })).toBeVisible();
    await expect(page.getByText('Pending').first()).toBeVisible();

    // Triage the seeded incident.
    await page.getByRole('navigation', { name: 'Main' }).getByRole('link', { name: /Incidents/ }).click();
    await page.getByRole('link', { name: /GitHub status API is responding slowly/ }).click();
    await expect(page.getByRole('heading', { name: 'GitHub status API is responding slowly' })).toBeVisible();

    await page.getByRole('button', { name: 'Acknowledge' }).click();
    await expect(page.getByText('Acknowledged by Demo Operator.')).toBeVisible();

    await page.getByLabel('Add a note to the timeline').fill('Upstream status page reports elevated latency.');
    await page.getByRole('button', { name: 'Add note' }).click();
    await expect(page.getByText('Upstream status page reports elevated latency.')).toBeVisible();

    await page.getByRole('button', { name: 'Resolve…' }).click();
    await page.getByLabel('Resolution note (optional)').fill('Latency back to normal.');
    await page.getByRole('button', { name: 'Resolve incident' }).click();
    await expect(page.getByText('Resolved by Demo Operator. Latency back to normal.')).toBeVisible();
    await expect(page.getByRole('button', { name: 'Acknowledge' })).toHaveCount(0);
  }

  // Public status page (no auth).
  await page.goto('/status/demo');
  await expect(page.getByRole('heading', { name: 'Acme Cloud status' })).toBeVisible();
  await expect(page.getByText('All systems operational')).toBeVisible();
  // 90 days of bars; phones show the most recent 30 so each stays tappable.
  const bars = page.getByRole('list', { name: 'Marketing website: daily uptime history' });
  await expect(bars.getByRole('listitem')).toHaveCount(mobile ? 30 : 90);
  await expect(page.getByRole('heading', { name: /Past incidents/ })).toBeVisible();
});
