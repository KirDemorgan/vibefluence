import { Component } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { doc, fail, flush, http, provideApp, REPO } from '../../../testing';
import { docMatcher } from '../../app.routes';
import { I18n } from '../../core/i18n';
import { DocPage } from './doc-page';

@Component({ template: '' })
class Blank {}

const URL = '/api/v1/repos/platform/doc?path=docs/guide/start.md';

describe('DocPage', () => {
  let fixture: ComponentFixture<DocPage>;
  let existingDrafts: object[];
  let observerCallbacks: IntersectionObserverCallback[];

  beforeEach(() => {
    localStorage.clear();
    existingDrafts = [];
    observerCallbacks = [];
    vi.stubGlobal(
      'IntersectionObserver',
      class {
        constructor(callback: IntersectionObserverCallback) {
          observerCallbacks.push(callback);
        }
        observe = vi.fn();
        unobserve = vi.fn();
        disconnect = vi.fn();
      },
    );
    TestBed.configureTestingModule({
      imports: [DocPage],
      providers: provideApp([
        { matcher: docMatcher, component: Blank },
        { path: '**', component: Blank },
      ]),
    });
    TestBed.inject(I18n).setLang('en');
  });

  afterEach(() => {
    http().verify();
    vi.unstubAllGlobals();
    vi.restoreAllMocks();
  });

  async function settle(): Promise<void> {
    await Promise.resolve();
    TestBed.tick();
    fixture.detectChanges();
    TestBed.tick();
  }

  async function create(
    response: object | number | null = doc(),
    url = '/r/platform/docs/guide/start.md',
  ): Promise<HTMLElement> {
    await TestBed.inject(Router).navigateByUrl(url);
    fixture = TestBed.createComponent(DocPage);
    fixture.componentRef.setInput('repo', 'platform');
    fixture.componentRef.setInput('path', 'docs/guide/start.md');
    fixture.detectChanges();
    TestBed.tick();
    http()
      .match('/api/v1/repos')
      .forEach((request) => request.flush({ repos: [REPO] }));
    http()
      .match('/api/v1/drafts')
      .forEach((request) => request.flush({ drafts: existingDrafts }));
    if (typeof response === 'number') {
      fail(
        URL,
        response,
        response === 404
          ? { error: 'not_found', message: 'x' }
          : { error: 'upstream_unavailable', message: 'x' },
      );
    } else if (response !== null) {
      flush(URL, response);
    }
    await settle();
    return fixture.nativeElement;
  }

  it('shows a skeleton while loading', async () => {
    const element = await create(null);
    expect(element.querySelectorAll('.skeleton').length).toBeGreaterThan(3);
    expect(element.querySelector('[role="status"]')?.textContent).toContain('Loading');
    flush(URL, doc());
  });

  it('renders the document HTML and metadata', async () => {
    const element = await create();
    expect(element.querySelector('.vf-prose h1')?.textContent).toBe('Getting started');
    expect(element.querySelector('.meta .chip')?.textContent).toBe('guide');
    expect(element.querySelector('.meta__path')?.textContent).toBe('docs/guide/start.md');
    expect(element.querySelector('.meta__link')?.getAttribute('href')).toContain(
      '/-/blob/master/docs/guide/start.md',
    );
    expect(element.querySelector('.meta__link')?.getAttribute('rel')).toBe('noopener noreferrer');
    expect(document.title).toBe('Getting started · Platform · Vibefluence');
  });

  it('does not repeat the title when the document starts with its own heading', async () => {
    const element = await create();
    expect(element.querySelector('h1.title')).toBeNull();
  });

  it('shows the title itself when the document has no level one heading', async () => {
    const element = await create(
      doc({
        html: '<p>body</p><h2 id="h-a">A</h2><h2 id="h-b">B</h2>',
        headings: [
          { level: 2, text: 'A', id: 'h-a' },
          { level: 2, text: 'B', id: 'h-b' },
        ],
      }),
    );
    expect(element.querySelector('h1.title')?.textContent).toBe('Getting started');
  });

  it('lists second and third level headings in the outline', async () => {
    const element = await create();
    const links = [...element.querySelectorAll('.toc__link')];
    expect(links.map((link) => link.textContent?.trim())).toEqual(['Install', 'Usage', 'Flags']);
    expect(links[2].classList.contains('toc__link--sub')).toBe(true);
    expect(links[0].classList.contains('toc__link--active')).toBe(true);
  });

  it('hides the outline for short documents', async () => {
    const element = await create(
      doc({
        headings: [
          { level: 1, text: 'T', id: 'h-t' },
          { level: 2, text: 'Only', id: 'h-only' },
        ],
      }),
    );
    expect(element.querySelector('.toc')).toBeNull();
  });

  it('scrolls to a heading from the outline and marks it active', async () => {
    const element = await create();
    const scroll = vi.spyOn(Element.prototype, 'scrollIntoView');
    const link = element.querySelectorAll<HTMLAnchorElement>('.toc__link')[1];
    const event = new MouseEvent('click', { bubbles: true, cancelable: true });
    link.dispatchEvent(event);
    fixture.detectChanges();
    expect(event.defaultPrevented).toBe(true);
    expect(scroll).toHaveBeenCalled();
    expect(link.classList.contains('toc__link--active')).toBe(true);
  });

  it('follows the heading that scrolls into view', async () => {
    const element = await create();
    const heading = element.querySelector('#h-usage') as HTMLElement;
    observerCallbacks.at(-1)?.(
      [{ isIntersecting: true, target: heading } as unknown as IntersectionObserverEntry],
      {} as IntersectionObserver,
    );
    fixture.detectChanges();
    const active = element.querySelector('.toc__link--active');
    expect(active?.textContent?.trim()).toBe('Usage');

    observerCallbacks.at(-1)?.(
      [
        {
          isIntersecting: false,
          target: element.querySelector('#h-flags'),
        } as unknown as IntersectionObserverEntry,
      ],
      {} as IntersectionObserver,
    );
    fixture.detectChanges();
    expect(element.querySelector('.toc__link--active')?.textContent?.trim()).toBe('Usage');
  });

  it('scrolls to the fragment of the URL after rendering', async () => {
    const scroll = vi.spyOn(Element.prototype, 'scrollIntoView');
    await create(doc(), '/r/platform/docs/guide/start.md#h-usage');
    expect(scroll).toHaveBeenCalled();
  });

  it('ignores a fragment that matches nothing', async () => {
    const scroll = vi.spyOn(Element.prototype, 'scrollIntoView');
    await create(doc(), '/r/platform/docs/guide/start.md#h-missing');
    expect(scroll).not.toHaveBeenCalled();
  });

  describe('links inside the document', () => {
    const html =
      '<h1 id="h-t">T</h1><p><a id="internal" href="/r/platform/docs/README.md">readme</a> <a id="hash" href="#h-install">install</a> ' +
      '<a id="external" href="https://example.com" target="_blank">ext</a> <a id="empty">no href</a> <span id="text">text</span></p><h2 id="h-install">Install</h2><h2 id="h-b">B</h2>';

    async function setup(): Promise<{
      element: HTMLElement;
      navigate: ReturnType<typeof vi.spyOn>;
    }> {
      const element = await create(doc({ html }));
      const navigate = vi.spyOn(TestBed.inject(Router), 'navigateByUrl').mockResolvedValue(true);
      return { element, navigate: navigate as unknown as ReturnType<typeof vi.spyOn> };
    }

    function click(element: HTMLElement, selector: string, init: MouseEventInit = {}): MouseEvent {
      const event = new MouseEvent('click', {
        bubbles: true,
        cancelable: true,
        button: 0,
        ...init,
      });
      element.querySelector(selector)?.dispatchEvent(event);
      return event;
    }

    it('navigates internal links inside the application', async () => {
      const { element, navigate } = await setup();
      const event = click(element, '#internal');
      expect(event.defaultPrevented).toBe(true);
      expect(navigate).toHaveBeenCalledWith('/r/platform/docs/README.md');
    });

    it('scrolls to in-page anchors instead of reloading', async () => {
      const { element, navigate } = await setup();
      const scroll = vi.spyOn(Element.prototype, 'scrollIntoView');
      const event = click(element, '#hash');
      expect(event.defaultPrevented).toBe(true);
      expect(scroll).toHaveBeenCalled();
      expect(navigate).not.toHaveBeenCalled();
    });

    it('leaves external links to the browser', async () => {
      const { element, navigate } = await setup();
      const event = click(element, '#external');
      expect(event.defaultPrevented).toBe(false);
      expect(navigate).not.toHaveBeenCalled();
    });

    it('lets the browser handle modified and non-primary clicks', async () => {
      const { element, navigate } = await setup();
      for (const init of [
        { ctrlKey: true },
        { metaKey: true },
        { shiftKey: true },
        { altKey: true },
        { button: 1 },
      ]) {
        expect(click(element, '#internal', init).defaultPrevented).toBe(false);
      }
      expect(navigate).not.toHaveBeenCalled();
    });

    it('ignores clicks that are not on links', async () => {
      const { element, navigate } = await setup();
      expect(click(element, '#text').defaultPrevented).toBe(false);
      expect(click(element, '#empty').defaultPrevented).toBe(false);
      expect(navigate).not.toHaveBeenCalled();
    });
  });

  it('shows a stale warning', async () => {
    const element = await create(doc({ stale: true }));
    expect(element.querySelector('.banner[role="status"]')?.textContent).toContain('saved copy');
  });

  it('lists problems found in the document', async () => {
    const element = await create(doc({ warnings: ['front matter is not valid YAML', 'another'] }));
    const banner = element.querySelector('.banner[role="alert"]') as HTMLElement;
    expect(banner.textContent).toContain('This document has problems');
    expect([...banner.querySelectorAll('li')].map((item) => item.textContent)).toEqual([
      'front matter is not valid YAML',
      'another',
    ]);
  });

  it('omits the GitLab link and type chip when unknown', async () => {
    const element = await create(doc({ webUrl: '', type: undefined }));
    expect(element.querySelector('.meta__link')).toBeNull();
    expect(element.querySelector('.meta .chip')).toBeNull();
  });

  it('explains a missing document and links back to the repository', async () => {
    const element = await create(404);
    expect(element.querySelector('h1')?.textContent?.trim()).toBe('Document not found');
    expect(element.querySelector('a')?.getAttribute('href')).toBe('/r/platform');
    expect(element.querySelector('button')).toBeNull();
  });

  it('shows other failures with a retry that loads again', async () => {
    const element = await create(502);
    expect(element.querySelector('h1')?.textContent?.trim()).toBe('Cannot load the document');
    (element.querySelector('button') as HTMLButtonElement).click();
    TestBed.tick();
    flush(URL, doc());
    await settle();
    expect(element.querySelector('.vf-prose h1')?.textContent).toBe('Getting started');
  });

  it('renders the outline label in Russian', async () => {
    TestBed.inject(I18n).setLang('ru');
    const element = await create();
    expect(element.querySelector('.toc__title')?.textContent).toBe('На этой странице');
  });

  it('offers to edit the document', async () => {
    const element = await create();
    const edit = element.querySelector('.meta__actions a') as HTMLAnchorElement;
    expect(edit.textContent).toContain('Edit');
    expect(edit.getAttribute('href')).toBe('/edit/platform/docs/guide/start.md');
    expect(element.querySelector('.banner--info')).toBeNull();
  });

  it('points to an existing draft of the document', async () => {
    existingDrafts = [
      {
        id: '0123456789abcdef',
        repo: 'platform',
        path: 'docs/guide/start.md',
        title: 'x',
        status: 'draft',
        isNew: false,
        author: '',
        updatedAt: '2026-10-22T12:00:00Z',
        baseBlobId: 'abc',
      },
    ];
    const element = await create();
    const banner = element.querySelector('.banner--info') as HTMLElement;
    expect(banner.textContent).toContain('draft of this document');
    expect(banner.querySelector('a')?.getAttribute('href')).toBe(
      '/edit/platform/docs/guide/start.md',
    );
  });
});
