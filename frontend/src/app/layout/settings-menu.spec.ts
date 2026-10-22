import { TestBed } from '@angular/core/testing';
import { provideApp } from '../../testing';
import { I18n } from '../core/i18n';
import { Theme } from '../core/theme';
import { SettingsMenu } from './settings-menu';

describe('SettingsMenu', () => {
  beforeEach(() => {
    localStorage.clear();
    TestBed.configureTestingModule({ imports: [SettingsMenu], providers: provideApp() });
  });

  function create(): HTMLElement {
    const fixture = TestBed.createComponent(SettingsMenu);
    fixture.detectChanges();
    return fixture.nativeElement;
  }

  const radios = (element: HTMLElement, group: number): HTMLButtonElement[] => {
    const container = element.querySelectorAll('[role="radiogroup"]')[group];
    return Array.from(container.querySelectorAll<HTMLButtonElement>('[role="radio"]'));
  };

  it('opens the panel from the trigger and reflects it in aria-expanded', () => {
    const element = create();
    const trigger = element.querySelector('.trigger') as HTMLButtonElement;
    expect(trigger.getAttribute('aria-expanded')).toBe('false');
    trigger.click();
    TestBed.tick();
    expect(element.querySelector('.panel')?.hasAttribute('data-popover-open')).toBe(true);
  });

  it('lists the three appearances and checks the current one', () => {
    const element = create();
    const buttons = radios(element, 0);
    expect(buttons.map((button) => button.textContent?.trim())).toEqual([
      'System',
      'Light',
      'Dark',
    ]);
    expect(buttons.map((button) => button.getAttribute('aria-checked'))).toEqual([
      'true',
      'false',
      'false',
    ]);
  });

  it('changes the theme', () => {
    const element = create();
    const theme = TestBed.inject(Theme);
    radios(element, 0)[2].click();
    TestBed.tick();
    expect(theme.preference()).toBe('dark');
    expect(radios(element, 0)[2].getAttribute('aria-checked')).toBe('true');
  });

  it('changes the language and relabels the menu', () => {
    const element = create();
    const i18n = TestBed.inject(I18n);
    i18n.setLang('en');
    radios(element, 1)[0].click();
    TestBed.tick();
    expect(i18n.lang()).toBe('ru');
    expect(radios(element, 0).map((button) => button.textContent?.trim())).toEqual([
      'Системное',
      'Светлое',
      'Тёмное',
    ]);
    expect(radios(element, 1)[0].getAttribute('aria-checked')).toBe('true');
  });
});
