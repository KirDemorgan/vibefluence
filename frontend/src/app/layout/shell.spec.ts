import { Component } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { fail, flush, http, provideApp, REPO, SECOND_REPO, TREE } from '../../testing';
import { I18n } from '../core/i18n';
import { Ui } from '../core/ui';
import { Shell } from './shell';

@Component({ template: '<p id="child">child</p>' })
class Child {}

describe('Shell', () => {
  let fixture: ComponentFixture<Shell>;
  let router: Router;
  let ui: Ui;

  beforeEach(() => {
    localStorage.clear();
    sessionStorage.clear();
    TestBed.configureTestingModule({
      imports: [Shell],
      providers: provideApp([{ path: '**', component: Child }]),
    });
    TestBed.inject(I18n).setLang('en');
    router = TestBed.inject(Router);
    ui = TestBed.inject(Ui);
  });

  afterEach(() => http().verify());

  async function settle(): Promise<void> {
    await Promise.resolve();
    TestBed.tick();
    fixture.detectChanges();
  }

  async function create(url: string, tree: object | number | null = TREE): Promise<HTMLElement> {
    await router.navigateByUrl(url);
    fixture = TestBed.createComponent(Shell);
    fixture.detectChanges();
    TestBed.tick();
    http()
      .match('/api/v1/repos')
      .forEach((request) => request.flush({ repos: [REPO, SECOND_REPO] }));
    const isRepo = /^\/r\/platform/.test(url);
    if (isRepo && tree !== null) {
      if (typeof tree === 'number') {
        fail('/api/v1/repos/platform/tree', tree, { error: 'upstream_unavailable', message: 'x' });
      } else {
        flush('/api/v1/repos/platform/tree', tree);
      }
    }
    await settle();
    return fixture.nativeElement;
  }

  function press(init: KeyboardEventInit, target: EventTarget = document): KeyboardEvent {
    const event = new KeyboardEvent('keydown', { bubbles: true, cancelable: true, ...init });
    target.dispatchEvent(event);
    return event;
  }

  it('renders the brand, the child route and the settings', async () => {
    const element = await create('/');
    expect(element.querySelector('.brand__name')?.textContent).toBe('Vibefluence');
    expect(element.querySelector('.brand')?.getAttribute('href')).toBe('/');
    expect(element.querySelector('#child')).not.toBeNull();
    expect(element.querySelector('vf-settings-menu')).not.toBeNull();
    expect(element.querySelector('main#content')).not.toBeNull();
  });

  it('lists the repositories in the sidebar on the home page', async () => {
    const element = await create('/');
    const links = [...element.querySelectorAll('.repo-list__item')];
    expect(links.map((link) => link.getAttribute('href'))).toEqual(['/r/platform', '/r/web']);
    expect(element.querySelector('vf-doc-tree')).toBeNull();
  });

  it('shows the document tree for the current repository', async () => {
    const element = await create('/r/platform/docs/guide/start.md');
    expect(element.querySelector('vf-doc-tree')).not.toBeNull();
    expect(element.querySelector('a[aria-current="page"]')?.textContent?.trim()).toBe(
      'Getting started',
    );
    expect(element.querySelector('.repo-list')).toBeNull();
  });

  it('shows a skeleton while the tree loads', async () => {
    const element = await create('/r/platform', null);
    expect(element.querySelectorAll('.skeleton__line').length).toBeGreaterThan(3);
    flush('/api/v1/repos/platform/tree', TREE);
  });

  it('offers a retry when the tree fails to load', async () => {
    const element = await create('/r/platform', 502);
    expect(element.querySelector('.note[role="alert"]')?.textContent).toContain(
      'This repository is unavailable',
    );
    (element.querySelector('.link-button') as HTMLButtonElement).click();
    TestBed.tick();
    flush('/api/v1/repos/platform/tree', TREE);
    await settle();
    expect(element.querySelector('vf-doc-tree')).not.toBeNull();
  });

  it('shows breadcrumbs for a document', async () => {
    const element = await create('/r/platform/docs/guide/start.md');
    const items = [...element.querySelectorAll('.crumbs__item')];
    expect(items.map((item) => item.textContent?.trim())).toEqual([
      'Platform',
      'docs',
      'guide',
      'start.md',
    ]);
    expect(items[0].tagName).toBe('A');
    expect(items[0].getAttribute('href')).toBe('/r/platform');
    expect(items[1].tagName).toBe('SPAN');
    expect(items[3].getAttribute('aria-current')).toBe('page');
  });

  it('shows only the repository on its landing page and nothing on the home page', async () => {
    let element = await create('/r/platform');
    expect(
      [...element.querySelectorAll('.crumbs__item')].map((item) => item.textContent?.trim()),
    ).toEqual(['Platform']);
    fixture.destroy();
    http().match(() => true);

    element = await create('/');
    expect(element.querySelectorAll('.crumbs__item')).toHaveLength(0);
  });

  it('opens the search from the top bar', async () => {
    const element = await create('/');
    (element.querySelector('.search-button') as HTMLButtonElement).click();
    expect(ui.searchOpen()).toBe(true);
  });

  it('opens the search with Ctrl+K and Cmd+K', async () => {
    await create('/');
    const ctrl = press({ key: 'k', ctrlKey: true });
    expect(ctrl.defaultPrevented).toBe(true);
    expect(ui.searchOpen()).toBe(true);

    ui.closeSearch();
    press({ key: 'K', metaKey: true });
    expect(ui.searchOpen()).toBe(true);
  });

  it('opens the search with a slash unless the user is typing', async () => {
    const element = await create('/');
    const field = document.createElement('input');
    element.appendChild(field);
    expect(press({ key: '/' }, field).defaultPrevented).toBe(false);
    expect(ui.searchOpen()).toBe(false);

    const editable = document.createElement('div');
    Object.defineProperty(editable, 'isContentEditable', { value: true });
    element.appendChild(editable);
    expect(press({ key: '/' }, editable).defaultPrevented).toBe(false);

    expect(press({ key: '/' }).defaultPrevented).toBe(true);
    expect(ui.searchOpen()).toBe(true);
  });

  it('ignores a slash combined with a modifier and other keys', async () => {
    await create('/');
    press({ key: '/', ctrlKey: true });
    press({ key: 'a' });
    expect(ui.searchOpen()).toBe(false);
  });

  it('toggles the mobile navigation', async () => {
    const element = await create('/');
    const shell = element.querySelector('.shell') as HTMLElement;
    expect(shell.classList.contains('shell--open')).toBe(false);

    (element.querySelector('.topbar__menu') as HTMLButtonElement).click();
    fixture.detectChanges();
    expect(shell.classList.contains('shell--open')).toBe(true);
    expect(element.querySelector('.topbar__menu')?.getAttribute('aria-expanded')).toBe('true');

    (element.querySelector('.backdrop') as HTMLButtonElement).click();
    fixture.detectChanges();
    expect(shell.classList.contains('shell--open')).toBe(false);
  });

  it('closes the navigation with Escape and after navigating', async () => {
    const element = await create('/');
    const shell = element.querySelector('.shell') as HTMLElement;

    ui.openNav();
    fixture.detectChanges();
    press({ key: 'Escape' });
    fixture.detectChanges();
    expect(shell.classList.contains('shell--open')).toBe(false);

    ui.openNav();
    await router.navigateByUrl('/elsewhere');
    TestBed.tick();
    fixture.detectChanges();
    expect(shell.classList.contains('shell--open')).toBe(false);
  });

  it('does nothing on Escape when the navigation is closed', async () => {
    await create('/');
    press({ key: 'Escape' });
    expect(ui.navOpen()).toBe(false);
  });

  it('moves focus to the content from the skip link', async () => {
    const element = await create('/');
    document.body.appendChild(element);
    const link = element.querySelector('.skip') as HTMLAnchorElement;
    const event = new MouseEvent('click', { bubbles: true, cancelable: true });
    link.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(true);
    expect(document.activeElement?.id).toBe('content');
    element.remove();
  });

  it('labels the sidebar in Russian', async () => {
    TestBed.inject(I18n).setLang('ru');
    const element = await create('/');
    expect(element.querySelector('.sidebar')?.getAttribute('aria-label')).toBe('Документы');
  });
});
