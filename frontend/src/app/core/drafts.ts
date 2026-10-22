import { Injectable, computed, inject } from '@angular/core';
import { rxResource } from '@angular/core/rxjs-interop';
import { Api } from './api';
import { DraftSummary } from './models';

@Injectable({ providedIn: 'root' })
export class Drafts {
  private readonly api = inject(Api);

  readonly resource = rxResource<DraftSummary[], void>({
    defaultValue: [],
    stream: () => this.api.allDrafts(),
  });

  readonly list = computed(() => (this.resource.hasValue() ? this.resource.value() : []));
  readonly count = computed(() => this.list().length);
  readonly loading = computed(() => this.resource.status() === 'loading');
  readonly error = computed(() => this.resource.error());

  forPath(repo: string, path: string): DraftSummary | undefined {
    return this.list().find((draft) => draft.repo === repo && draft.path === path);
  }

  reload(): void {
    this.resource.reload();
  }
}
