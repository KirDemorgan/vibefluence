import { UrlSegment } from '@angular/router';
import { docMatcher } from './app.routes';

function segments(...paths: string[]): UrlSegment[] {
  return paths.map((path) => new UrlSegment(path, {}));
}

describe('docMatcher', () => {
  it('matches a repository followed by a document path', () => {
    const result = docMatcher(segments('r', 'platform', 'docs', 'guide', 'start.md'));
    expect(result?.consumed).toHaveLength(5);
    expect(result?.posParams?.['repo'].path).toBe('platform');
    expect(result?.posParams?.['path'].path).toBe('docs/guide/start.md');
  });

  it('matches a single segment document path', () => {
    const result = docMatcher(segments('r', 'platform', 'README.md'));
    expect(result?.posParams?.['path'].path).toBe('README.md');
  });

  it('does not match the repository page itself', () => {
    expect(docMatcher(segments('r', 'platform'))).toBeNull();
  });

  it('does not match other prefixes or the root', () => {
    expect(docMatcher(segments('x', 'platform', 'a.md'))).toBeNull();
    expect(docMatcher([])).toBeNull();
    expect(docMatcher(segments('r'))).toBeNull();
  });
});
