import { Component } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { Router } from '@angular/router';
import { provideApp } from '../../testing';
import { RouteState } from './route-state';

@Component({ template: '' })
class Blank {}

describe('RouteState', () => {
  let router: Router;
  let state: RouteState;

  beforeEach(() => {
    TestBed.configureTestingModule({ providers: provideApp([{ path: '**', component: Blank }]) });
    router = TestBed.inject(Router);
    state = TestBed.inject(RouteState);
  });

  it('has no repository on the home page', async () => {
    await router.navigateByUrl('/');
    expect(state.repo()).toBeNull();
    expect(state.docPath()).toBeNull();
    expect(state.fragment()).toBeNull();
  });

  it('exposes the repository on a repository page', async () => {
    await router.navigateByUrl('/r/platform');
    expect(state.repo()).toBe('platform');
    expect(state.docPath()).toBeNull();
  });

  it('exposes repository, document and fragment', async () => {
    await router.navigateByUrl('/r/platform/docs/guide/start.md#h-install');
    expect(state.repo()).toBe('platform');
    expect(state.docPath()).toBe('docs/guide/start.md');
    expect(state.fragment()).toBe('h-install');
  });

  it('decodes encoded segments and fragments', async () => {
    await router.navigateByUrl(
      '/r/platform/docs/my%20file.md#h-%D0%BF%D1%80%D0%B8%D0%B2%D0%B5%D1%82',
    );
    expect(state.docPath()).toBe('docs/my file.md');
    expect(state.fragment()).toBe('h-привет');
  });

  it('treats an empty fragment as none', async () => {
    await router.navigateByUrl('/r/platform/docs/a.md#');
    expect(state.fragment()).toBeNull();
  });

  it('updates when the route changes', async () => {
    await router.navigateByUrl('/r/one');
    expect(state.repo()).toBe('one');
    await router.navigateByUrl('/r/two/docs/x.md');
    expect(state.repo()).toBe('two');
    expect(state.docPath()).toBe('docs/x.md');
  });
});
