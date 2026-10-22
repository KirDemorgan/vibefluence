import { Component, inject } from '@angular/core';
import { RouterLink } from '@angular/router';
import { TuiButton, TuiIcon } from '@taiga-ui/core';
import { I18n } from '../../core/i18n';
import { usePageTitle } from '../../core/page-title';

@Component({
  selector: 'vf-not-found-page',
  imports: [RouterLink, TuiIcon, TuiButton],
  template: `
    <section class="state">
      <tui-icon class="state__icon" icon="@tui.file-search" />
      <h1 class="state__title">{{ i18n.t('notFound.title') }}</h1>
      <p class="state__text">{{ i18n.t('notFound.text') }}</p>
      <a tuiButton appearance="secondary" size="m" routerLink="/">{{ i18n.t('notFound.home') }}</a>
    </section>
  `,
  styles: `
    .state {
      display: flex;
      flex-direction: column;
      align-items: center;
      gap: 0.7rem;
      padding: 6rem 1.5rem;
      text-align: center;
    }
    .state__icon {
      width: 2.4rem;
      height: 2.4rem;
      color: var(--vf-text-3);
    }
    .state__title {
      margin: 0;
      font-size: 1.6rem;
      font-weight: 650;
      letter-spacing: -0.02em;
    }
    .state__text {
      margin: 0 0 0.8rem;
      color: var(--vf-text-2);
    }
  `,
})
export class NotFoundPage {
  protected readonly i18n = inject(I18n);

  constructor() {
    usePageTitle(() => [this.i18n.t('notFound.title')]);
  }
}
