import { Component } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { fail, flush, http, provideApp, REPO, TREE } from '../../../testing';
import { I18n } from '../../core/i18n';
import { Toasts } from '../../core/toast';
import { RepoPage } from './repo-page';

@Component({ template: '' })
class Blank {}

describe('RepoPage', () => {
  const toasts = { success: vi.fn(), error: vi.fn() };
  let fixture: ComponentFixture<RepoPage>;

  beforeEach(() => {
    localStorage.clear();
    toasts.success.mockReset();
    toasts.error.mockReset();
    TestBed.configureTestingModule({
      imports: [RepoPage],
      providers: provideApp([{ path: '**', component: Blank }], {
        provide: Toasts,
        useValue: toasts,
      }),
    });
    TestBed.inject(I18n).setLang('en');
  });

  afterEach(() => http().verify());

  async function settle(): Promise<void> {
    await Promise.resolve();
    TestBed.tick();
    fixture.detectChanges();
  }

  async function create(
    options: { tree?: object | number; repos?: object[] } = {},
  ): Promise<HTMLElement> {
    await TestBed.inject(Router).navigateByUrl('/r/platform');
    fixture = TestBed.createComponent(RepoPage);
    fixture.componentRef.setInput('repo', 'platform');
    fixture.detectChanges();
    TestBed.tick();
    flush('/api/v1/repos', { repos: options.repos ?? [REPO] });
    if (typeof options.tree === 'number') {
      fail('/api/v1/repos/platform/tree', options.tree, {
        error: 'upstream_unavailable',
        message: 'x',
      });
    } else if (options.tree !== undefined || options.tree === undefined) {
      flush('/api/v1/repos/platform/tree', options.tree ?? TREE);
    }
    await settle();
    return fixture.nativeElement;
  }

  it('shows a skeleton while the tree loads', async () => {
    await TestBed.inject(Router).navigateByUrl('/r/platform');
    fixture = TestBed.createComponent(RepoPage);
    fixture.componentRef.setInput('repo', 'platform');
    fixture.detectChanges();
    TestBed.tick();
    expect(fixture.nativeElement.querySelectorAll('.skeleton__row')).toHaveLength(4);
    flush('/api/v1/repos', { repos: [REPO] });
    flush('/api/v1/repos/platform/tree', TREE);
  });

  it('shows the repository header with project, branch and links', async () => {
    const element = await create();
    expect(element.querySelector('h1')?.textContent).toBe('Platform');
    expect(element.querySelector('.head__meta')?.textContent).toContain('demo/platform');
    expect(element.querySelector('.chip')?.textContent).toContain('master');
    expect(element.querySelector('.head__actions a[target="_blank"]')?.getAttribute('href')).toBe(
      REPO.webUrl,
    );
    expect(element.querySelector('.head__actions a[href="/new/platform"]')?.textContent).toContain(
      'New document',
    );
    expect(document.title).toBe('Platform · Vibefluence');
  });

  it('groups documents by folder', async () => {
    const element = await create();
    expect(
      [...element.querySelectorAll('.group__title')].map((title) => title.textContent),
    ).toEqual(['docs', 'docs/guide']);
    const links = [...element.querySelectorAll('a.doc')];
    expect(links.map((link) => link.getAttribute('href'))).toEqual([
      '/r/platform/docs/README.md',
      '/r/platform/docs/guide/start.md',
    ]);
    expect(links[1].querySelector('.chip')?.textContent).toBe('guide');
  });

  it('summarises documents and hidden drafts', async () => {
    const element = await create();
    expect(element.querySelector('.summary')?.textContent?.replace(/\s+/g, ' ').trim()).toBe(
      '2 documents · 1 draft hidden',
    );
  });

  it('warns when the data is stale', async () => {
    const element = await create({ tree: { ...TREE, stale: true } });
    expect(element.querySelector('.banner')?.textContent).toContain('saved copy');
  });

  it('shows an empty state without documents', async () => {
    const element = await create({
      tree: { ...TREE, roots: [{ name: 'docs', path: 'docs', kind: 'folder', children: [] }] },
    });
    expect(element.querySelector('.state__title')?.textContent).toContain('No documents yet');
  });

  it('shows an unavailable state with a working retry', async () => {
    const element = await create({ tree: 502 });
    expect(element.querySelector('[role="alert"]')?.textContent).toContain(
      'This repository is unavailable',
    );

    (element.querySelector('[role="alert"] button') as HTMLButtonElement).click();
    TestBed.tick();
    flush('/api/v1/repos', { repos: [REPO] });
    flush('/api/v1/repos/platform/tree', TREE);
    await settle();
    expect(element.querySelectorAll('a.doc')).toHaveLength(2);
  });

  it('ignores a tree that belongs to another repository', async () => {
    const element = await create({ tree: { ...TREE, repo: 'other' } });
    expect(element.querySelectorAll('a.doc')).toHaveLength(0);
  });

  it('refreshes the repository and reloads the data', async () => {
    const element = await create();
    const button = element.querySelector('.head__actions button') as HTMLButtonElement;
    button.click();
    fixture.detectChanges();
    expect(button.disabled).toBe(true);
    expect(button.textContent).toContain('Refreshing');

    const refresh = http().expectOne('/api/v1/repos/platform/refresh');
    expect(refresh.request.method).toBe('POST');
    refresh.flush(REPO);
    TestBed.tick();
    expect(toasts.success).toHaveBeenCalledWith('Repository refreshed');
    flush('/api/v1/repos', { repos: [REPO] });
    flush('/api/v1/repos/platform/tree', TREE);
    await settle();
    expect(button.disabled).toBe(false);
  });

  it('does not start a second refresh while one is running', async () => {
    const element = await create();
    const button = element.querySelector('.head__actions button') as HTMLButtonElement;
    button.click();
    button.disabled = false;
    button.click();
    expect(http().match('/api/v1/repos/platform/refresh')).toHaveLength(1);
  });

  it('reports a failed refresh', async () => {
    const warn = vi.spyOn(console, 'warn').mockImplementation(() => undefined);
    const element = await create();
    (element.querySelector('.head__actions button') as HTMLButtonElement).click();
    fail('/api/v1/repos/platform/refresh', 502, { error: 'upstream_unavailable', message: 'x' });
    await settle();
    expect(toasts.error).toHaveBeenCalledWith('Could not refresh the repository');
    expect(toasts.success).not.toHaveBeenCalled();
    expect((element.querySelector('.head__actions button') as HTMLButtonElement).disabled).toBe(
      false,
    );
    warn.mockRestore();
  });
});
