import { Injectable, computed, inject } from '@angular/core';
import { rxResource } from '@angular/core/rxjs-interop';
import { Api } from './api';
import { Repo } from './models';

@Injectable({ providedIn: 'root' })
export class Repos {
  private readonly api = inject(Api);

  readonly resource = rxResource<Repo[], void>({
    defaultValue: [],
    stream: () => this.api.repos(),
  });

  readonly list = computed(() => (this.resource.hasValue() ? this.resource.value() : []));
  readonly loading = computed(() => this.resource.status() === 'loading');
  readonly error = computed(() => this.resource.error());

  byId(id: string | null | undefined): Repo | undefined {
    return id ? this.list().find((repo) => repo.id === id) : undefined;
  }

  reload(): void {
    this.resource.reload();
  }
}
