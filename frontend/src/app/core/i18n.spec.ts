import { TestBed } from '@angular/core/testing';
import { I18n, MESSAGES } from './i18n';

describe('I18n', () => {
  beforeEach(() => localStorage.clear());

  function create(language?: string): I18n {
    if (language) {
      vi.spyOn(navigator, 'language', 'get').mockReturnValue(language);
    }
    TestBed.resetTestingModule();
    return TestBed.inject(I18n);
  }

  afterEach(() => vi.restoreAllMocks());

  it('has a Russian translation for every English message', () => {
    const missing = Object.keys(MESSAGES.en).filter((key) => !(key in MESSAGES.ru));
    expect(missing).toEqual([]);
  });

  it('has no Russian messages without an English counterpart (except plural forms)', () => {
    const extra = Object.keys(MESSAGES.ru).filter(
      (key) => !(key in MESSAGES.en) && !/\.(one|few|many|other)$/.test(key),
    );
    expect(extra).toEqual([]);
  });

  it('keeps placeholders consistent between languages', () => {
    for (const key of Object.keys(MESSAGES.en)) {
      const placeholders = (text: string) => (text.match(/\{\w+\}/g) ?? []).sort().join();
      expect(placeholders(MESSAGES.ru[key]), key).toBe(placeholders(MESSAGES.en[key]));
    }
  });

  it('detects Russian from the browser language', () => {
    expect(create('ru-RU').lang()).toBe('ru');
  });

  it('falls back to English for other languages', () => {
    expect(create('de-DE').lang()).toBe('en');
  });

  it('prefers the stored language', () => {
    localStorage.setItem('vf.lang', 'ru');
    expect(create('en-US').lang()).toBe('ru');
  });

  it('ignores a corrupted stored language', () => {
    localStorage.setItem('vf.lang', 'klingon');
    expect(create('en-US').lang()).toBe('en');
  });

  it('switches language, persists it and updates the document language', async () => {
    const i18n = create('en-US');
    i18n.setLang('ru');
    TestBed.tick();
    expect(i18n.lang()).toBe('ru');
    expect(localStorage.getItem('vf.lang')).toBe('ru');
    expect(document.documentElement.lang).toBe('ru');
    expect(i18n.t('nav.home')).toBe('Главная');
  });

  it('interpolates parameters and keeps unknown placeholders', () => {
    const i18n = create('en-US');
    expect(i18n.t('search.empty', { query: 'x' })).toBe('Nothing found for “x”');
    expect(i18n.t('search.empty')).toBe('Nothing found for “{query}”');
    expect(i18n.t('search.empty', { other: 1 })).toBe('Nothing found for “{query}”');
  });

  it('pluralises English', () => {
    const i18n = create('en-US');
    expect(i18n.plural('repo.docs', 1)).toBe('1 document');
    expect(i18n.plural('repo.docs', 0)).toBe('0 documents');
    expect(i18n.plural('repo.docs', 5)).toBe('5 documents');
  });

  it('pluralises Russian with the four forms', () => {
    const i18n = create('ru-RU');
    expect(i18n.plural('repo.docs', 1)).toBe('1 документ');
    expect(i18n.plural('repo.docs', 2)).toBe('2 документа');
    expect(i18n.plural('repo.docs', 5)).toBe('5 документов');
    expect(i18n.plural('repo.docs', 21)).toBe('21 документ');
    expect(i18n.plural('repo.docs', 11)).toBe('11 документов');
    expect(i18n.plural('repo.drafts', 3)).toBe('3 черновика скрыто');
  });

  it('survives unavailable storage', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    const i18n = create('ru-RU');
    expect(i18n.lang()).toBe('ru');
    expect(() => i18n.setLang('en')).not.toThrow();
    expect(i18n.lang()).toBe('en');
  });
});
