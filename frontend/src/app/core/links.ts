export function docLink(repo: string, path: string): string[] {
  return ['/r', repo, ...path.split('/')];
}

export function repoLink(repo: string): string[] {
  return ['/r', repo];
}

export function repoFromUrl(url: string): string | null {
  const match = /^\/(?:r|new|edit)\/([^/?#]+)/.exec(url);
  return match ? decodeURIComponent(match[1]) : null;
}

export function docPathFromUrl(url: string): string | null {
  const match = /^\/(?:r|edit)\/[^/?#]+\/([^?#]+)/.exec(url);
  if (!match) {
    return null;
  }
  return match[1].split('/').map(decodeURIComponent).join('/');
}

export function folderOf(path: string): string {
  const index = path.lastIndexOf('/');
  return index < 0 ? '' : path.slice(0, index);
}

export function ancestorsOf(path: string): string[] {
  const segments = path.split('/');
  const ancestors: string[] = [];
  for (let i = 1; i < segments.length; i++) {
    ancestors.push(segments.slice(0, i).join('/'));
  }
  return ancestors;
}

export function editLink(repo: string, path: string): string[] {
  return ['/edit', repo, ...path.split('/')];
}

export function newLink(repo: string): string[] {
  return ['/new', repo];
}
