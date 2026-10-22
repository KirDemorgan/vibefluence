import { TestBed } from '@angular/core/testing';
import { fail, flush, http, provideApp, REPO, SECOND_REPO } from '../../testing';
import { Repos } from './repos';

describe('Repos', () => {
  let repos: Repos;

  beforeEach(() => {
    TestBed.configureTestingModule({ providers: provideApp() });
    repos = TestBed.inject(Repos);
  });

  afterEach(() => http().verify());

  it('loads the repository list once', async () => {
    TestBed.tick();
    expect(repos.loading()).toBe(true);
    expect(repos.list()).toEqual([]);

    flush('/api/v1/repos', { repos: [REPO, SECOND_REPO] });
    await Promise.resolve();
    TestBed.tick();

    expect(repos.loading()).toBe(false);
    expect(repos.list().map((repo) => repo.id)).toEqual(['platform', 'web']);
    expect(repos.error()).toBeUndefined();
  });

  it('looks repositories up by id', async () => {
    TestBed.tick();
    flush('/api/v1/repos', { repos: [REPO] });
    await Promise.resolve();
    TestBed.tick();

    expect(repos.byId('platform')?.name).toBe('Platform');
    expect(repos.byId('nope')).toBeUndefined();
    expect(repos.byId(null)).toBeUndefined();
    expect(repos.byId(undefined)).toBeUndefined();
  });

  it('exposes load errors', async () => {
    TestBed.tick();
    fail('/api/v1/repos', 502, { error: 'upstream_unavailable', message: 'down' });
    await Promise.resolve();
    TestBed.tick();

    expect(repos.error()).toBeTruthy();
    expect(repos.loading()).toBe(false);
  });

  it('reloads on demand', async () => {
    TestBed.tick();
    flush('/api/v1/repos', { repos: [REPO] });
    await Promise.resolve();
    TestBed.tick();

    repos.reload();
    TestBed.tick();
    flush('/api/v1/repos', { repos: [REPO, SECOND_REPO] });
    await Promise.resolve();
    TestBed.tick();

    expect(repos.list()).toHaveLength(2);
  });
});
