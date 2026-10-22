import { Component, ElementRef, effect, inject, signal, viewChild } from '@angular/core';
import { takeUntilDestroyed, toObservable } from '@angular/core/rxjs-interop';
import { Router } from '@angular/router';
import { TuiIcon } from '@taiga-ui/core';
import {
  combineLatest,
  debounceTime,
  distinctUntilChanged,
  map,
  of,
  switchMap,
  catchError,
  tap,
} from 'rxjs';
import { Api } from '../../core/api';
import { I18n } from '../../core/i18n';
import { docLink } from '../../core/links';
import { SearchResult } from '../../core/models';
import { Repos } from '../../core/repos';
import { RouteState } from '../../core/route-state';
import { Ui } from '../../core/ui';
import { highlight, Segment } from './highlight';

type Scope = 'all' | 'repo';
type SearchState = 'idle' | 'loading' | 'ready' | 'error';

const DEBOUNCE_MS = 220;
const MIN_QUERY = 2;
const LIMIT = 20;

@Component({
  selector: 'vf-search-palette',
  imports: [TuiIcon],
  templateUrl: './search-palette.html',
  styleUrl: './search-palette.scss',
  host: {
    '(click)': 'onHostClick($event)',
  },
})
export class SearchPalette {
  protected readonly i18n = inject(I18n);
  protected readonly ui = inject(Ui);
  protected readonly repos = inject(Repos);
  private readonly api = inject(Api);
  private readonly router = inject(Router);
  private readonly route = inject(RouteState);

  private readonly dialog = viewChild.required<ElementRef<HTMLDialogElement>>('dialog');
  private readonly input = viewChild.required<ElementRef<HTMLInputElement>>('input');

  protected readonly query = signal('');
  protected readonly scope = signal<Scope>('all');
  protected readonly state = signal<SearchState>('idle');
  protected readonly results = signal<SearchResult[]>([]);
  protected readonly active = signal(0);
  protected readonly repoId = this.route.repo;

  constructor() {
    effect(() => {
      const dialog = this.dialog().nativeElement;
      if (this.ui.searchOpen() && !dialog.open) {
        this.query.set('');
        this.results.set([]);
        this.state.set('idle');
        this.active.set(0);
        this.scope.set(this.repoId() ? 'repo' : 'all');
        dialog.showModal();
        queueMicrotask(() => this.input().nativeElement.focus());
      } else if (!this.ui.searchOpen() && dialog.open) {
        dialog.close();
      }
    });

    combineLatest([toObservable(this.query), toObservable(this.scope), toObservable(this.repoId)])
      .pipe(
        map(([query, scope, repo]) => ({
          query: query.trim(),
          repo: scope === 'repo' ? repo : null,
        })),
        distinctUntilChanged((a, b) => a.query === b.query && a.repo === b.repo),
        tap(({ query }) => {
          if (query.length < MIN_QUERY) {
            this.results.set([]);
            this.state.set('idle');
          }
        }),
        debounceTime(DEBOUNCE_MS),
        switchMap(({ query, repo }) => {
          if (query.length < MIN_QUERY) {
            return of(null);
          }
          this.state.set('loading');
          return this.api.search(query, repo ?? undefined, LIMIT).pipe(
            map((response) => response.results),
            catchError(() => of('error' as const)),
          );
        }),
        takeUntilDestroyed(),
      )
      .subscribe((outcome) => {
        if (outcome === null) {
          return;
        }
        if (outcome === 'error') {
          this.results.set([]);
          this.state.set('error');
          return;
        }
        this.results.set(outcome);
        this.active.set(0);
        this.state.set('ready');
      });
  }

  protected repoName(id: string): string {
    return this.repos.byId(id)?.name ?? id;
  }

  protected showRepoName(): boolean {
    return this.repos.list().length > 1;
  }

  protected segments(text: string): Segment[] {
    return highlight(text, this.query());
  }

  protected link(result: SearchResult): string[] {
    return docLink(result.repo, result.path);
  }

  protected onInput(event: Event): void {
    this.query.set((event.target as HTMLInputElement).value);
  }

  protected onKeydown(event: KeyboardEvent): void {
    const count = this.results().length;
    if (event.key === 'ArrowDown' && count > 0) {
      event.preventDefault();
      this.active.set((this.active() + 1) % count);
    } else if (event.key === 'ArrowUp' && count > 0) {
      event.preventDefault();
      this.active.set((this.active() - 1 + count) % count);
    } else if (event.key === 'Enter') {
      const result = this.results()[this.active()];
      if (result) {
        event.preventDefault();
        this.open(result);
      }
    }
  }

  protected open(result: SearchResult): void {
    this.ui.closeSearch();
    void this.router.navigate(this.link(result));
  }

  protected setScope(scope: Scope): void {
    this.scope.set(scope);
    this.input().nativeElement.focus();
  }

  protected onClose(): void {
    this.ui.closeSearch();
  }

  protected onHostClick(event: MouseEvent): void {
    const target = event.target as HTMLElement;
    if (target === this.dialog().nativeElement) {
      this.ui.closeSearch();
      return;
    }
    const option = target.closest<HTMLElement>('[role="option"]');
    const result = option ? this.results()[Number(option.dataset['index'])] : undefined;
    if (result) {
      this.open(result);
    }
  }
}
