import { ancestorsOf, docLink, docPathFromUrl, folderOf, repoFromUrl, repoLink } from './links';

describe('links', () => {
  it('builds router commands for documents and repositories', () => {
    expect(docLink('platform', 'docs/guide/start.md')).toEqual([
      '/r',
      'platform',
      'docs',
      'guide',
      'start.md',
    ]);
    expect(repoLink('platform')).toEqual(['/r', 'platform']);
  });

  it('reads the repository from a URL', () => {
    expect(repoFromUrl('/r/platform')).toBe('platform');
    expect(repoFromUrl('/r/platform/docs/a.md?x=1#h-y')).toBe('platform');
    expect(repoFromUrl('/r/my%20repo/docs/a.md')).toBe('my repo');
    expect(repoFromUrl('/')).toBeNull();
    expect(repoFromUrl('/other/platform')).toBeNull();
  });

  it('reads the document path from a URL', () => {
    expect(docPathFromUrl('/r/platform/docs/guide/start.md')).toBe('docs/guide/start.md');
    expect(docPathFromUrl('/r/platform/docs/my%20file.md?x=1#h-y')).toBe('docs/my file.md');
    expect(docPathFromUrl('/r/platform')).toBeNull();
    expect(docPathFromUrl('/')).toBeNull();
  });

  it('computes folders and ancestors', () => {
    expect(folderOf('docs/guide/start.md')).toBe('docs/guide');
    expect(folderOf('README.md')).toBe('');
    expect(ancestorsOf('docs/guide/start.md')).toEqual(['docs', 'docs/guide']);
    expect(ancestorsOf('README.md')).toEqual([]);
  });
});
