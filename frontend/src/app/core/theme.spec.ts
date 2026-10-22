import { TestBed } from '@angular/core/testing';
import { Theme } from './theme';

describe('Theme', () => {
  beforeEach(() => {
    localStorage.clear();
    document.documentElement.removeAttribute('data-theme');
    document.body.removeAttribute('tuiTheme');
  });

  afterEach(() => vi.restoreAllMocks());

  function create(): Theme {
    TestBed.resetTestingModule();
    const theme = TestBed.inject(Theme);
    TestBed.tick();
    return theme;
  }

  it('follows the system by default', () => {
    const theme = create();
    expect(theme.preference()).toBe('system');
    expect(theme.effective()).toBe('light');
    expect(document.documentElement.dataset['theme']).toBe('light');
  });

  it('applies and persists an explicit dark choice', () => {
    const theme = create();
    theme.set('dark');
    TestBed.tick();
    expect(theme.preference()).toBe('dark');
    expect(theme.effective()).toBe('dark');
    expect(document.documentElement.dataset['theme']).toBe('dark');
    expect(localStorage.getItem('vf.theme')).toBe('dark');
  });

  it('switches back to light and then to the system choice', () => {
    const theme = create();
    theme.set('dark');
    theme.set('light');
    TestBed.tick();
    expect(theme.effective()).toBe('light');
    theme.set('system');
    TestBed.tick();
    expect(theme.preference()).toBe('system');
    expect(theme.effective()).toBe('light');
  });

  it('restores the stored preference on start', () => {
    localStorage.setItem('vf.theme', 'dark');
    const theme = create();
    expect(theme.preference()).toBe('dark');
    expect(theme.effective()).toBe('dark');
  });

  it('ignores a corrupted stored value', () => {
    localStorage.setItem('vf.theme', 'purple');
    expect(create().preference()).toBe('system');
  });
});
