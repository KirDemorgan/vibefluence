import { Component, computed, effect, inject } from '@angular/core';
import { RouterLink, RouterLinkActive, RouterOutlet } from '@angular/router';
import { TuiIcon } from '@taiga-ui/core';
import { I18n } from '../core/i18n';
import { docLink, repoLink } from '../core/links';
import { Repos } from '../core/repos';
import { Drafts } from '../core/drafts';
import { RouteState } from '../core/route-state';
import { Trees } from '../core/trees';
import { Ui } from '../core/ui';
import { SearchPalette } from '../features/search/search-palette';
import { DocTree } from '../features/tree/doc-tree';
import { RepoSwitcher } from './repo-switcher';
import { SettingsMenu } from './settings-menu';

interface Crumb {
  label: string;
  link: string[] | null;
}

@Component({
  selector: 'vf-shell',
  imports: [
    RouterOutlet,
    RouterLink,
    RouterLinkActive,
    TuiIcon,
    DocTree,
    RepoSwitcher,
    SettingsMenu,
    SearchPalette,
  ],
  templateUrl: './shell.html',
  styleUrl: './shell.scss',
  host: {
    '(document:keydown)': 'onKeydown($event)',
  },
})
export class Shell {
  protected readonly i18n = inject(I18n);
  protected readonly ui = inject(Ui);
  protected readonly repos = inject(Repos);
  protected readonly trees = inject(Trees);
  protected readonly drafts = inject(Drafts);
  private readonly route = inject(RouteState);

  protected readonly repoId = this.route.repo;
  protected readonly activePath = this.route.docPath;
  protected readonly canWrite = computed(
    () => this.repos.list().length === 0 || this.repos.list().some((repo) => !repo.readOnly),
  );
  protected readonly repo = computed(() => this.repos.byId(this.repoId()));

  protected readonly crumbs = computed<Crumb[]>(() => {
    const id = this.repoId();
    if (!id) {
      return [];
    }
    const crumbs: Crumb[] = [{ label: this.repo()?.name ?? id, link: repoLink(id) }];
    const path = this.activePath();
    if (path) {
      const segments = path.split('/');
      segments.forEach((segment, index) => {
        const isLast = index === segments.length - 1;
        crumbs.push({ label: segment, link: isLast ? docLink(id, path) : null });
      });
    }
    return crumbs;
  });

  constructor() {
    effect(() => {
      this.route.url();
      this.ui.closeNav();
    });
  }

  protected skipToContent(event: Event): void {
    event.preventDefault();
    document.getElementById('content')?.focus();
  }

  protected onKeydown(event: KeyboardEvent): void {
    const modifier = event.metaKey || event.ctrlKey;
    if (modifier && event.key.toLowerCase() === 'k') {
      event.preventDefault();
      this.ui.openSearch();
      return;
    }
    if (event.key === '/' && !modifier && !isTyping(event.target)) {
      event.preventDefault();
      this.ui.openSearch();
      return;
    }
    if (event.key === 'Escape' && this.ui.navOpen()) {
      this.ui.closeNav();
    }
  }

  protected readonly skeletonWidths = [72, 58, 84, 64, 76, 52, 68];

  protected homeLink(): string[] {
    return ['/'];
  }

  protected link(id: string): string[] {
    return repoLink(id);
  }
}

function isTyping(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) {
    return false;
  }
  return target.isContentEditable || ['INPUT', 'TEXTAREA', 'SELECT'].includes(target.tagName);
}
