import { TestBed } from '@angular/core/testing';
import { fail, flush, http, provideApp, REPO, SECOND_REPO } from '../../../testing';
import { I18n } from '../../core/i18n';
import { DraftSummary } from '../../core/models';
import { Toasts } from '../../core/toast';
import { DraftsPage } from './drafts-page';

const draft = (overrides: Partial<DraftSummary> = {}): DraftSummary => ({
  id: '0123456789abcdef',
  repo: 'platform',
  path: 'docs/plans/rollout.md',
  title: 'Rollout plan',
  status: 'draft',
  isNew: true,
  author: 'Alex',
  updatedAt: new Date(Date.now() - 3 * 3600 * 1000).toISOString(),
  baseBlobId: '',
  ...overrides,
});

describe('DraftsPage', () => {
  const toasts = { success: vi.fn(), error: vi.fn() };

  beforeEach(() => {
    localStorage.clear();
    toasts.success.mockReset();
    toasts.error.mockReset();
    TestBed.configureTestingModule({
      imports: [DraftsPage],
      providers: provideApp([], { provide: Toasts, useValue: toasts }),
    });
    TestBed.inject(I18n).setLang('en');
  });

  afterEach(() => http().verify());

  async function create(
    drafts: DraftSummary[] | number | null,
  ): Promise<{ element: HTMLElement; settle: () => Promise<void> }> {
    const fixture = TestBed.createComponent(DraftsPage);
    fixture.detectChanges();
    TestBed.tick();
    const settle = async () => {
      await Promise.resolve();
      TestBed.tick();
      fixture.detectChanges();
    };
    http()
      .match('/api/v1/repos')
      .forEach((request) => request.flush({ repos: [REPO, SECOND_REPO] }));
    if (typeof drafts === 'number') {
      fail('/api/v1/drafts', drafts, { error: 'internal_error', message: 'x' });
    } else if (drafts) {
      flush('/api/v1/drafts', { drafts });
    }
    await settle();
    return { element: fixture.nativeElement, settle };
  }

  it('shows a skeleton while loading', async () => {
    const { element } = await create(null);
    expect(element.querySelectorAll('.skeleton__row')).toHaveLength(3);
    flush('/api/v1/drafts', { drafts: [] });
  });

  it('explains that there are no drafts', async () => {
    const { element } = await create([]);
    expect(element.querySelector('.state__title')?.textContent).toBe('No drafts');
    expect(element.querySelector('.list')).toBeNull();
  });

  it('groups drafts by repository with their details', async () => {
    const { element } = await create([
      draft(),
      draft({
        id: 'f'.repeat(16),
        repo: 'web',
        path: 'docs/web.md',
        title: '',
        isNew: false,
        status: 'published',
        mergeRequestUrl: 'https://gl/mr/4',
      }),
    ]);
    expect(
      [...element.querySelectorAll('.group__title')].map((title) => title.textContent),
    ).toEqual(['Platform', 'Web']);
    const items = [...element.querySelectorAll('.item')];
    expect(items[0].querySelector('.item__title')?.textContent).toBe('Rollout plan');
    expect(items[0].querySelector('.item__path')?.textContent).toBe('docs/plans/rollout.md');
    expect(items[0].textContent).toContain('New');
    expect(items[0].textContent).toContain('Draft');
    expect(items[0].textContent).toContain('3 hours ago');
    expect(items[0].querySelector('.item__mr')).toBeNull();
    expect(items[0].querySelector('a[href="/edit/platform/docs/plans/rollout.md"]')).not.toBeNull();

    expect(items[1].querySelector('.item__title')?.textContent).toBe('docs/web.md');
    expect(items[1].textContent).toContain('Will be published');
    expect(items[1].textContent).not.toContain('New');
    expect(items[1].querySelector('.item__mr')?.getAttribute('href')).toBe('https://gl/mr/4');
  });

  it('offers to start a new document in the first repository', async () => {
    const { element } = await create([]);
    expect(element.querySelector('.head a')?.getAttribute('href')).toBe('/new/platform');
  });

  it('deletes a draft after a second click', async () => {
    const { element, settle } = await create([draft()]);
    (element.querySelector('.item__actions button') as HTMLButtonElement).click();
    await settle();
    const [confirm, cancel] = [
      ...element.querySelectorAll<HTMLButtonElement>('.item__actions button'),
    ];
    expect(confirm.textContent).toContain('Delete this draft?');
    expect(cancel.textContent).toContain('Cancel');

    confirm.click();
    const removal = http().expectOne('/api/v1/repos/platform/drafts/0123456789abcdef');
    expect(removal.request.method).toBe('DELETE');
    removal.flush(null, { status: 204, statusText: 'No Content' });
    TestBed.tick();
    expect(toasts.success).toHaveBeenCalledWith('Draft deleted');
    flush('/api/v1/drafts', { drafts: [] });
    await settle();
    expect(element.querySelector('.state__title')?.textContent).toBe('No drafts');
  });

  it('can cancel the deletion', async () => {
    const { element, settle } = await create([draft()]);
    (element.querySelector('.item__actions button') as HTMLButtonElement).click();
    await settle();
    (element.querySelectorAll('.item__actions button')[1] as HTMLButtonElement).click();
    await settle();
    expect(element.querySelectorAll('.item__actions button')).toHaveLength(1);
    http().expectNone((request) => request.method === 'DELETE');
  });

  it('reports a failed deletion', async () => {
    const { element, settle } = await create([draft()]);
    (element.querySelector('.item__actions button') as HTMLButtonElement).click();
    await settle();
    (element.querySelectorAll('.item__actions button')[0] as HTMLButtonElement).click();
    http()
      .expectOne('/api/v1/repos/platform/drafts/0123456789abcdef')
      .flush({}, { status: 500, statusText: 'x' });
    await settle();
    expect(toasts.error).toHaveBeenCalledWith('Could not delete the draft');
    expect(element.querySelectorAll('.item__actions button')).toHaveLength(1);
  });

  it('shows an error with a retry', async () => {
    const { element, settle } = await create(500);
    expect(element.querySelector('[role="alert"]')?.textContent).toContain('Cannot load drafts');
    (element.querySelector('[role="alert"] button') as HTMLButtonElement).click();
    TestBed.tick();
    flush('/api/v1/drafts', { drafts: [draft()] });
    await settle();
    expect(element.querySelectorAll('.item')).toHaveLength(1);
  });

  it('sets the page title', async () => {
    await create([]);
    expect(document.title).toBe('Drafts · Vibefluence');
  });
});
