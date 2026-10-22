const UNITS: { unit: Intl.RelativeTimeFormatUnit; seconds: number }[] = [
  { unit: 'year', seconds: 365 * 24 * 3600 },
  { unit: 'month', seconds: 30 * 24 * 3600 },
  { unit: 'week', seconds: 7 * 24 * 3600 },
  { unit: 'day', seconds: 24 * 3600 },
  { unit: 'hour', seconds: 3600 },
  { unit: 'minute', seconds: 60 },
];

export function relativeTime(iso: string, lang: string, now: Date = new Date()): string {
  const then = new Date(iso).getTime();
  if (Number.isNaN(then)) {
    return '';
  }
  const formatter = new Intl.RelativeTimeFormat(lang, { numeric: 'auto' });
  const elapsed = Math.round((then - now.getTime()) / 1000);
  for (const { unit, seconds } of UNITS) {
    if (Math.abs(elapsed) >= seconds) {
      return formatter.format(Math.round(elapsed / seconds), unit);
    }
  }
  return formatter.format(0, 'second');
}
