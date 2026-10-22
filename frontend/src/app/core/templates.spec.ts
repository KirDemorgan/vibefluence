import { DOC_TYPES, bodyTemplate, isDocType } from './templates';

describe('templates', () => {
  it('has a template for every type in both languages', () => {
    for (const type of DOC_TYPES) {
      expect(bodyTemplate(type, 'en').length, `en ${type}`).toBeGreaterThan(10);
      expect(bodyTemplate(type, 'ru').length, `ru ${type}`).toBeGreaterThan(10);
    }
  });

  it('returns nothing for an unknown type', () => {
    expect(bodyTemplate('', 'en')).toBe('');
    expect(bodyTemplate('whatever', 'ru')).toBe('');
  });

  it('recognises known types only', () => {
    expect(isDocType('adr')).toBe(true);
    expect(isDocType('ADR')).toBe(false);
    expect(isDocType('')).toBe(false);
  });
});
