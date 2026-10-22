import { TestBed } from '@angular/core/testing';
import { TestRequest } from '@angular/common/http/testing';
import { fail, flush, http, provideApp, REPO } from '../../testing';
import { Api, ApiError } from './api';

describe('Api', () => {
  let api: Api;

  beforeEach(() => {
    TestBed.configureTestingModule({ providers: provideApp() });
    api = TestBed.inject(Api);
  });

  afterEach(() => http().verify());

  it('unwraps the repository list', () => {
    let result: unknown;
    api.repos().subscribe((repos) => (result = repos));
    flush('/api/v1/repos', { repos: [REPO] });
    expect(result).toEqual([REPO]);
  });

  it('encodes repository ids and document paths', () => {
    api.tree('my repo').subscribe();
    const tree = http().expectOne('/api/v1/repos/my%20repo/tree');
    expect(tree.request.method).toBe('GET');
    tree.flush({});

    api.document('platform', 'docs/a b.md').subscribe();
    const document = http().expectOne((request) => request.url === '/api/v1/repos/platform/doc');
    expect(document.request.params.get('path')).toBe('docs/a b.md');
    document.flush({});
  });

  it('passes search parameters only when they are set', () => {
    api.search('gateway').subscribe();
    let request: TestRequest = http().expectOne((candidate) => candidate.url === '/api/v1/search');
    expect(request.request.params.keys().sort()).toEqual(['q']);
    request.flush({ query: 'gateway', results: [] });

    api.search('gateway', 'platform', 5).subscribe();
    request = http().expectOne((candidate) => candidate.url === '/api/v1/search');
    expect(request.request.params.get('repo')).toBe('platform');
    expect(request.request.params.get('limit')).toBe('5');
    request.flush({ query: 'gateway', results: [] });
  });

  it('refreshes a repository with a POST', () => {
    let result: unknown;
    api.refresh('platform').subscribe((repo) => (result = repo));
    const request = http().expectOne('/api/v1/repos/platform/refresh');
    expect(request.request.method).toBe('POST');
    request.flush(REPO);
    expect(result).toEqual(REPO);
  });

  it('turns JSON error bodies into ApiError', () => {
    let error: unknown;
    api.tree('x').subscribe({ error: (failure: unknown) => (error = failure) });
    fail('/api/v1/repos/x/tree', 404, { error: 'not_found', message: 'nope' });

    expect(error).toBeInstanceOf(ApiError);
    const apiError = error as ApiError;
    expect([apiError.status, apiError.code, apiError.message]).toEqual([404, 'not_found', 'nope']);
    expect(apiError.isNotFound).toBe(true);
    expect(apiError.isUpstream).toBe(false);
  });

  it('flags upstream failures', () => {
    for (const status of [502, 504]) {
      let error: ApiError | undefined;
      api.repos().subscribe({ error: (failure: ApiError) => (error = failure) });
      fail('/api/v1/repos', status, { error: 'upstream_unavailable', message: 'down' });
      expect(error?.isUpstream, String(status)).toBe(true);
    }
  });

  it('handles non-JSON error bodies', () => {
    let error: ApiError | undefined;
    api.repos().subscribe({ error: (failure: ApiError) => (error = failure) });
    fail('/api/v1/repos', 500, '<html>boom</html>');
    expect(error?.status).toBe(500);
    expect(error?.code).toBe('unknown');
  });

  it('reports network failures with status 0', () => {
    let error: ApiError | undefined;
    api.repos().subscribe({ error: (failure: ApiError) => (error = failure) });
    http().expectOne('/api/v1/repos').error(new ProgressEvent('error'));
    expect(error?.status).toBe(0);
    expect(error?.code).toBe('network');
  });

  it('maps refresh errors too', () => {
    let error: ApiError | undefined;
    api.refresh('x').subscribe({ error: (failure: ApiError) => (error = failure) });
    fail('/api/v1/repos/x/refresh', 404, { error: 'not_found', message: 'no' });
    expect(error?.isNotFound).toBe(true);
  });

  it('lists drafts across repositories and per repository', () => {
    let all: unknown;
    let one: unknown;
    api.allDrafts().subscribe((drafts) => (all = drafts));
    flush('/api/v1/drafts', { drafts: [{ id: 'a' }] });
    api.drafts('my repo').subscribe((drafts) => (one = drafts));
    flush('/api/v1/repos/my%20repo/drafts', { drafts: [{ id: 'b' }] });
    expect(all).toEqual([{ id: 'a' }]);
    expect(one).toEqual([{ id: 'b' }]);
  });

  it('loads and deletes a draft', () => {
    let draft: unknown;
    api.draft('platform', 'abc').subscribe((value) => (draft = value));
    flush('/api/v1/repos/platform/drafts/abc', { id: 'abc', content: 'x' });
    expect(draft).toEqual({ id: 'abc', content: 'x' });

    api.deleteDraft('platform', 'abc').subscribe();
    const request = http().expectOne('/api/v1/repos/platform/drafts/abc');
    expect(request.request.method).toBe('DELETE');
    request.flush(null, { status: 204, statusText: 'No Content' });
  });

  it('saves, publishes and previews with JSON bodies', () => {
    api
      .saveDraft('platform', { path: 'docs/a.md', content: 'x', baseBlobId: '', author: 'A' })
      .subscribe();
    let request = http().expectOne('/api/v1/repos/platform/drafts');
    expect(request.request.method).toBe('PUT');
    expect(request.request.body).toEqual({
      path: 'docs/a.md',
      content: 'x',
      baseBlobId: '',
      author: 'A',
    });
    request.flush({});

    api.publish('platform', 'abc', { author: 'A', force: true }).subscribe();
    request = http().expectOne('/api/v1/repos/platform/drafts/abc/publish');
    expect(request.request.method).toBe('POST');
    expect(request.request.body).toEqual({ author: 'A', force: true });
    request.flush({});

    api.preview('platform', { path: 'docs/a.md', content: '# x' }).subscribe();
    request = http().expectOne('/api/v1/repos/platform/preview');
    expect(request.request.body).toEqual({ path: 'docs/a.md', content: '# x' });
    request.flush({});
  });

  it('exposes conflict details', () => {
    let error: ApiError | undefined;
    api
      .publish('platform', 'abc', { author: '', force: false })
      .subscribe({ error: (failure: ApiError) => (error = failure) });
    fail('/api/v1/repos/platform/drafts/abc/publish', 409, {
      error: 'conflict',
      message: 'changed',
      exists: true,
      currentBlobId: 'cur',
    });
    expect(error?.isConflict).toBe(true);
    expect(error?.isBusy).toBe(false);
    expect(error?.details).toMatchObject({ exists: true, currentBlobId: 'cur' });
  });

  it('recognises a busy document', () => {
    let error: ApiError | undefined;
    api
      .publish('platform', 'abc', { author: '', force: false })
      .subscribe({ error: (failure: ApiError) => (error = failure) });
    fail('/api/v1/repos/platform/drafts/abc/publish', 409, { error: 'busy', message: 'later' });
    expect(error?.isBusy).toBe(true);
    expect(error?.isConflict).toBe(false);
  });

  it('maps errors of every write call', () => {
    const errors: ApiError[] = [];
    const collect = { error: (failure: ApiError) => errors.push(failure) };
    api.saveDraft('p', { path: 'x', content: '', baseBlobId: '', author: '' }).subscribe(collect);
    fail('/api/v1/repos/p/drafts', 400, { error: 'invalid_request', message: 'bad' });
    api.deleteDraft('p', 'x').subscribe(collect);
    fail('/api/v1/repos/p/drafts/x', 404, { error: 'not_found', message: 'gone' });
    api.preview('p', { path: 'x', content: '' }).subscribe(collect);
    fail('/api/v1/repos/p/preview', 413, { error: 'too_large', message: 'big' });
    api.draft('p', 'x').subscribe(collect);
    fail('/api/v1/repos/p/drafts/x', 404, { error: 'not_found', message: 'gone' });
    expect(errors.map((error) => error.code)).toEqual([
      'invalid_request',
      'not_found',
      'too_large',
      'not_found',
    ]);
  });
});
