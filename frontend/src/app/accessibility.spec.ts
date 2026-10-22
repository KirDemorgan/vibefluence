import { Component } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import {
  accessibilityViolations,
  doc,
  flush,
  http,
  provideApp,
  REPO,
  SECOND_REPO,
  TREE,
} from '../testing';
import { I18n } from './core/i18n';
import { Ui } from './core/ui';
import { DocPage } from './features/doc/doc-page';
import { DraftsPage } from './features/drafts/drafts-page';
import { EditorPage } from './features/editor/editor-page';
import { HomePage } from './features/home/home-page';
import { RepoPage } from './features/repo/repo-page';
import { Shell } from './layout/shell';
import { SettingsMenu } from './layout/settings-menu';

@Component({ template: '<h1>Page</h1><p>content</p>' })
class Child {}

describe('accessibility', () => {
  let fixture: ComponentFixture<unknown>;

  beforeEach(() => {
    localStorage.clear();
    TestBed.configureTestingModule({
      providers: provideApp([{ path: '**', component: Child }]),
    });
    TestBed.inject(I18n).setLang('en');
  });

  afterEach(() => {
    http()
      .match(() => true)
      .forEach(() => undefined);
    fixture.destroy();
  });

  async function settle(): Promise<void> {
    await Promise.resolve();
    TestBed.tick();
    fixture.detectChanges();
  }

  function answer(): void {
    http()
      .match('/api/v1/repos')
      .forEach((request) => request.flush({ repos: [REPO, SECOND_REPO] }));
    http()
      .match('/api/v1/drafts')
      .forEach((request) =>
        request.flush({
          drafts: [
            {
              id: '0123456789abcdef',
              repo: 'platform',
              path: 'docs/a.md',
              title: 'A',
              status: 'draft',
              isNew: true,
              author: '',
              updatedAt: '2026-10-22T12:00:00Z',
              baseBlobId: '',
            },
          ],
        }),
      );
    http()
      .match('/api/v1/repos/platform/tree')
      .forEach((request) => request.flush(TREE));
  }

  async function render<T>(
    type: new () => T,
    url: string,
    inputs: Record<string, unknown> = {},
  ): Promise<HTMLElement> {
    await TestBed.inject(Router).navigateByUrl(url);
    const created = TestBed.createComponent(type);
    fixture = created;
    for (const [name, value] of Object.entries(inputs)) {
      created.componentRef.setInput(name, value);
    }
    created.detectChanges();
    TestBed.tick();
    answer();
    await settle();
    answer();
    await settle();
    return created.nativeElement as HTMLElement;
  }

  it('the home page has no violations', async () => {
    const element = await render(HomePage, '/');
    expect(await accessibilityViolations(element)).toEqual([]);
  });

  it('the repository page has no violations', async () => {
    const element = await render(RepoPage, '/r/platform', { repo: 'platform' });
    expect(await accessibilityViolations(element)).toEqual([]);
  });

  it('the drafts page has no violations', async () => {
    const element = await render(DraftsPage, '/drafts');
    expect(await accessibilityViolations(element)).toEqual([]);
  });

  it('the application shell has no violations on the home page and inside a repository', async () => {
    let element = await render(Shell, '/');
    expect(await accessibilityViolations(element)).toEqual([]);
    fixture.destroy();
    element = await render(Shell, '/r/platform/docs/guide/start.md');
    expect(await accessibilityViolations(element)).toEqual([]);
  });

  it('the search palette has no violations while open', async () => {
    const element = await render(Shell, '/r/platform');
    TestBed.inject(Ui).openSearch();
    fixture.detectChanges();
    TestBed.tick();
    fixture.detectChanges();
    expect(await accessibilityViolations(element)).toEqual([]);
  });

  it('the settings menu has no violations', async () => {
    const element = await render(SettingsMenu, '/');
    expect(await accessibilityViolations(element)).toEqual([]);
  });

  it('a rendered document has no violations', async () => {
    await TestBed.inject(Router).navigateByUrl('/r/platform/docs/guide/start.md');
    const created = TestBed.createComponent(DocPage);
    fixture = created;
    created.componentRef.setInput('repo', 'platform');
    created.componentRef.setInput('path', 'docs/guide/start.md');
    created.detectChanges();
    TestBed.tick();
    answer();
    flush('/api/v1/repos/platform/doc?path=docs/guide/start.md', doc());
    await settle();
    answer();
    await settle();
    expect(await accessibilityViolations(created.nativeElement)).toEqual([]);
  });

  it('the editor has no violations, including the publish dialog', async () => {
    const element = await render(EditorPage, '/new/platform', { repo: 'platform' });
    http()
      .match(() => true)
      .forEach((request) => request.flush({}));
    expect(await accessibilityViolations(element)).toEqual([]);
  });
});
