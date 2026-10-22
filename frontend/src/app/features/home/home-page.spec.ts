import { TestBed } from '@angular/core/testing';
import { fail, flush, http, provideApp, REPO, SECOND_REPO } from '../../../testing';
import { I18n } from '../../core/i18n';
import { Ui } from '../../core/ui';
import { HomePage } from './home-page';

describe('HomePage', () => {
  beforeEach(() => {
    localStorage.clear();
    TestBed.configureTestingModule({ imports: [HomePage], providers: provideApp() });
    TestBed.inject(I18n).setLang('en');
  });

  afterEach(() => http().verify());

  async function create(): Promise<{ element: HTMLElement; settle: () => Promise<void> }> {
    const fixture = TestBed.createComponent(HomePage);
    fixture.detectChanges();
    TestBed.tick();
    const settle = async () => {
      await Promise.resolve();
      TestBed.tick();
      fixture.detectChanges();
    };
    return { element: fixture.nativeElement, settle };
  }

  it('shows skeleton cards while loading', async () => {
    const { element } = await create();
    expect(element.querySelectorAll('.card--skeleton')).toHaveLength(3);
    expect(element.querySelector('[role="status"]')?.textContent).toContain('Loading');
    flush('/api/v1/repos', { repos: [] });
  });

  it('renders a card per repository with counts and a link', async () => {
    const { element, settle } = await create();
    flush('/api/v1/repos', { repos: [REPO, SECOND_REPO] });
    await settle();

    const cards = [...element.querySelectorAll('a.card')];
    expect(cards).toHaveLength(2);
    expect(cards[0].getAttribute('href')).toBe('/r/platform');
    expect(cards[0].textContent).toContain('Platform');
    expect(cards[0].textContent).toContain('demo/platform');
    expect(cards[0].textContent).toContain('3 documents');
    expect(cards[0].textContent).toContain('1 draft hidden');
    expect(cards[1].textContent).toContain('1 document');
    expect(cards[1].textContent).not.toContain('hidden');
    expect(element.querySelector('.card--skeleton')).toBeNull();
  });

  it('flags unavailable and stale repositories', async () => {
    const { element, settle } = await create();
    flush('/api/v1/repos', {
      repos: [
        { ...REPO, available: false },
        { ...SECOND_REPO, stale: true },
      ],
    });
    await settle();

    const cards = [...element.querySelectorAll('a.card')];
    expect(cards[0].querySelector('.badge--negative')?.textContent).toContain('Unavailable');
    expect(cards[0].textContent).not.toContain('documents');
    expect(cards[1].querySelector('.badge--warning')?.textContent).toContain('saved data');
  });

  it('explains how to connect repositories when there are none', async () => {
    const { element, settle } = await create();
    flush('/api/v1/repos', { repos: [] });
    await settle();
    expect(element.querySelector('.state__title')?.textContent).toContain(
      'No repositories connected',
    );
    expect(element.querySelector('a.card')).toBeNull();
  });

  it('shows an error with a retry that loads again', async () => {
    const { element, settle } = await create();
    fail('/api/v1/repos', 502, { error: 'upstream_unavailable', message: 'x' });
    await settle();
    expect(element.querySelector('[role="alert"]')?.textContent).toContain(
      'Cannot load repositories',
    );

    (element.querySelector('[role="alert"] button') as HTMLButtonElement).click();
    TestBed.tick();
    flush('/api/v1/repos', { repos: [REPO] });
    await settle();
    expect(element.querySelectorAll('a.card')).toHaveLength(1);
  });

  it('opens the search palette from the hero field', async () => {
    const { element } = await create();
    flush('/api/v1/repos', { repos: [REPO] });
    (element.querySelector('.hero__search') as HTMLButtonElement).click();
    expect(TestBed.inject(Ui).searchOpen()).toBe(true);
  });

  it('sets the page title', async () => {
    await create();
    flush('/api/v1/repos', { repos: [] });
    TestBed.tick();
    expect(document.title).toBe('Vibefluence');
  });

  it('renders in Russian', async () => {
    TestBed.inject(I18n).setLang('ru');
    const { element, settle } = await create();
    flush('/api/v1/repos', { repos: [REPO] });
    await settle();
    expect(element.querySelector('h1')?.textContent).toBe('Ваша документация');
    expect(element.querySelector('a.card')?.textContent).toContain('3 документа');
  });
});
