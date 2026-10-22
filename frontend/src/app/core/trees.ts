import { Injectable, computed, inject } from '@angular/core';
import { rxResource } from '@angular/core/rxjs-interop';
import { Api } from './api';
import { RouteState } from './route-state';
import { TreeResponse } from './models';

@Injectable({ providedIn: 'root' })
export class Trees {
  private readonly api = inject(Api);
  private readonly route = inject(RouteState);

  readonly resource = rxResource<TreeResponse | undefined, string | undefined>({
    params: () => this.route.repo() ?? undefined,
    stream: ({ params }) => this.api.tree(params as string),
  });

  readonly tree = computed(() => (this.resource.hasValue() ? this.resource.value() : undefined));
  readonly loading = computed(() => this.resource.isLoading() && !this.tree());
  readonly error = computed(() => this.resource.error());

  reload(): void {
    this.resource.reload();
  }
}
