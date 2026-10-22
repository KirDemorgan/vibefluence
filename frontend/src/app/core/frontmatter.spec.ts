import {
  bodyOf,
  hasControlCharacter,
  isPristineBody,
  isValidPath,
  readMeta,
  setMeta,
  slugify,
  suggestPath,
} from './frontmatter';

describe('readMeta', () => {
  it('reads title, type and status', () => {
    expect(readMeta('---\ntitle: "Rollout plan"\ntype: plan\nstatus: draft\n---\n# Body')).toEqual({
      title: 'Rollout plan',
      type: 'plan',
      status: 'draft',
    });
  });

  it('returns empty values without front matter', () => {
    expect(readMeta('# Just a heading')).toEqual({ title: '', type: '', status: '' });
    expect(readMeta('')).toEqual({ title: '', type: '', status: '' });
  });

  it('ignores an unterminated block', () => {
    expect(readMeta('---\ntitle: x\nno end')).toEqual({ title: '', type: '', status: '' });
  });

  it('understands quoting styles', () => {
    expect(readMeta("---\ntitle: 'It''s fine'\n---\n").title).toBe("It's fine");
    expect(readMeta('---\ntitle: "Say \\"hi\\""\n---\n').title).toBe('Say "hi"');
    expect(readMeta('---\ntitle: plain words\n---\n').title).toBe('plain words');
    expect(readMeta('---\ntitle: "broken\n---\n').title).toBe('"broken');
  });

  it('accepts windows line endings and the dots terminator', () => {
    expect(readMeta('---\r\ntitle: Win\r\n...\r\nbody')).toMatchObject({ title: 'Win' });
  });

  it('does not read keys from the body', () => {
    expect(readMeta('---\ntitle: T\n---\nstatus: draft\n').status).toBe('');
  });

  it('ignores a byte order mark', () => {
    expect(readMeta('﻿---\ntitle: Bom\n---\n').title).toBe('Bom');
  });
});

describe('setMeta', () => {
  it('replaces an existing value', () => {
    expect(setMeta('---\ntitle: "Old"\nstatus: draft\n---\nbody', 'status', 'published')).toBe(
      '---\ntitle: "Old"\nstatus: published\n---\nbody',
    );
  });

  it('adds a missing key to an existing block', () => {
    expect(setMeta('---\ntitle: "T"\n---\nbody', 'type', 'adr')).toBe(
      '---\ntitle: "T"\ntype: "adr"\n---\nbody',
    );
  });

  it('creates a block when there is none', () => {
    expect(setMeta('# Heading\n', 'status', 'draft')).toBe(
      '---\nstatus: draft\n---\n\n# Heading\n',
    );
  });

  it('quotes titles and keeps them parseable', () => {
    const raw = setMeta('body', 'title', 'Say "hi": a\\b');
    expect(readMeta(raw).title).toBe('Say "hi": a\\b');
  });

  it('removes a key when the value is empty', () => {
    expect(setMeta('---\ntitle: "T"\ntype: "adr"\n---\nbody', 'type', '')).toBe(
      '---\ntitle: "T"\n---\nbody',
    );
  });

  it('leaves the text alone when clearing an absent key or an absent block', () => {
    expect(setMeta('body', 'type', '')).toBe('body');
    expect(setMeta('---\ntitle: "T"\n---\nbody', 'type', '')).toBe('---\ntitle: "T"\n---\nbody');
  });

  it('keeps windows line endings', () => {
    expect(setMeta('---\r\nstatus: draft\r\n---\r\nx', 'status', 'published')).toBe(
      '---\r\nstatus: published\r\n---\r\nx',
    );
    expect(setMeta('text\r\nmore', 'status', 'draft')).toBe(
      '---\r\nstatus: draft\r\n---\r\n\r\ntext\r\nmore',
    );
  });

  it('does not touch lookalike lines in the body', () => {
    const raw = '---\ntitle: "T"\n---\nstatus: draft\n';
    expect(setMeta(raw, 'status', 'published')).toBe(
      '---\ntitle: "T"\nstatus: published\n---\nstatus: draft\n',
    );
  });

  it('is the inverse of readMeta for every key', () => {
    let raw = '# Body\n';
    raw = setMeta(raw, 'title', 'Заголовок');
    raw = setMeta(raw, 'type', 'spec');
    raw = setMeta(raw, 'status', 'draft');
    expect(readMeta(raw)).toEqual({ title: 'Заголовок', type: 'spec', status: 'draft' });
  });
});

describe('paths', () => {
  it('slugifies titles', () => {
    expect(slugify('Rollout Plan')).toBe('rollout-plan');
    expect(slugify('  Что-то  новое!  ')).toBe('что-то-новое');
    expect(slugify('***')).toBe('untitled');
    expect(slugify('')).toBe('untitled');
    expect(slugify('x'.repeat(100))).toHaveLength(60);
    expect(slugify('a'.repeat(59) + ' b')).toBe('a'.repeat(59));
  });

  it('suggests a path inside the root', () => {
    expect(suggestPath('docs', 'Rollout Plan')).toBe('docs/rollout-plan.md');
  });

  it('accepts only safe paths inside the roots', () => {
    const roots = ['docs', 'specs/internal'];
    for (const ok of [
      'docs/a.md',
      'docs/guide/start.markdown',
      'specs/internal/x.MD',
      'docs/спека.md',
    ]) {
      expect(isValidPath(ok, roots), ok).toBe(true);
    }
    for (const bad of [
      '',
      '/docs/a.md',
      'docs/a.md/',
      'docs\\a.md',
      'docs/../a.md',
      'docs//a.md',
      'docs/./a.md',
      'docs/.hidden/a.md',
      'docs/a.txt',
      'other/a.md',
      'docsx/a.md',
      'docs/ a.md',
      'docs/a\u0000.md',
      'docs/' + 'x'.repeat(300) + '.md',
    ]) {
      expect(isValidPath(bad, roots), JSON.stringify(bad)).toBe(false);
    }
  });

  it('detects control characters', () => {
    expect(hasControlCharacter('plain')).toBe(false);
    expect(hasControlCharacter('tab\there')).toBe(true);
    expect(hasControlCharacter('del\u007f')).toBe(true);
    expect(hasControlCharacter('')).toBe(false);
  });
});

describe('bodyOf', () => {
  it('returns the text after the front matter', () => {
    expect(bodyOf('---\ntitle: "T"\n---\n# Body\n')).toBe('# Body\n');
    expect(bodyOf('---\r\ntitle: "T"\r\n---\r\nbody')).toBe('body');
  });

  it('returns the whole text without front matter', () => {
    expect(bodyOf('# Body')).toBe('# Body');
    expect(bodyOf('\ufeff# Body')).toBe('# Body');
    expect(bodyOf('---\nunterminated')).toBe('---\nunterminated');
  });

  it('knows whether a body is still untouched', () => {
    expect(isPristineBody('---\nstatus: draft\n---\n\n', '')).toBe(true);
    expect(isPristineBody('---\nstatus: draft\n---\n\n# \n', '')).toBe(true);
    expect(isPristineBody('---\nstatus: draft\n---\n\n# Plan\n', 'Plan')).toBe(true);
    expect(isPristineBody('---\nstatus: draft\n---\n\n# Plan\n\ntext', 'Plan')).toBe(false);
    expect(isPristineBody('plain words', '')).toBe(false);
  });
});
