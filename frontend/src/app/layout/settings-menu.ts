import { Component, inject } from '@angular/core';
import { TuiIcon } from '@taiga-ui/core';
import { I18n, Lang } from '../core/i18n';
import { Theme, ThemePreference } from '../core/theme';
import { Popover } from '../shared/popover';

@Component({
  selector: 'vf-settings-menu',
  imports: [TuiIcon, Popover],
  template: `
    <button
      #trigger
      type="button"
      class="trigger"
      [attr.aria-label]="i18n.t('settings.open')"
      [attr.aria-expanded]="popover.open()"
      aria-haspopup="true"
      (click)="popover.toggle(trigger)"
    >
      <tui-icon icon="@tui.settings" />
    </button>

    <div vfPopover #popover="vfPopover" class="panel" align="start">
      <section class="group" [attr.aria-label]="i18n.t('theme.label')">
        <h2 class="group__title">{{ i18n.t('theme.label') }}</h2>
        <div class="segments" role="radiogroup" [attr.aria-label]="i18n.t('theme.label')">
          @for (option of themes; track option.value) {
            <button
              type="button"
              class="segment"
              role="radio"
              [attr.aria-checked]="theme.preference() === option.value"
              (click)="theme.set(option.value)"
            >
              <tui-icon [icon]="option.icon" />
              {{ i18n.t(option.label) }}
            </button>
          }
        </div>
      </section>

      <section class="group" [attr.aria-label]="i18n.t('lang.label')">
        <h2 class="group__title">{{ i18n.t('lang.label') }}</h2>
        <div class="segments" role="radiogroup" [attr.aria-label]="i18n.t('lang.label')">
          @for (option of languages; track option.value) {
            <button
              type="button"
              class="segment"
              role="radio"
              [attr.aria-checked]="i18n.lang() === option.value"
              (click)="i18n.setLang(option.value)"
            >
              {{ option.label }}
            </button>
          }
        </div>
      </section>
    </div>
  `,
  styleUrl: './settings-menu.scss',
})
export class SettingsMenu {
  protected readonly i18n = inject(I18n);
  protected readonly theme = inject(Theme);

  protected readonly themes: {
    value: ThemePreference;
    icon: string;
    label: 'theme.system' | 'theme.light' | 'theme.dark';
  }[] = [
    { value: 'system', icon: '@tui.monitor', label: 'theme.system' },
    { value: 'light', icon: '@tui.sun', label: 'theme.light' },
    { value: 'dark', icon: '@tui.moon', label: 'theme.dark' },
  ];

  protected readonly languages: { value: Lang; label: string }[] = [
    { value: 'ru', label: 'Русский' },
    { value: 'en', label: 'English' },
  ];
}
