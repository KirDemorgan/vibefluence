import { provideHttpClient } from '@angular/common/http';
import {
  HttpTestingController,
  TestRequest,
  provideHttpClientTesting,
} from '@angular/common/http/testing';
import { EnvironmentProviders, Provider } from '@angular/core';
import { TestBed } from '@angular/core/testing';
import { provideRouter, Routes } from '@angular/router';
import { provideTaiga } from '@taiga-ui/core';
import axe from 'axe-core';
import { DocumentResponse, Repo, TreeResponse } from './app/core/models';

export const REPO: Repo = {
  id: 'platform',
  name: 'Platform',
  project: 'demo/platform',
  branch: 'master',
  paths: ['docs'],
  autoMerge: true,
  docCount: 3,
  hiddenCount: 1,
  stale: false,
  available: true,
  webUrl: 'https://gitlab.example.com/demo/platform',
};

export const SECOND_REPO: Repo = {
  ...REPO,
  id: 'web',
  name: 'Web',
  project: 'demo/web',
  docCount: 1,
  hiddenCount: 0,
};

export const TREE: TreeResponse = {
  repo: 'platform',
  branch: 'master',
  stale: false,
  roots: [
    {
      name: 'docs',
      path: 'docs',
      kind: 'folder',
      children: [
        {
          name: 'guide',
          path: 'docs/guide',
          kind: 'folder',
          children: [
            {
              name: 'start.md',
              path: 'docs/guide/start.md',
              kind: 'doc',
              title: 'Getting started',
              type: 'guide',
            },
          ],
        },
        { name: 'README.md', path: 'docs/README.md', kind: 'doc', title: 'Welcome' },
      ],
    },
  ],
};

export function doc(overrides: Partial<DocumentResponse> = {}): DocumentResponse {
  return {
    repo: 'platform',
    path: 'docs/guide/start.md',
    title: 'Getting started',
    status: 'published',
    type: 'guide',
    warnings: [],
    html: '<h1 id="h-getting-started">Getting started</h1><p>Hello <a href="/r/platform/docs/README.md">readme</a></p><h2 id="h-install">Install</h2><p>text</p><h2 id="h-usage">Usage</h2><h3 id="h-flags">Flags</h3>',
    headings: [
      { level: 1, text: 'Getting started', id: 'h-getting-started' },
      { level: 2, text: 'Install', id: 'h-install' },
      { level: 2, text: 'Usage', id: 'h-usage' },
      { level: 3, text: 'Flags', id: 'h-flags' },
    ],
    raw: '# Getting started',
    blobId: 'abc',
    webUrl: 'https://gitlab.example.com/demo/platform/-/blob/master/docs/guide/start.md',
    stale: false,
    ...overrides,
  };
}

export function provideApp(
  routes: Routes = [],
  ...extra: (Provider | EnvironmentProviders)[]
): (Provider | EnvironmentProviders)[] {
  return [
    provideRouter(routes),
    provideHttpClient(),
    provideHttpClientTesting(),
    provideTaiga(),
    ...extra,
  ];
}

export function http(): HttpTestingController {
  return TestBed.inject(HttpTestingController);
}

export function flush(
  url: string | RegExp,
  body: object | string | null,
  options?: { status: number; statusText: string },
): TestRequest {
  const request =
    typeof url === 'string'
      ? http().expectOne((candidate) => candidate.urlWithParams === url)
      : http().expectOne((candidate) => url.test(candidate.urlWithParams));
  request.flush(body, options);
  return request;
}

export function fail(
  url: string | RegExp,
  status: number,
  body: object | string | null = {},
): void {
  flush(url, body, { status, statusText: 'error' });
}

export async function accessibilityViolations(element: HTMLElement): Promise<string[]> {
  const results = await axe.run(element, {
    rules: { 'color-contrast': { enabled: false }, region: { enabled: false } },
  });
  return results.violations.map(
    (violation) =>
      `${violation.id}: ${violation.nodes.map((node) => node.target.join(' ')).join(', ')}`,
  );
}
