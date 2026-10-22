import { highlight } from './highlight';

describe('highlight', () => {
  it('splits text around matches, ignoring case', () => {
    expect(highlight('Deploy the Deployment', 'deploy')).toEqual([
      { text: 'Deploy', hit: true },
      { text: ' the ', hit: false },
      { text: 'Deploy', hit: true },
      { text: 'ment', hit: false },
    ]);
  });

  it('highlights every term', () => {
    const segments = highlight('alpha beta gamma', 'beta alpha');
    expect(segments.filter((segment) => segment.hit).map((segment) => segment.text)).toEqual([
      'alpha',
      'beta',
    ]);
  });

  it('returns the whole text when there is nothing to highlight', () => {
    expect(highlight('plain text', '')).toEqual([{ text: 'plain text', hit: false }]);
    expect(highlight('plain text', '   ')).toEqual([{ text: 'plain text', hit: false }]);
    expect(highlight('', 'x')).toEqual([]);
  });

  it('escapes regular expression characters in the query', () => {
    expect(highlight('cost is $5 (approx.)', '$5 (approx.)')).toEqual([
      { text: 'cost is ', hit: false },
      { text: '$5', hit: true },
      { text: ' ', hit: false },
      { text: '(approx.)', hit: true },
    ]);
    expect(() => highlight('x', '[')).not.toThrow();
  });

  it('works with Cyrillic', () => {
    expect(highlight('Как развернуть сервис', 'развернуть')).toEqual([
      { text: 'Как ', hit: false },
      { text: 'развернуть', hit: true },
      { text: ' сервис', hit: false },
    ]);
  });
});
