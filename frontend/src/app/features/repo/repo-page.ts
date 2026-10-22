import { Component, computed, inject, input, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { TuiButton, TuiIcon } from '@taiga-ui/core';
import { Api, ApiError } from '../../core/api';
import { I18n } from '../../core/i18n';
import { docLink } from '../../core/links';
import { usePageTitle } from '../../core/page-title';
import { Repos } from '../../core/repos';
import { Toasts } from '../../core/toast';
import { Trees } from '../../core/trees';
import { flattenDocs, groupByFolder } from './flatten';

@Component({
  selector: 'vf-repo-page',
  imports: [RouterLink, TuiIcon, TuiButton],
  templateUrl: './repo-page.html',
  styleUrl: './repo-page.scss',
})
export class RepoPage {
  protected readonly i18n = inject(I18n);
  protected readonly repos = inject(Repos);
  protected readonly trees = inject(Trees);
  private readonly api = inject(Api);
  private readonly toasts = inject(Toasts);

  readonly repo = input.required<string>();

  protected readonly refreshing = signal(false);
  protected readonly info = computed(() => this.repos.byId(this.repo()));
  protected readonly tree = computed(() => {
    const tree = this.trees.tree();
    return tree?.repo === this.repo() ? tree : undefined;
  });
  protected readonly groups = computed(() => groupByFolder(flattenDocs(this.tree()?.roots ?? [])));
  protected readonly docCount = computed(() =>
    this.groups().reduce((sum, group) => sum + group.docs.length, 0),
  );
  protected readonly failed = computed(
    () => !this.tree() && !this.trees.loading() && !!this.trees.error(),
  );
  protected readonly placeholders = [1, 2, 3, 4];

  constructor() {
    usePageTitle(() => [this.info()?.name ?? this.repo()]);
  }

  protected link(path: string): string[] {
    return docLink(this.repo(), path);
  }

  protected refresh(): void {
    if (this.refreshing()) {
      return;
    }
    this.refreshing.set(true);
    this.api.refresh(this.repo()).subscribe({
      next: () => {
        this.refreshing.set(false);
        this.repos.reload();
        this.trees.reload();
        this.toasts.success(this.i18n.t('repo.refreshed'));
      },
      error: (error: ApiError) => {
        this.refreshing.set(false);
        this.toasts.error(this.i18n.t('repo.refreshFailed'));
        console.warn('refresh failed', error.code);
      },
    });
  }

  protected retry(): void {
    this.trees.reload();
    this.repos.reload();
  }
}
