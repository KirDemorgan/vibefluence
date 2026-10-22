import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { flush, http, provideApp, REPO, SECOND_REPO } from '../../testing';
import { I18n } from '../core/i18n';
import { RepoSwitcher } from './repo-switcher';

@Component({ template: '' })
class Blank {}

describe('RepoSwitcher', () => {
  beforeEach(() => {
    localStorage.clear();
    TestBed.configureTestingModule({
      imports: [RepoSwitcher],
      providers: provideApp([{ path: '**', component: Blank }]),
    });
    TestBed.inject(I18n).setLang('en');
  });

  afterEach(() => http().verify());

  async function create(url = '/') {
    const router = TestBed.inject(Router);
    await router.navigateByUrl(url);
    const fixture = TestBed.createComponent(RepoSwitcher);
    fixture.detectChanges();
    flush('/api/v1/repos', { repos: [REPO, { ...SECOND_REPO, available: false }] });
    await Promise.resolve();
    TestBed.tick();
    fixture.detectChanges();
    return fixture;
  }

  it('asks to choose a repository on the home page', async () => {
    const fixture = await create('/');
    expect(fixture.nativeElement.querySelector('.trigger__name').textContent).toBe(
      'Choose a repository',
    );
  });

  it('shows the current repository with its project', async () => {
    const fixture = await create('/r/platform/docs/a.md');
    const element = fixture.nativeElement as HTMLElement;
    expect(element.querySelector('.trigger__name')?.textContent).toBe('Platform');
    expect(element.querySelector('.trigger__meta')?.textContent).toBe('demo/platform');
  });

  it('lists all repositories and marks the current one', async () => {
    const fixture = await create('/r/platform');
    const options = [...(fixture.nativeElement as HTMLElement).querySelectorAll('.option')];
    expect(options).toHaveLength(2);
    expect(options[0].getAttribute('aria-current')).toBe('true');
    expect(options[0].getAttribute('href')).toBe('/r/platform');
    expect(options[1].getAttribute('aria-current')).toBeNull();
    expect(options[0].textContent).toContain('3 documents');
    expect(options[1].textContent).toContain('Unavailable');
  });

  it('opens the menu and closes it after choosing', async () => {
    const fixture = await create('/');
    const element = fixture.nativeElement as HTMLElement;
    (element.querySelector('.trigger') as HTMLButtonElement).click();
    TestBed.tick();
    fixture.detectChanges();
    expect(element.querySelector('.trigger')?.getAttribute('aria-expanded')).toBe('true');

    (element.querySelector('.option') as HTMLAnchorElement).click();
    TestBed.tick();
    fixture.detectChanges();
    expect(element.querySelector('.trigger')?.getAttribute('aria-expanded')).toBe('false');
  });
});
