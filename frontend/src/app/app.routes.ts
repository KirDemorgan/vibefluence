import { Routes, UrlMatchResult, UrlSegment } from '@angular/router';
import { Shell } from './layout/shell';

export function docMatcher(segments: UrlSegment[]): UrlMatchResult | null {
  if (segments.length < 3 || segments[0].path !== 'r') {
    return null;
  }
  return {
    consumed: segments,
    posParams: {
      repo: new UrlSegment(segments[1].path, {}),
      path: new UrlSegment(
        segments
          .slice(2)
          .map((segment) => segment.path)
          .join('/'),
        {},
      ),
    },
  };
}

export const routes: Routes = [
  {
    path: '',
    component: Shell,
    children: [
      {
        path: '',
        pathMatch: 'full',
        loadComponent: () => import('./features/home/home-page').then((module) => module.HomePage),
      },
      {
        matcher: docMatcher,
        loadComponent: () => import('./features/doc/doc-page').then((module) => module.DocPage),
      },
      {
        path: 'r/:repo',
        loadComponent: () => import('./features/repo/repo-page').then((module) => module.RepoPage),
      },
      {
        path: '**',
        loadComponent: () =>
          import('./features/not-found/not-found-page').then((module) => module.NotFoundPage),
      },
    ],
  },
];
