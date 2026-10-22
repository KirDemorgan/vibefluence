import { expect, test } from '@playwright/test';

test.describe.configure({ mode: 'serial' });

test('lists documents and hides drafts that are in the repository', async ({ page }) => {
  await page.goto('/r/platform');
  await expect(page.getByText('6 documents · 1 draft hidden')).toBeVisible();
  await expect(page.getByText('Rollout plan')).toHaveCount(0);
});

test('opens a document with a table of contents', async ({ page }) => {
  await page.goto('/r/platform/docs/architecture/overview.md');
  await expect(page.getByRole('heading', { level: 1, name: 'Architecture overview' })).toBeVisible();
  await expect(page.getByRole('navigation', { name: /on this page/i })).toBeVisible();
});

test('finds documents with the search palette', async ({ page }) => {
  await page.goto('/r/platform');
  await page.keyboard.press('ControlOrMeta+k');
  await page.getByPlaceholder('Search documents…').fill('architecture');
  await expect(page.getByRole('option', { name: /Architecture overview/ })).toBeVisible();
});

test('creates a document and publishes it through a merge request', async ({ page }) => {
  await page.goto('/new/platform');
  await page.getByLabel('Title').fill('E2E handbook');
  await page.getByRole('radio', { name: 'Published' }).click();
  await page.getByRole('textbox', { name: 'Markdown' }).fill('# E2E handbook\n\nWritten by the browser test.\n');
  await expect(page.getByLabel('File path')).toHaveValue('docs/e2e-handbook.md');

  await page.getByRole('button', { name: 'Publish', exact: true }).click();
  await page.getByRole('dialog').getByRole('button', { name: 'Publish', exact: true }).click();

  await expect(page.getByText('Published', { exact: true }).first()).toBeVisible({ timeout: 30_000 });
  await page.getByRole('link', { name: 'Open the document' }).click();
  await expect(page.getByText('Written by the browser test.')).toBeVisible();
});

test('serves a strict content security policy', async ({ request }) => {
  const response = await request.get('/');
  const policy = response.headers()['content-security-policy'];
  expect(policy).toContain("script-src 'self';");
  expect(policy).toContain("frame-ancestors 'none'");
});

test('exposes request metrics', async ({ request }) => {
  const response = await request.get('/metrics');
  expect(response.ok()).toBe(true);
  expect(await response.text()).toContain('vibefluence_http_requests_total');
});

for (const path of ['/', '/r/platform', '/r/platform/docs/architecture/overview.md', '/new/platform', '/drafts']) {
  test(`has no horizontal scroll on a phone: ${path}`, async ({ page }) => {
    await page.setViewportSize({ width: 375, height: 812 });
    await page.goto(path);
    await page.waitForLoadState('networkidle');
    const overflow = await page.evaluate(() => document.documentElement.scrollWidth - window.innerWidth);
    expect(overflow).toBeLessThanOrEqual(0);
  });
}
