import { TestBed } from '@angular/core/testing';
import { fail, flush, http, provideApp } from '../../testing';
import { DraftSummary } from './models';
import { Drafts } from './drafts';

const summary = (overrides: Partial<DraftSummary>): DraftSummary => ({
  id: '0123456789abcdef',
  repo: 'platform',
  path: 'docs/a.md',
  title: 'A',
  status: 'draft',
  isNew: true,
  author: '',
  updatedAt: '2026-10-22T12:00:00Z',
  baseBlobId: '',
  ...overrides,
});

describe('Drafts', () => {
  let drafts: Drafts;

  beforeEach(() => {
    TestBed.configureTestingModule({ providers: provideApp() });
    drafts = TestBed.inject(Drafts);
  });

  afterEach(() => http().verify());

  async function settle(): Promise<void> {
    await Promise.resolve();
    TestBed.tick();
  }

  it('loads all drafts and counts them', async () => {
    TestBed.tick();
    expect(drafts.loading()).toBe(true);
    flush('/api/v1/drafts', {
      drafts: [summary({}), summary({ id: 'f'.repeat(16), repo: 'web', path: 'docs/b.md' })],
    });
    await settle();
    expect(drafts.count()).toBe(2);
    expect(drafts.loading()).toBe(false);
  });

  it('finds a draft by repository and path', async () => {
    TestBed.tick();
    flush('/api/v1/drafts', { drafts: [summary({}), summary({ repo: 'web' })] });
    await settle();
    expect(drafts.forPath('platform', 'docs/a.md')?.repo).toBe('platform');
    expect(drafts.forPath('web', 'docs/a.md')?.repo).toBe('web');
    expect(drafts.forPath('platform', 'docs/none.md')).toBeUndefined();
  });

  it('is empty after a failure and exposes the error', async () => {
    TestBed.tick();
    fail('/api/v1/drafts', 500, { error: 'internal_error', message: 'x' });
    await settle();
    expect(drafts.list()).toEqual([]);
    expect(drafts.error()).toBeTruthy();
  });

  it('reloads on demand', async () => {
    TestBed.tick();
    flush('/api/v1/drafts', { drafts: [] });
    await settle();
    drafts.reload();
    TestBed.tick();
    flush('/api/v1/drafts', { drafts: [summary({})] });
    await settle();
    expect(drafts.count()).toBe(1);
  });
});
