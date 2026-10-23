import { Component } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { doc, fail, flush, http, provideApp, REPO } from '../../../testing';
import { Author } from '../../core/author';
import { I18n } from '../../core/i18n';
import { Draft, PreviewResponse, PublishResult } from '../../core/models';
import { EditorPage } from './editor-page';

@Component({ template: '' })
class Blank {}

const PREVIEW: PreviewResponse = {
  title: 'T',
  status: 'draft',
  warnings: [],
  html: '<h1>T</h1>',
  headings: [],
};

function draftOf(overrides: Partial<Draft> = {}): Draft {
  return {
    id: '0123456789abcdef',
    repo: 'platform',
    path: 'docs/a.md',
    title: 'A',
    status: 'draft',
    isNew: false,
    author: 'Alex',
    updatedAt: '2026-10-22T12:00:00Z',
    baseBlobId: 'base',
    content: '---\ntitle: "A"\nstatus: draft\n---\n# A\n',
    ...overrides,
  };
}

function echo(request: { request: { body: unknown } }, overrides: Partial<Draft> = {}): Draft {
  const body = request.request.body as { path: string; content: string; baseBlobId: string };
  return draftOf({
    path: body.path,
    content: body.content,
    baseBlobId: body.baseBlobId,
    isNew: body.baseBlobId === '',
    ...overrides,
  });
}

const MERGED: PublishResult = {
  outcome: 'merged',
  created: true,
  path: 'docs/new-doc.md',
  draftRemoved: true,
  mergeRequestUrl: 'https://gl/mr/1',
};

describe('EditorPage', () => {
  let fixture: ComponentFixture<EditorPage>;

  beforeEach(() => {
    vi.useFakeTimers({
      toFake: ['setTimeout', 'clearTimeout', 'setInterval', 'clearInterval', 'Date'],
    });
    localStorage.clear();
    TestBed.configureTestingModule({
      imports: [EditorPage],
      providers: provideApp([{ path: '**', component: Blank }]),
    });
    TestBed.inject(I18n).setLang('en');
  });

  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  async function settle(): Promise<void> {
    await vi.advanceTimersByTimeAsync(0);
    TestBed.tick();
    fixture.detectChanges();
    TestBed.tick();
  }

  function answerPreviews(): void {
    http()
      .match((request) => request.url.endsWith('/preview'))
      .forEach((request) => request.flush(PREVIEW));
  }

  async function openNew(repo: object = REPO): Promise<HTMLElement> {
    fixture = TestBed.createComponent(EditorPage);
    fixture.componentRef.setInput('repo', 'platform');
    fixture.detectChanges();
    TestBed.tick();
    http()
      .match('/api/v1/repos')
      .forEach((request) => request.flush({ repos: [repo] }));
    await settle();
    return fixture.nativeElement;
  }

  async function openPath(
    path: string,
    options: { drafts?: object[]; document?: object | number; draft?: Draft } = {},
  ): Promise<HTMLElement> {
    fixture = TestBed.createComponent(EditorPage);
    fixture.componentRef.setInput('repo', 'platform');
    fixture.componentRef.setInput('path', path);
    fixture.detectChanges();
    TestBed.tick();
    http()
      .match('/api/v1/repos')
      .forEach((request) => request.flush({ repos: [REPO] }));
    flush('/api/v1/repos/platform/drafts', { drafts: options.drafts ?? [] });
    if (options.draft) {
      flush(`/api/v1/repos/platform/drafts/${options.draft.id}`, options.draft);
    } else if (typeof options.document === 'number') {
      fail(`/api/v1/repos/platform/doc?path=${path}`, options.document, {
        error: options.document === 404 ? 'not_found' : 'upstream_unavailable',
        message: 'x',
      });
    } else if (!options.drafts?.length) {
      flush(
        `/api/v1/repos/platform/doc?path=${path}`,
        options.document ??
          doc({ path, raw: '---\ntitle: "Start"\nstatus: published\n---\n# Start\n\ntext\n' }),
      );
    }
    await settle();
    return fixture.nativeElement;
  }

  const query = <T extends HTMLElement>(element: HTMLElement, selector: string) =>
    element.querySelector(selector) as T;

  function typeInto(element: HTMLElement, selector: string, value: string): void {
    const field = query<HTMLInputElement>(element, selector);
    field.value = value;
    field.dispatchEvent(new Event('input'));
    fixture.detectChanges();
    TestBed.tick();
  }

  function edit(element: HTMLElement, value: string): void {
    typeInto(element, 'textarea.source', value);
  }

  async function pause(ms: number): Promise<void> {
    await vi.advanceTimersByTimeAsync(ms);
    TestBed.tick();
    fixture.detectChanges();
  }

  describe('a new document', () => {
    it('starts as an empty draft', async () => {
      const element = await openNew();
      answerPreviews();
      expect(element.querySelector('h1.heading')?.textContent?.trim()).toBe('New document');
      const source = query<HTMLTextAreaElement>(element, 'textarea.source').value;
      expect(source).toContain('status: draft');
      expect(source).toContain('# ');
      const segments = [...element.querySelectorAll('.segment')].map((segment) =>
        segment.getAttribute('aria-checked'),
      );
      expect(segments).toEqual(['true', 'false']);
      expect(
        query<HTMLButtonElement>(
          element,
          'button[iconStart="@tui.send"], .bar__actions button:last-child',
        ).disabled,
      ).toBe(true);
    });

    it('derives the path from the title until the path is edited by hand', async () => {
      const element = await openNew();
      typeInto(element, '.field--title input', 'Rollout Plan');
      expect(query<HTMLInputElement>(element, '#vf-path').value).toBe('docs/rollout-plan.md');
      expect(query<HTMLTextAreaElement>(element, 'textarea.source').value).toContain(
        'title: "Rollout Plan"',
      );
      expect(query<HTMLTextAreaElement>(element, 'textarea.source').value).toContain(
        '# Rollout Plan',
      );

      typeInto(element, '#vf-path', 'docs/custom/name.md');
      typeInto(element, '.field--title input', 'Another Title');
      expect(query<HTMLInputElement>(element, '#vf-path').value).toBe('docs/custom/name.md');
    });

    it('clears the path again when the title is removed', async () => {
      const element = await openNew();
      typeInto(element, '.field--title input', 'Plan');
      typeInto(element, '.field--title input', '');
      expect(query<HTMLInputElement>(element, '#vf-path').value).toBe('');
    });

    it('does not overwrite a body the user has written', async () => {
      const element = await openNew();
      edit(element, '---\nstatus: draft\n---\n\n# My own heading\n\nreal text\n');
      typeInto(element, '.field--title input', 'Later title');
      expect(query<HTMLTextAreaElement>(element, 'textarea.source').value).toContain(
        '# My own heading',
      );
    });

    it('fills a template when a type is chosen on an untouched body', async () => {
      const element = await openNew();
      typeInto(element, '.field--title input', 'Auth rework');
      const select = query<HTMLSelectElement>(element, 'select');
      select.value = 'adr';
      select.dispatchEvent(new Event('change'));
      fixture.detectChanges();
      const source = query<HTMLTextAreaElement>(element, 'textarea.source').value;
      expect(source).toContain('type: "adr"');
      expect(source).toContain('## Decision');
      expect(source).toContain('# Auth rework');
    });

    it('keeps a written body when the type changes', async () => {
      const element = await openNew();
      edit(element, '---\nstatus: draft\n---\n\n# Mine\n\nwritten\n');
      const select = query<HTMLSelectElement>(element, 'select');
      select.value = 'spec';
      select.dispatchEvent(new Event('change'));
      fixture.detectChanges();
      const source = query<HTMLTextAreaElement>(element, 'textarea.source').value;
      expect(source).toContain('type: "spec"');
      expect(source).toContain('written');
      expect(source).not.toContain('## Requirements');
    });

    it('removes the type again', async () => {
      const element = await openNew();
      edit(element, '---\ntype: "adr"\nstatus: draft\n---\n\n# Mine\n\ntext\n');
      const select = query<HTMLSelectElement>(element, 'select');
      select.value = '';
      select.dispatchEvent(new Event('change'));
      fixture.detectChanges();
      expect(query<HTMLTextAreaElement>(element, 'textarea.source').value).not.toContain('type:');
    });

    it('toggles the status in the front matter', async () => {
      const element = await openNew();
      const segments = element.querySelectorAll<HTMLButtonElement>('.segment');
      segments[1].click();
      fixture.detectChanges();
      expect(query<HTMLTextAreaElement>(element, 'textarea.source').value).toContain(
        'status: published',
      );
      expect(segments[1].getAttribute('aria-checked')).toBe('true');
      expect(element.querySelector('.status-hint')?.textContent).toContain(
        'merge request into master',
      );
      segments[0].click();
      fixture.detectChanges();
      expect(element.querySelector('.status-hint')?.textContent).toContain(
        'Saved on this server only',
      );
    });

    it('validates the path', async () => {
      const element = await openNew();
      typeInto(element, '#vf-path', 'elsewhere/a.md');
      expect(query(element, '#vf-path').getAttribute('aria-invalid')).toBe('true');
      expect(element.querySelector('.field__hint--error')?.textContent).toContain('inside docs');
      typeInto(element, '#vf-path', 'docs/a.md');
      expect(element.querySelector('.field__hint--error')).toBeNull();
    });

    it('cannot be saved or published without a valid path', async () => {
      const element = await openNew();
      const [save, publish] = [
        ...element.querySelectorAll<HTMLButtonElement>('.bar__actions button'),
      ];
      expect(save.disabled).toBe(true);
      expect(publish.disabled).toBe(true);
      typeInto(element, '#vf-path', 'docs/a.md');
      expect(save.disabled).toBe(false);
      expect(publish.disabled).toBe(true);
    });
  });

  describe('previewing', () => {
    it('renders the server preview after a pause', async () => {
      const element = await openNew();
      answerPreviews();
      edit(element, '# Hi\n');
      await pause(300);
      http().expectNone((request) => request.url.endsWith('/preview'));
      await pause(200);
      const request = http().expectOne('/api/v1/repos/platform/preview');
      expect(request.request.body).toMatchObject({ content: '# Hi\n' });
      request.flush({ ...PREVIEW, html: '<h1>Hi</h1><p><a href="/r/x">link</a></p>' });
      await settle();
      expect(element.querySelector('.preview h1')?.textContent).toBe('Hi');
    });

    it('shows problems found in the document header', async () => {
      const element = await openNew();
      edit(element, '---\ntitle: [oops\n---\nx');
      await pause(500);
      http()
        .expectOne('/api/v1/repos/platform/preview')
        .flush({ ...PREVIEW, warnings: ['front matter is not valid YAML'] });
      await settle();
      expect(element.querySelector('.banner--warning')?.textContent).toContain(
        'front matter is not valid YAML',
      );
    });

    it('keeps the last preview when a request fails', async () => {
      const element = await openNew();
      edit(element, '# One\n');
      await pause(500);
      http()
        .expectOne('/api/v1/repos/platform/preview')
        .flush({ ...PREVIEW, html: '<h1>One</h1>' });
      await settle();
      edit(element, '# Two\n');
      await pause(500);
      http()
        .expectOne('/api/v1/repos/platform/preview')
        .flush({}, { status: 500, statusText: 'x' });
      await settle();
      expect(element.querySelector('.preview h1')?.textContent).toBe('One');
    });

    it('does not follow links inside the preview', async () => {
      const element = await openNew();
      edit(element, '[x](y)');
      await pause(500);
      http()
        .expectOne('/api/v1/repos/platform/preview')
        .flush({ ...PREVIEW, html: '<p><a href="/r/x">link</a></p>' });
      await settle();
      const event = new MouseEvent('click', { bubbles: true, cancelable: true });
      element.querySelector('.preview a')?.dispatchEvent(event);
      expect(event.defaultPrevented).toBe(true);
      const outside = new MouseEvent('click', { bubbles: true, cancelable: true });
      element.querySelector('.bar')?.dispatchEvent(outside);
      expect(outside.defaultPrevented).toBe(false);
    });

    it('switches between writing and previewing on small screens', async () => {
      const element = await openNew();
      const tabs = element.querySelectorAll<HTMLButtonElement>('.tab');
      expect(query(element, '.split').getAttribute('data-view')).toBe('write');
      tabs[1].click();
      fixture.detectChanges();
      expect(query(element, '.split').getAttribute('data-view')).toBe('preview');
      expect(tabs[1].getAttribute('aria-selected')).toBe('true');
    });
  });

  describe('saving', () => {
    async function openValid(): Promise<HTMLElement> {
      const element = await openNew();
      typeInto(element, '#vf-path', 'docs/a.md');
      edit(element, '---\ntitle: "A"\nstatus: draft\n---\n\n# A\n\ntext\n');
      return element;
    }

    it('saves automatically after a pause and reports it', async () => {
      const element = await openValid();
      await pause(1400);
      http().expectNone((request) => request.method === 'PUT');
      await pause(300);
      const request = http().expectOne((candidate) => candidate.method === 'PUT');
      expect(request.request.body).toMatchObject({ path: 'docs/a.md', baseBlobId: '' });
      request.flush(echo(request));
      await settle();
      http()
        .match('/api/v1/drafts')
        .forEach((pending) => pending.flush({ drafts: [] }));
      expect(element.querySelector('.state')?.textContent).toContain('Saved');
    });

    it('saves immediately with the button and the keyboard shortcut', async () => {
      const element = await openValid();
      const [save] = [...element.querySelectorAll<HTMLButtonElement>('.bar__actions button')];
      save.click();
      http()
        .expectOne((candidate) => candidate.method === 'PUT')
        .flush(draftOf({ isNew: true, baseBlobId: '' }));
      await settle();

      edit(element, '---\ntitle: "A"\nstatus: draft\n---\n\n# A\n\nmore\n');
      const event = new KeyboardEvent('keydown', {
        key: 's',
        ctrlKey: true,
        bubbles: true,
        cancelable: true,
      });
      document.dispatchEvent(event);
      expect(event.defaultPrevented).toBe(true);
      http()
        .expectOne((candidate) => candidate.method === 'PUT')
        .flush(draftOf({ isNew: true, baseBlobId: '' }));
    });

    it('sends the author name with the draft', async () => {
      TestBed.inject(Author).set('Alex');
      const element = await openValid();
      query<HTMLButtonElement>(element, '.bar__actions button').click();
      expect(
        http().expectOne((candidate) => candidate.method === 'PUT').request.body,
      ).toMatchObject({ author: 'Alex' });
    });

    it('reports a failed save and allows another attempt', async () => {
      const element = await openValid();
      query<HTMLButtonElement>(element, '.bar__actions button').click();
      http()
        .expectOne((candidate) => candidate.method === 'PUT')
        .flush({ error: 'invalid_request', message: 'x' }, { status: 400, statusText: 'bad' });
      await settle();
      expect(element.querySelector('.state')?.textContent).toContain('Could not save');
      expect(query<HTMLButtonElement>(element, '.bar__actions button').disabled).toBe(false);
    });

    it('shows unsaved changes', async () => {
      const element = await openValid();
      expect(element.querySelector('.state')?.textContent).toContain('Unsaved changes');
    });

    it('does not save an empty path automatically', async () => {
      const element = await openNew();
      edit(element, '# something\n');
      await pause(3000);
      http().expectNone((candidate) => candidate.method === 'PUT');
      expect(element.querySelector('.state')?.textContent).toContain('Unsaved changes');
    });

    it('renames the draft by removing the old one', async () => {
      const element = await openValid();
      query<HTMLButtonElement>(element, '.bar__actions button').click();
      http()
        .expectOne((candidate) => candidate.method === 'PUT')
        .flush(draftOf({ id: 'aaaaaaaaaaaaaaaa', path: 'docs/a.md', isNew: true, baseBlobId: '' }));
      await settle();

      typeInto(element, '#vf-path', 'docs/b.md');
      query<HTMLButtonElement>(element, '.bar__actions button').click();
      http()
        .expectOne((candidate) => candidate.method === 'PUT')
        .flush(draftOf({ id: 'bbbbbbbbbbbbbbbb', path: 'docs/b.md', isNew: true, baseBlobId: '' }));
      await settle();
      const removal = http().expectOne('/api/v1/repos/platform/drafts/aaaaaaaaaaaaaaaa');
      expect(removal.request.method).toBe('DELETE');
      removal.flush(null, { status: 204, statusText: 'No Content' });
    });
  });

  describe('editing', () => {
    it('edits an existing document, keeping its path fixed', async () => {
      const element = await openPath('docs/guide/start.md');
      answerPreviews();
      expect(element.querySelector('h1.heading')?.textContent?.trim()).toBe('Edit document');
      const path = query<HTMLInputElement>(element, '#vf-path');
      expect(path.readOnly).toBe(true);
      expect(path.value).toBe('docs/guide/start.md');
      expect(query<HTMLInputElement>(element, '.field--title input').value).toBe('Start');
      expect(element.querySelectorAll('.segment')[1].getAttribute('aria-checked')).toBe('true');
      expect(element.textContent).toContain('cannot be changed');
    });

    it('saves with the blob it was based on', async () => {
      const element = await openPath('docs/guide/start.md', {
        document: doc({ path: 'docs/guide/start.md', blobId: 'blob-1', raw: '# Start\n' }),
      });
      edit(element, '# Start\n\nchanged\n');
      query<HTMLButtonElement>(element, '.bar__actions button').click();
      const request = http().expectOne((candidate) => candidate.method === 'PUT');
      expect(request.request.body).toMatchObject({
        path: 'docs/guide/start.md',
        baseBlobId: 'blob-1',
      });
      request.flush(draftOf({ path: 'docs/guide/start.md' }));
    });

    it('continues an existing draft instead of the published version', async () => {
      const draft = draftOf({
        content: '---\ntitle: "Draft title"\nstatus: draft\n---\n# From draft\n',
      });
      const element = await openPath('docs/a.md', {
        drafts: [{ ...draft, content: undefined }],
        draft,
      });
      expect(query<HTMLTextAreaElement>(element, 'textarea.source').value).toContain('From draft');
      expect(element.querySelector('.banner--info')?.textContent).toContain('saved draft');
    });

    it('opens a draft of a document that is not published yet, with an editable path', async () => {
      const draft = draftOf({ isNew: true, baseBlobId: '', path: 'docs/new.md' });
      const element = await openPath('docs/new.md', { drafts: [{ ...draft }], draft });
      const path = query<HTMLInputElement>(element, '#vf-path');
      expect(path.readOnly).toBe(false);
      expect(path.value).toBe('docs/new.md');
    });

    it('shows a draft that already has a merge request', async () => {
      const draft = draftOf({ mergeRequestUrl: 'https://gl/mr/3' });
      const element = await openPath('docs/a.md', { drafts: [{ ...draft }], draft });
      const note = [...element.querySelectorAll('.banner--info')].find((banner) =>
        banner.textContent?.includes('merge request already exists'),
      );
      expect(note?.querySelector('a')?.getAttribute('href')).toBe('https://gl/mr/3');
    });

    it('starts a new document at a path that does not exist yet', async () => {
      const element = await openPath('docs/brand-new.md', { document: 404 });
      const path = query<HTMLInputElement>(element, '#vf-path');
      expect(path.readOnly).toBe(false);
      expect(path.value).toBe('docs/brand-new.md');
      expect(element.querySelector('h1.heading')?.textContent?.trim()).toBe('New document');
    });

    it('explains when the editor cannot be opened', async () => {
      const element = await openPath('docs/a.md', { document: 502 });
      expect(element.querySelector('[role="alert"] h1')?.textContent).toBe(
        'Cannot open the editor',
      );
      expect(element.querySelector('[role="alert"] a')?.getAttribute('href')).toBe('/r/platform');
    });
  });

  describe('publishing', () => {
    async function openReady(options: { existing?: boolean } = {}): Promise<HTMLElement> {
      const element = options.existing
        ? await openPath('docs/guide/start.md', {
            document: doc({
              path: 'docs/guide/start.md',
              blobId: 'blob-1',
              raw: '---\ntitle: "Start"\nstatus: published\n---\n# Start\n',
            }),
          })
        : await openNew();
      if (!options.existing) {
        typeInto(element, '#vf-path', 'docs/new-doc.md');
        edit(element, '---\ntitle: "New doc"\nstatus: published\n---\n\n# New doc\n\ntext\n');
      } else {
        edit(element, '---\ntitle: "Start"\nstatus: published\n---\n# Start\n\nchanged\n');
      }
      return element;
    }

    function openDialog(element: HTMLElement): void {
      (element.querySelectorAll('.bar__actions button')[1] as HTMLButtonElement).click();
      fixture.detectChanges();
      TestBed.tick();
    }

    function submit(element: HTMLElement): void {
      query<HTMLFormElement>(element, 'dialog form').dispatchEvent(
        new Event('submit', { cancelable: true }),
      );
    }

    async function answerSave(path = 'docs/new-doc.md'): Promise<void> {
      http()
        .expectOne((candidate) => candidate.method === 'PUT')
        .flush(
          draftOf({
            path,
            isNew: !path.includes('guide'),
            baseBlobId: path.includes('guide') ? 'blob-1' : '',
          }),
        );
      await settle();
    }

    it('asks for confirmation first and explains what will happen', async () => {
      const element = await openReady();
      openDialog(element);
      const dialog = query<HTMLDialogElement>(element, 'dialog');
      expect(dialog.hasAttribute('open')).toBe(true);
      expect(dialog.textContent).toContain(
        'A merge request will be created in demo/platform and merged into master',
      );
      query<HTMLButtonElement>(element, 'dialog button[type="button"]').click();
      fixture.detectChanges();
      TestBed.tick();
      expect(dialog.hasAttribute('open')).toBe(false);
    });

    it('describes manual merging for repositories without automatic merge', async () => {
      const element = await openNew({ ...REPO, autoMerge: false });
      typeInto(element, '#vf-path', 'docs/new-doc.md');
      edit(element, '---\ntitle: "New doc"\nstatus: published\n---\n\n# New doc\n\ntext\n');
      openDialog(element);
      fixture.detectChanges();
      expect(query(element, 'dialog').textContent).toContain('You will merge it in GitLab');
    });

    it('locks the title and the text while the document is being published', async () => {
      const element = await openReady();
      openDialog(element);
      submit(element);
      await answerSave();
      fixture.detectChanges();
      expect(query<HTMLTextAreaElement>(element, 'textarea').readOnly).toBe(true);
      expect(query<HTMLInputElement>(element, '.field--title input').readOnly).toBe(true);
      http()
        .expectOne((candidate) => candidate.method === 'POST')
        .flush({ outcome: 'merged', draftRemoved: true, path: 'docs/new-doc.md', created: true });
      await settle();
      expect(query<HTMLTextAreaElement>(element, 'textarea').readOnly).toBe(false);
    });

    it('remembers the author name typed in the dialog', async () => {
      const element = await openReady();
      openDialog(element);
      typeInto(element, 'dialog input', 'Sam');
      expect(TestBed.inject(Author).name()).toBe('Sam');
    });

    it('opens from the keyboard only when publishing is possible', async () => {
      const element = await openNew();
      document.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Enter', ctrlKey: true, cancelable: true }),
      );
      fixture.detectChanges();
      expect(query<HTMLDialogElement>(element, 'dialog').hasAttribute('open')).toBe(false);

      typeInto(element, '#vf-path', 'docs/a.md');
      edit(element, 'text');
      document.dispatchEvent(
        new KeyboardEvent('keydown', { key: 'Enter', metaKey: true, cancelable: true }),
      );
      fixture.detectChanges();
      TestBed.tick();
      expect(query<HTMLDialogElement>(element, 'dialog').hasAttribute('open')).toBe(true);
    });

    it('ignores other shortcuts', async () => {
      await openNew();
      expect(() => {
        document.dispatchEvent(new KeyboardEvent('keydown', { key: 'x', ctrlKey: true }));
        document.dispatchEvent(new KeyboardEvent('keydown', { key: 's' }));
      }).not.toThrow();
      http().expectNone((candidate) => candidate.method === 'PUT');
    });

    it('saves, publishes and reports success', async () => {
      const element = await openReady();
      TestBed.inject(Author).set('Alex');
      openDialog(element);
      submit(element);
      fixture.detectChanges();
      expect(element.querySelector('.result')).toBeNull();
      await answerSave();

      const request = http().expectOne('/api/v1/repos/platform/drafts/0123456789abcdef/publish');
      expect(request.request.body).toEqual({ author: 'Alex', force: false });
      request.flush(MERGED);
      await settle();
      http()
        .match('/api/v1/drafts')
        .forEach((pending) => pending.flush({ drafts: [] }));
      http()
        .match('/api/v1/repos')
        .forEach((pending) => pending.flush({ repos: [REPO] }));
      http()
        .match('/api/v1/repos/platform/tree')
        .forEach((pending) =>
          pending.flush({ repo: 'platform', branch: 'master', stale: false, roots: [] }),
        );
      await settle();

      const result = query(element, '.result--merged');
      expect(result.textContent).toContain('Published');
      expect(result.textContent).toContain('now in master');
      expect(result.querySelector('a[href="/r/platform/docs/new-doc.md"]')).not.toBeNull();
      expect(result.querySelector('a[target="_blank"]')?.getAttribute('href')).toBe(
        'https://gl/mr/1',
      );
      expect(query<HTMLDialogElement>(element, 'dialog').hasAttribute('open')).toBe(false);
    });

    it('explains why a merge request was not merged', async () => {
      const element = await openReady();
      openDialog(element);
      submit(element);
      await answerSave();
      http()
        .expectOne('/api/v1/repos/platform/drafts/0123456789abcdef/publish')
        .flush({
          ...MERGED,
          outcome: 'pending',
          reason: 'not_mergeable',
          detail: 'ci_must_pass',
          draftRemoved: false,
        });
      await settle();
      http().match(() => true);
      const result = query(element, '.result--pending');
      expect(result.textContent).toContain('Merge request created');
      expect(result.textContent).toContain('does not allow merging yet (ci_must_pass)');
    });

    it('offers to overwrite after a conflict and publishes with force', async () => {
      const element = await openReady({ existing: true });
      openDialog(element);
      submit(element);
      await answerSave('docs/guide/start.md');
      http()
        .expectOne('/api/v1/repos/platform/drafts/0123456789abcdef/publish')
        .flush(
          { error: 'conflict', message: 'changed', exists: true, currentBlobId: 'cur' },
          { status: 409, statusText: 'Conflict' },
        );
      await settle();

      const conflict = query(element, '.result--conflict');
      expect(conflict.textContent).toContain('changed in the repository');
      expect(conflict.textContent).toContain('Someone changed or created this file');

      const overwrite = conflict.querySelector('button') as HTMLButtonElement;
      overwrite.click();
      await answerSave('docs/guide/start.md');
      const forced = http().expectOne('/api/v1/repos/platform/drafts/0123456789abcdef/publish');
      expect(forced.request.body).toMatchObject({ force: true });
      forced.flush({ ...MERGED, created: false, path: 'docs/guide/start.md' });
    });

    it('explains a conflict about a removed file', async () => {
      const element = await openReady({ existing: true });
      openDialog(element);
      submit(element);
      await answerSave('docs/guide/start.md');
      http()
        .expectOne('/api/v1/repos/platform/drafts/0123456789abcdef/publish')
        .flush(
          { error: 'conflict', message: 'gone', exists: false },
          { status: 409, statusText: 'Conflict' },
        );
      await settle();
      expect(query(element, '.result--conflict').textContent).toContain(
        'was removed from the repository',
      );
    });

    it('lets the user keep editing after a conflict', async () => {
      const element = await openReady({ existing: true });
      openDialog(element);
      submit(element);
      await answerSave('docs/guide/start.md');
      http()
        .expectOne('/api/v1/repos/platform/drafts/0123456789abcdef/publish')
        .flush(
          { error: 'conflict', message: 'changed', exists: true },
          { status: 409, statusText: 'Conflict' },
        );
      await settle();
      (element.querySelectorAll('.result--conflict button')[2] as HTMLButtonElement).click();
      fixture.detectChanges();
      expect(element.querySelector('.result--conflict')).toBeNull();
    });

    it('discards the draft after a conflict and returns to the document', async () => {
      const navigate = vi.spyOn(TestBed.inject(Router), 'navigate').mockResolvedValue(true);
      const element = await openReady({ existing: true });
      openDialog(element);
      submit(element);
      await answerSave('docs/guide/start.md');
      http()
        .expectOne('/api/v1/repos/platform/drafts/0123456789abcdef/publish')
        .flush(
          { error: 'conflict', message: 'changed', exists: true },
          { status: 409, statusText: 'Conflict' },
        );
      await settle();
      (element.querySelectorAll('.result--conflict button')[1] as HTMLButtonElement).click();
      const removal = http().expectOne('/api/v1/repos/platform/drafts/0123456789abcdef');
      removal.flush(null, { status: 204, statusText: 'No Content' });
      await settle();
      http()
        .match('/api/v1/drafts')
        .forEach((pending) => pending.flush({ drafts: [] }));
      expect(navigate).toHaveBeenCalledWith(['/r', 'platform', 'docs', 'guide', 'start.md']);
    });

    it('reports a busy document and other failures', async () => {
      const element = await openReady();
      openDialog(element);
      submit(element);
      await answerSave();
      http()
        .expectOne('/api/v1/repos/platform/drafts/0123456789abcdef/publish')
        .flush({ error: 'busy', message: 'later' }, { status: 409, statusText: 'Conflict' });
      await settle();
      expect(query(element, '.banner--danger').textContent).toContain('being published right now');

      openDialog(element);
      submit(element);
      await answerSave();
      http()
        .expectOne('/api/v1/repos/platform/drafts/0123456789abcdef/publish')
        .flush(
          { error: 'invalid_request', message: 'nothing to publish' },
          { status: 400, statusText: 'Bad' },
        );
      await settle();
      expect(query(element, '.banner--danger').textContent).toContain('nothing to publish');

      openDialog(element);
      submit(element);
      await answerSave();
      http()
        .expectOne('/api/v1/repos/platform/drafts/0123456789abcdef/publish')
        .flush({}, { status: 502, statusText: 'Bad' });
      await settle();
      expect(query(element, '.banner--danger').textContent).toContain('Could not save');
    });

    it('does not publish when saving fails', async () => {
      const element = await openReady();
      openDialog(element);
      submit(element);
      http()
        .expectOne((candidate) => candidate.method === 'PUT')
        .flush(
          { error: 'invalid_request', message: 'bad path' },
          { status: 400, statusText: 'Bad' },
        );
      await settle();
      http().expectNone((candidate) => candidate.url.endsWith('/publish'));
      expect(query(element, '.banner--danger').textContent).toContain('bad path');
    });
  });

  describe('leaving', () => {
    it('asks before leaving with unsaved changes', async () => {
      const element = await openNew();
      typeInto(element, '#vf-path', 'docs/a.md');
      edit(element, 'unsaved');
      const confirm = vi.spyOn(window, 'confirm').mockReturnValue(false);
      expect(fixture.componentInstance.canLeave()).toBe(false);
      expect(confirm).toHaveBeenCalledWith('You have unsaved changes. Leave anyway?');
      confirm.mockReturnValue(true);
      expect(fixture.componentInstance.canLeave()).toBe(true);
    });

    it('lets the user leave a clean editor without asking', async () => {
      await openNew();
      const confirm = vi.spyOn(window, 'confirm');
      expect(fixture.componentInstance.canLeave()).toBe(true);
      expect(confirm).not.toHaveBeenCalled();
    });

    it('blocks closing the tab only while there are unsaved changes', async () => {
      const element = await openNew();
      const clean = new Event('beforeunload', { cancelable: true });
      window.dispatchEvent(clean);
      expect(clean.defaultPrevented).toBe(false);

      typeInto(element, '#vf-path', 'docs/a.md');
      edit(element, 'unsaved');
      const dirty = new Event('beforeunload', { cancelable: true });
      window.dispatchEvent(dirty);
      expect(dirty.defaultPrevented).toBe(true);
    });
  });

  it('renders in Russian', async () => {
    TestBed.inject(I18n).setLang('ru');
    const element = await openNew();
    expect(element.querySelector('h1.heading')?.textContent?.trim()).toBe('Новый документ');
    expect(element.querySelector('.bar__actions')?.textContent).toContain('Сохранить черновик');
  });

  it('sets the page title from the document title', async () => {
    const element = await openNew();
    typeInto(element, '.field--title input', 'Rollout');
    TestBed.tick();
    expect(document.title).toBe('Rollout · Vibefluence');
  });
});
