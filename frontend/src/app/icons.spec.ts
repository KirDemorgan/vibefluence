import { readFileSync, readdirSync, statSync } from 'node:fs';
import { join } from 'node:path';

function sources(directory: string): string[] {
  return readdirSync(directory).flatMap((name) => {
    const full = join(directory, name);
    if (statSync(full).isDirectory()) {
      return sources(full);
    }
    return /\.(ts|html)$/.test(name) && !name.endsWith('.spec.ts') ? [full] : [];
  });
}

function copiedIcons(): Set<string> {
  const config = JSON.parse(readFileSync('angular.json', 'utf8')) as {
    projects: Record<
      string,
      { architect: { build: { options: { assets: { glob: string; output?: string }[] } } } }
    >;
  };
  const assets = Object.values(config.projects)[0].architect.build.options.assets;
  const entry = assets.find((asset) => asset.output === 'assets/taiga-ui/icons');
  const match = /^\{(.+)\}\.svg$/.exec(entry?.glob ?? '');
  return new Set(match ? match[1].split(',') : []);
}

describe('icons', () => {
  it('copies every icon the templates use', () => {
    const copied = copiedIcons();
    const used = new Set<string>();
    for (const file of sources('src')) {
      for (const match of readFileSync(file, 'utf8').matchAll(/@tui\.([a-z0-9-]+)/g)) {
        used.add(match[1]);
      }
    }
    const missing = [...used].filter((name) => !copied.has(name));
    expect(missing).toEqual([]);
    expect(used.size).toBeGreaterThan(10);
  });
});
