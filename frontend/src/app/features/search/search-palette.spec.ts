import { Component } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { http, provideApp, REPO, SECOND_REPO } from '../../../testing';
import { I18n } from '../../core/i18n';
import { SearchResult } from '../../core/models';
import { Ui } from '../../core/ui';
import { SearchPalette } from './search-palette';

@Component({ template: '' })
class Blank {}

const RESULT: SearchResult = {
  repo: 'platform',
  path: 'docs/guide/start.md',
  title: 'Getting started',
  type: 'guide',
  snippet: 'Install the gateway first',
};
const SECOND: SearchResult = {
  repo: 'web',
  path: 'docs/web.md',
  title: 'Web gateway',
  snippet: '',
};

describe('SearchPalette', () => {
  let fixture: ComponentFixture<SearchPalette>;
  let ui: Ui;

  beforeEach(() => {
    vi.useFakeTimers({
      toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'],
    });
    localStorage.clear();
    TestBed.configureTestingModule({
      imports: [SearchPalette],
      providers: provideApp([{ path: '**', component: Blank }]),
    });
    TestBed.inject(I18n).setLang('en');
    ui = TestBed.inject(Ui);
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  async function create(url = '/r/platform', repos = [REPO]): Promise<HTMLElement> {
    await TestBed.inject(Router).navigateByUrl(url);
    fixture = TestBed.createComponent(SearchPalette);
    fixture.detectChanges();
    TestBed.tick();
    http().expectOne('/api/v1/repos').flush({ repos });
    return fixture.nativeElement;
  }

  const dialog = (element: HTMLElement) => element.querySelector('dialog') as HTMLDialogElement;
  const input = (element: HTMLElement) => element.querySelector('input') as HTMLInputElement;

  async function open(element: HTMLElement): Promise<void> {
    ui.openSearch();
    fixture.detectChanges();
    TestBed.tick();
    fixture.detectChanges();
    expect(dialog(element).hasAttribute('open')).toBe(true);
  }

  function type(element: HTMLElement, value: string): void {
    const field = input(element);
    field.value = value;
    field.dispatchEvent(new Event('input'));
    fixture.detectChanges();
    TestBed.tick();
  }

  function searchResponse(results: SearchResult[]): { query: string; results: SearchResult[] } {
    return { query: 'x', results };
  }

  async function runSearch(
    element: HTMLElement,
    value: string,
    results: SearchResult[],
  ): Promise<void> {
    type(element, value);
    await vi.advanceTimersByTimeAsync(300);
    http()
      .expectOne((request) => request.url === '/api/v1/search')
      .flush(searchResponse(results));
    TestBed.tick();
    fixture.detectChanges();
  }

  it('stays closed until requested and opens as a modal dialog', async () => {
    const element = await create();
    expect(dialog(element).hasAttribute('open')).toBe(false);
    await open(element);
  });

  it('closes when the UI state says so', async () => {
    const element = await create();
    await open(element);
    ui.closeSearch();
    fixture.detectChanges();
    TestBed.tick();
    expect(dialog(element).hasAttribute('open')).toBe(false);
  });

  it('shows a hint before anything is typed and ignores very short queries', async () => {
    const element = await create();
    await open(element);
    expect(element.textContent).toContain('Type to search');
    type(element, 'a');
    await vi.advanceTimersByTimeAsync(400);
    http().expectNone((request) => request.url === '/api/v1/search');
  });

  it('searches the current repository after a short pause', async () => {
    const element = await create();
    await open(element);
    type(element, 'gateway');
    expect(element.textContent).toContain('Type to search');
    await vi.advanceTimersByTimeAsync(100);
    http().expectNone((request) => request.url === '/api/v1/search');

    await vi.advanceTimersByTimeAsync(200);
    const request = http().expectOne((candidate) => candidate.url === '/api/v1/search');
    expect(request.request.params.get('q')).toBe('gateway');
    expect(request.request.params.get('repo')).toBe('platform');
    expect(request.request.params.get('limit')).toBe('20');
    fixture.detectChanges();
    expect(element.textContent).toContain('Loading');

    request.flush(searchResponse([RESULT]));
    TestBed.tick();
    fixture.detectChanges();
    expect(element.querySelectorAll('[role="option"]')).toHaveLength(1);
    expect(element.querySelector('.result__title')?.textContent).toContain('Getting started');
    expect(element.querySelector('.result__type')?.textContent).toBe('guide');
    expect(element.querySelector('.result__snippet mark')?.textContent).toBe('gateway');
    expect(element.querySelector('.result__path')?.textContent).not.toContain('Platform');
  });

  it('does not search again for the same text', async () => {
    const element = await create();
    await open(element);
    await runSearch(element, 'gateway', [RESULT]);
    type(element, 'gateway');
    await vi.advanceTimersByTimeAsync(400);
    http().expectNone((request) => request.url === '/api/v1/search');
  });

  it('searches every repository from the home page and names the repository of each hit', async () => {
    const element = await create('/', [REPO, SECOND_REPO]);
    await open(element);
    expect(element.querySelector('.scopes')).toBeNull();

    type(element, 'gateway');
    await vi.advanceTimersByTimeAsync(300);
    const request = http().expectOne((candidate) => candidate.url === '/api/v1/search');
    expect(request.request.params.has('repo')).toBe(false);
    request.flush(searchResponse([RESULT, SECOND]));
    TestBed.tick();
    fixture.detectChanges();

    const paths = [...element.querySelectorAll('.result__path')].map((path) =>
      path.textContent?.replace(/\s+/g, ' ').trim(),
    );
    expect(paths).toEqual(['Platform · docs/guide/start.md', 'Web · docs/web.md']);
  });

  it('switches the scope between this repository and all', async () => {
    const element = await create();
    await open(element);
    const scopes = [...element.querySelectorAll<HTMLButtonElement>('.scope')];
    expect(scopes.map((scope) => scope.getAttribute('aria-checked'))).toEqual(['true', 'false']);

    await runSearch(element, 'gateway', [RESULT]);
    scopes[1].click();
    fixture.detectChanges();
    TestBed.tick();
    await vi.advanceTimersByTimeAsync(300);
    const request = http().expectOne((candidate) => candidate.url === '/api/v1/search');
    expect(request.request.params.has('repo')).toBe(false);
    request.flush(searchResponse([]));
  });

  it('cancels an outdated request when the query changes', async () => {
    const element = await create();
    await open(element);
    type(element, 'first');
    await vi.advanceTimersByTimeAsync(300);
    const first = http().expectOne((candidate) => candidate.url === '/api/v1/search');

    type(element, 'second');
    await vi.advanceTimersByTimeAsync(300);
    const second = http().expectOne((candidate) => candidate.url === '/api/v1/search');
    expect(first.cancelled).toBe(true);
    second.flush(searchResponse([RESULT]));
    TestBed.tick();
    fixture.detectChanges();
    expect(element.querySelectorAll('[role="option"]')).toHaveLength(1);
  });

  it('clears the results when the query becomes too short', async () => {
    const element = await create();
    await open(element);
    await runSearch(element, 'gateway', [RESULT]);
    type(element, 'g');
    fixture.detectChanges();
    expect(element.querySelectorAll('[role="option"]')).toHaveLength(0);
    expect(element.textContent).toContain('Type to search');
  });

  it('says when nothing was found', async () => {
    const element = await create();
    await open(element);
    await runSearch(element, ' nothing ', []);
    expect(element.textContent).toContain('Nothing found for “nothing”');
  });

  it('reports a failed search', async () => {
    const element = await create();
    await open(element);
    type(element, 'gateway');
    await vi.advanceTimersByTimeAsync(300);
    http()
      .expectOne((candidate) => candidate.url === '/api/v1/search')
      .flush({}, { status: 502, statusText: 'bad' });
    TestBed.tick();
    fixture.detectChanges();
    expect(element.querySelector('[role="alert"]')?.textContent).toContain('Search is unavailable');
    expect(element.querySelectorAll('[role="option"]')).toHaveLength(0);
  });

  it('moves through results with the arrow keys and wraps around', async () => {
    const element = await create('/', [REPO, SECOND_REPO]);
    await open(element);
    await runSearch(element, 'gateway', [RESULT, SECOND]);
    const selected = () =>
      [...element.querySelectorAll('[role="option"]')].map((option) =>
        option.getAttribute('aria-selected'),
      );
    const press = (key: string) => {
      input(element).dispatchEvent(new KeyboardEvent('keydown', { key, cancelable: true }));
      fixture.detectChanges();
    };

    expect(selected()).toEqual(['true', 'false']);
    press('ArrowDown');
    expect(selected()).toEqual(['false', 'true']);
    press('ArrowDown');
    expect(selected()).toEqual(['true', 'false']);
    press('ArrowUp');
    expect(selected()).toEqual(['false', 'true']);
    expect(input(element).getAttribute('aria-activedescendant')).toBe('vf-search-option-1');
  });

  it('opens the active result with Enter', async () => {
    const element = await create('/', [REPO, SECOND_REPO]);
    const navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
    await open(element);
    await runSearch(element, 'gateway', [RESULT, SECOND]);
    input(element).dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown' }));
    fixture.detectChanges();
    input(element).dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter', cancelable: true }));
    expect(navigate).toHaveBeenCalledWith(['/r', 'web', 'docs', 'web.md']);
    expect(ui.searchOpen()).toBe(false);
  });

  it('ignores Enter and arrows without results', async () => {
    const element = await create();
    const navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
    await open(element);
    input(element).dispatchEvent(new KeyboardEvent('keydown', { key: 'Enter' }));
    input(element).dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowDown' }));
    expect(navigate).not.toHaveBeenCalled();
  });

  it('opens a result on click and follows the mouse with the selection', async () => {
    const element = await create('/', [REPO, SECOND_REPO]);
    const navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
    await open(element);
    await runSearch(element, 'gateway', [RESULT, SECOND]);
    const options = element.querySelectorAll<HTMLElement>('[role="option"]');
    options[1].dispatchEvent(new MouseEvent('mouseenter'));
    fixture.detectChanges();
    expect(options[1].getAttribute('aria-selected')).toBe('true');
    options[0].click();
    expect(navigate).toHaveBeenCalledWith(['/r', 'platform', 'docs', 'guide', 'start.md']);
  });

  it('closes from the button, the native close event and the backdrop', async () => {
    const element = await create();
    await open(element);
    (element.querySelector('.field__close') as HTMLButtonElement).click();
    expect(ui.searchOpen()).toBe(false);

    ui.openSearch();
    fixture.detectChanges();
    TestBed.tick();
    dialog(element).dispatchEvent(new Event('close'));
    expect(ui.searchOpen()).toBe(false);

    ui.openSearch();
    fixture.detectChanges();
    TestBed.tick();
    (element.querySelector('.panel') as HTMLElement).click();
    expect(ui.searchOpen()).toBe(true);
    dialog(element).click();
    expect(ui.searchOpen()).toBe(false);
  });

  it('resets its state each time it opens', async () => {
    const element = await create();
    await open(element);
    await runSearch(element, 'gateway', [RESULT]);
    ui.closeSearch();
    fixture.detectChanges();
    TestBed.tick();

    ui.openSearch();
    fixture.detectChanges();
    TestBed.tick();
    fixture.detectChanges();
    expect(input(element).value).toBe('');
    expect(element.querySelectorAll('[role="option"]')).toHaveLength(0);
  });
});
