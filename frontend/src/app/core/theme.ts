import { Injectable, computed, effect, inject, signal } from '@angular/core';
import { TUI_DARK_MODE } from '@taiga-ui/core';

export type ThemePreference = 'system' | 'light' | 'dark';

const STORAGE_KEY = 'vf.theme';

function stored(): ThemePreference {
  try {
    const value = localStorage.getItem(STORAGE_KEY);
    if (value === 'light' || value === 'dark' || value === 'system') {
      return value;
    }
  } catch {
    return 'system';
  }
  return 'system';
}

@Injectable({ providedIn: 'root' })
export class Theme {
  private readonly darkMode = inject(TUI_DARK_MODE);

  readonly preference = signal<ThemePreference>(stored());
  readonly effective = computed<'light' | 'dark'>(() => (this.darkMode() ? 'dark' : 'light'));

  constructor() {
    this.apply(this.preference());
    effect(() => {
      document.documentElement.dataset['theme'] = this.effective();
    });
  }

  set(preference: ThemePreference): void {
    this.preference.set(preference);
    try {
      localStorage.setItem(STORAGE_KEY, preference);
    } catch {
      this.apply(preference);
      return;
    }
    this.apply(preference);
  }

  private apply(preference: ThemePreference): void {
    if (preference === 'system') {
      this.darkMode.reset();
    } else {
      this.darkMode.set(preference === 'dark');
    }
  }
}
