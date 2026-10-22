import { relativeTime } from './time';

const NOW = new Date('2026-10-22T12:00:00Z');

describe('relativeTime', () => {
  it('says "now" for very recent moments', () => {
    expect(relativeTime('2026-10-22T11:59:40Z', 'en', NOW)).toBe('now');
    expect(relativeTime('2026-10-22T12:00:00Z', 'en', NOW)).toBe('now');
  });

  it('uses minutes, hours, days, weeks, months and years', () => {
    expect(relativeTime('2026-10-22T11:55:00Z', 'en', NOW)).toBe('5 minutes ago');
    expect(relativeTime('2026-10-22T09:00:00Z', 'en', NOW)).toBe('3 hours ago');
    expect(relativeTime('2026-10-21T12:00:00Z', 'en', NOW)).toBe('yesterday');
    expect(relativeTime('2026-10-15T12:00:00Z', 'en', NOW)).toBe('last week');
    expect(relativeTime('2026-08-22T12:00:00Z', 'en', NOW)).toBe('2 months ago');
    expect(relativeTime('2024-10-22T12:00:00Z', 'en', NOW)).toBe('2 years ago');
  });

  it('formats in Russian', () => {
    expect(relativeTime('2026-10-22T09:00:00Z', 'ru', NOW)).toContain('3 часа назад');
  });

  it('handles the future', () => {
    expect(relativeTime('2026-10-22T13:00:00Z', 'en', NOW)).toBe('in 1 hour');
  });

  it('returns nothing for an invalid date', () => {
    expect(relativeTime('not a date', 'en', NOW)).toBe('');
  });

  it('defaults to the current time', () => {
    expect(relativeTime(new Date().toISOString(), 'en')).toBe('now');
  });
});
