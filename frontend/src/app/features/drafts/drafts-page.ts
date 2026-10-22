import { Component, computed, inject, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { TuiButton, TuiIcon } from '@taiga-ui/core';
import { Api } from '../../core/api';
import { Drafts } from '../../core/drafts';
import { I18n } from '../../core/i18n';
import { editLink, newLink } from '../../core/links';
import { DraftSummary } from '../../core/models';
import { usePageTitle } from '../../core/page-title';
import { Repos } from '../../core/repos';
import { relativeTime } from '../../core/time';
import { Toasts } from '../../core/toast';

interface Group {
  repo: string;
  name: string;
  items: DraftSummary[];
}

@Component({
  selector: 'vf-drafts-page',
  imports: [RouterLink, TuiIcon, TuiButton],
  templateUrl: './drafts-page.html',
  styleUrl: './drafts-page.scss',
})
export class DraftsPage {
  protected readonly i18n = inject(I18n);
  protected readonly drafts = inject(Drafts);
  private readonly repos = inject(Repos);
  private readonly api = inject(Api);
  private readonly toasts = inject(Toasts);

  protected readonly confirming = signal<string | null>(null);
  protected readonly placeholders = [1, 2, 3];

  protected readonly groups = computed<Group[]>(() => {
    const byRepo = new Map<string, DraftSummary[]>();
    for (const draft of this.drafts.list()) {
      byRepo.set(draft.repo, [...(byRepo.get(draft.repo) ?? []), draft]);
    }
    return [...byRepo.entries()].map(([repo, items]) => ({
      repo,
      name: this.repos.byId(repo)?.name ?? repo,
      items,
    }));
  });

  protected readonly firstRepo = computed(() => this.repos.list()[0]?.id ?? null);

  constructor() {
    usePageTitle(() => [this.i18n.t('drafts.title')]);
  }

  protected edit(draft: DraftSummary): string[] {
    return editLink(draft.repo, draft.path);
  }

  protected newLink(repo: string): string[] {
    return newLink(repo);
  }

  protected when(draft: DraftSummary): string {
    return relativeTime(draft.updatedAt, this.i18n.lang());
  }

  protected askDelete(draft: DraftSummary): void {
    this.confirming.set(this.confirming() === draft.id ? null : draft.id);
  }

  protected remove(draft: DraftSummary): void {
    this.api.deleteDraft(draft.repo, draft.id).subscribe({
      next: () => {
        this.confirming.set(null);
        this.drafts.reload();
        this.toasts.success(this.i18n.t('drafts.deleted'));
      },
      error: () => {
        this.confirming.set(null);
        this.toasts.error(this.i18n.t('drafts.deleteFailed'));
      },
    });
  }
}
