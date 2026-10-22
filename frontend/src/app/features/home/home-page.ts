import { Component, inject } from '@angular/core';
import { RouterLink } from '@angular/router';
import { TuiButton, TuiIcon } from '@taiga-ui/core';
import { I18n } from '../../core/i18n';
import { repoLink } from '../../core/links';
import { usePageTitle } from '../../core/page-title';
import { Repos } from '../../core/repos';
import { Ui } from '../../core/ui';

@Component({
  selector: 'vf-home-page',
  imports: [RouterLink, TuiIcon, TuiButton],
  templateUrl: './home-page.html',
  styleUrl: './home-page.scss',
})
export class HomePage {
  protected readonly i18n = inject(I18n);
  protected readonly repos = inject(Repos);
  protected readonly ui = inject(Ui);

  protected readonly placeholders = [1, 2, 3];

  constructor() {
    usePageTitle(() => []);
  }

  protected link(id: string): string[] {
    return repoLink(id);
  }
}
