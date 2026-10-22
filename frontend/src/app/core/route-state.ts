import { Injectable, computed, inject } from '@angular/core';
import { toSignal } from '@angular/core/rxjs-interop';
import { NavigationEnd, Router } from '@angular/router';
import { filter, map, startWith } from 'rxjs';
import { docPathFromUrl, repoFromUrl } from './links';

@Injectable({ providedIn: 'root' })
export class RouteState {
  private readonly router = inject(Router);

  readonly url = toSignal(
    this.router.events.pipe(
      filter((event): event is NavigationEnd => event instanceof NavigationEnd),
      map((event) => event.urlAfterRedirects),
      startWith(this.router.url),
    ),
    { initialValue: this.router.url },
  );

  readonly repo = computed(() => repoFromUrl(this.url()));
  readonly docPath = computed(() => docPathFromUrl(this.url()));
  readonly fragment = computed(() => {
    const hash = this.url().indexOf('#');
    return hash < 0 ? null : decodeURIComponent(this.url().slice(hash + 1)) || null;
  });
}
