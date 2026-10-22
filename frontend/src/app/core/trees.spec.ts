import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { fail, flush, http, provideApp, TREE } from '../../testing';
import { Trees } from './trees';

@Component({ template: '' })
class Blank {}

describe('Trees', () => {
  let trees: Trees;
  let router: Router;

  beforeEach(() => {
    TestBed.configureTestingModule({ providers: provideApp([{ path: '**', component: Blank }]) });
    trees = TestBed.inject(Trees);
    router = TestBed.inject(Router);
  });

  afterEach(() => http().verify());

  async function settle(): Promise<void> {
    await Promise.resolve();
    TestBed.tick();
  }

  it('does not load anything outside a repository', async () => {
    await router.navigateByUrl('/');
    TestBed.tick();
    http().expectNone((request) => request.url.includes('/tree'));
    expect(trees.tree()).toBeUndefined();
    expect(trees.loading()).toBe(false);
  });

  it('loads the tree of the current repository', async () => {
    await router.navigateByUrl('/r/platform');
    TestBed.tick();
    expect(trees.loading()).toBe(true);
    flush('/api/v1/repos/platform/tree', TREE);
    await settle();

    expect(trees.tree()?.repo).toBe('platform');
    expect(trees.loading()).toBe(false);
  });

  it('loads a new tree when the repository changes', async () => {
    await router.navigateByUrl('/r/platform');
    TestBed.tick();
    flush('/api/v1/repos/platform/tree', TREE);
    await settle();

    await router.navigateByUrl('/r/web');
    TestBed.tick();
    flush('/api/v1/repos/web/tree', { ...TREE, repo: 'web' });
    await settle();
    expect(trees.tree()?.repo).toBe('web');
  });

  it('does not reload when navigating inside the same repository', async () => {
    await router.navigateByUrl('/r/platform');
    TestBed.tick();
    flush('/api/v1/repos/platform/tree', TREE);
    await settle();

    await router.navigateByUrl('/r/platform/docs/README.md');
    TestBed.tick();
    http().expectNone((request) => request.url.includes('/tree'));
  });

  it('reports errors and can reload', async () => {
    await router.navigateByUrl('/r/platform');
    TestBed.tick();
    fail('/api/v1/repos/platform/tree', 502, { error: 'upstream_unavailable', message: 'x' });
    await settle();
    expect(trees.error()).toBeTruthy();
    expect(trees.tree()).toBeUndefined();

    trees.reload();
    TestBed.tick();
    flush('/api/v1/repos/platform/tree', TREE);
    await settle();
    expect(trees.tree()).toBeTruthy();
    expect(trees.error()).toBeUndefined();
  });
});
