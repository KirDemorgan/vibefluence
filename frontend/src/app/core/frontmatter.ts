export type MetaKey = 'title' | 'type' | 'status';

export interface Meta {
  title: string;
  type: string;
  status: string;
}

interface Block {
  start: number;
  end: number;
  lines: string[];
  newline: string;
}

function newlineOf(raw: string): string {
  return raw.includes('\r\n') ? '\r\n' : '\n';
}

function locate(raw: string): Block | null {
  const text = raw.startsWith('﻿') ? raw.slice(1) : raw;
  const newline = newlineOf(text);
  const lines = text.split('\n').map((line) => line.replace(/\r$/, ''));
  if (lines[0]?.trimEnd() !== '---') {
    return null;
  }
  for (let index = 1; index < lines.length; index++) {
    const marker = lines[index].trimEnd();
    if (marker === '---' || marker === '...') {
      return { start: 0, end: index, lines, newline };
    }
  }
  return null;
}

function unquote(value: string): string {
  const trimmed = value.trim();
  if (trimmed.length >= 2 && trimmed.startsWith('"') && trimmed.endsWith('"')) {
    try {
      return JSON.parse(trimmed) as string;
    } catch {
      return trimmed.slice(1, -1);
    }
  }
  if (trimmed.length >= 2 && trimmed.startsWith("'") && trimmed.endsWith("'")) {
    return trimmed.slice(1, -1).replace(/''/g, "'");
  }
  return trimmed;
}

function keyPattern(key: MetaKey): RegExp {
  return new RegExp(`^${key}[ \\t]*:(.*)$`);
}

export function readMeta(raw: string): Meta {
  const meta: Meta = { title: '', type: '', status: '' };
  const block = locate(raw);
  if (!block) {
    return meta;
  }
  for (const key of ['title', 'type', 'status'] as const) {
    for (let index = 1; index < block.end; index++) {
      const match = keyPattern(key).exec(block.lines[index]);
      if (match) {
        meta[key] = unquote(match[1]);
        break;
      }
    }
  }
  return meta;
}

function format(key: MetaKey, value: string): string {
  return key === 'status' ? `${key}: ${value}` : `${key}: ${JSON.stringify(value)}`;
}

export function setMeta(raw: string, key: MetaKey, value: string): string {
  const text = raw.startsWith('﻿') ? raw.slice(1) : raw;
  const block = locate(text);
  const newline = newlineOf(text);

  if (!block) {
    if (value === '') {
      return raw;
    }
    return ['---', format(key, value), '---', '', text].join(newline);
  }

  const lines = block.lines;
  for (let index = 1; index < block.end; index++) {
    if (keyPattern(key).test(lines[index])) {
      if (value === '') {
        lines.splice(index, 1);
      } else {
        lines[index] = format(key, value);
      }
      return lines.join(newline);
    }
  }
  if (value === '') {
    return raw;
  }
  lines.splice(block.end, 0, format(key, value));
  return lines.join(newline);
}

export function slugify(title: string): string {
  const slug = title
    .toLowerCase()
    .replace(/[^\p{L}\p{N}]+/gu, '-')
    .replace(/^-+|-+$/g, '')
    .slice(0, 60)
    .replace(/-+$/g, '');
  return slug || 'untitled';
}

export function suggestPath(root: string, title: string): string {
  return `${root}/${slugify(title)}.md`;
}

export function isValidPath(path: string, roots: string[]): boolean {
  if (
    !path ||
    path.length > 240 ||
    path.includes('\\') ||
    path.startsWith('/') ||
    path.endsWith('/')
  ) {
    return false;
  }
  const segments = path.split('/');
  if (
    segments.some(
      (segment) =>
        segment === '' ||
        segment === '.' ||
        segment === '..' ||
        segment.startsWith('.') ||
        segment !== segment.trim(),
    )
  ) {
    return false;
  }
  if (!/\.(md|markdown)$/i.test(path)) {
    return false;
  }
  if (hasControlCharacter(path)) {
    return false;
  }
  return roots.some((root) => path.startsWith(`${root}/`));
}

export function hasControlCharacter(value: string): boolean {
  for (const char of value) {
    const code = char.codePointAt(0) ?? 0;
    if (code < 0x20 || code === 0x7f) {
      return true;
    }
  }
  return false;
}

export function bodyOf(raw: string): string {
  const text = raw.startsWith('\ufeff') ? raw.slice(1) : raw;
  const block = locate(text);
  if (!block) {
    return text;
  }
  return block.lines.slice(block.end + 1).join(block.newline);
}

export function isPristineBody(raw: string, title: string): boolean {
  const body = bodyOf(raw).trim();
  return body === '' || /^#\s*$/.test(body) || body === `# ${title}`.trim();
}
