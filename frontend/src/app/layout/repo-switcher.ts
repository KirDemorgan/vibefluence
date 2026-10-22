import { Component, computed, inject } from '@angular/core';
import { RouterLink } from '@angular/router';
import { TuiIcon } from '@taiga-ui/core';
import { I18n } from '../core/i18n';
import { repoLink } from '../core/links';
import { Repos } from '../core/repos';
import { RouteState } from '../core/route-state';
import { Popover } from '../shared/popover';

@Component({
  selector: 'vf-repo-switcher',
  imports: [RouterLink, TuiIcon, Popover],
  template: `
    <button
      #trigger
      type="button"
      class="trigger"
      aria-haspopup="true"
      [attr.aria-expanded]="menu.open()"
      (click)="menu.toggle(trigger)"
    >
      <tui-icon class="trigger__icon" icon="@tui.book-marked" />
      <span class="trigger__text">
        @if (current(); as repo) {
          <span class="trigger__name">{{ repo.name }}</span>
          <span class="trigger__meta">{{ repo.project }}</span>
        } @else {
          <span class="trigger__name">{{ i18n.t('nav.chooseRepo') }}</span>
        }
      </span>
      <tui-icon class="trigger__chevron" icon="@tui.chevrons-up-down" />
    </button>

    <nav
      vfPopover
      #menu="vfPopover"
      class="menu"
      [matchWidth]="true"
      [attr.aria-label]="i18n.t('nav.repositories')"
    >
      @for (repo of repos.list(); track repo.id) {
        <a
          class="option"
          [routerLink]="link(repo.id)"
          [attr.aria-current]="repo.id === currentId() ? 'true' : null"
          (click)="menu.close()"
        >
          <span class="option__text">
            <span class="option__name">{{ repo.name }}</span>
            <span class="option__meta">
              @if (repo.available) {
                {{ i18n.plural('repo.docs', repo.docCount) }}
              } @else {
                {{ i18n.t('home.unavailable') }}
              }
            </span>
          </span>
          @if (repo.id === currentId()) {
            <tui-icon class="option__check" icon="@tui.check" />
          }
        </a>
      } @empty {
        <p class="empty">{{ i18n.t('home.empty.title') }}</p>
      }
    </nav>
  `,
  styleUrl: './repo-switcher.scss',
})
export class RepoSwitcher {
  protected readonly i18n = inject(I18n);
  protected readonly repos = inject(Repos);
  private readonly route = inject(RouteState);

  protected readonly currentId = this.route.repo;
  protected readonly current = computed(() => this.repos.byId(this.currentId()));

  protected link(id: string): string[] {
    return repoLink(id);
  }
}
