import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideApp, TREE } from '../../../testing';
import { TreeNode } from '../../core/models';
import { DocTree } from './doc-tree';

describe('DocTree', () => {
  let fixture: ComponentFixture<DocTree>;

  beforeEach(() => {
    sessionStorage.clear();
    TestBed.configureTestingModule({ imports: [DocTree], providers: provideApp() });
  });

  function create(roots: TreeNode[] = TREE.roots, active: string | null = null): HTMLElement {
    fixture = TestBed.createComponent(DocTree);
    fixture.componentRef.setInput('repo', 'platform');
    fixture.componentRef.setInput('roots', roots);
    fixture.componentRef.setInput('activePath', active);
    fixture.detectChanges();
    return fixture.nativeElement;
  }

  async function update(): Promise<void> {
    fixture.detectChanges();
    await fixture.whenStable();
    fixture.detectChanges();
  }

  const labels = (element: HTMLElement) =>
    [...element.querySelectorAll('.label')].map((label) => label.textContent?.trim());

  it('shows only the top level collapsed at first', async () => {
    const element = create();
    await update();
    expect(labels(element)).toEqual(['docs']);
    expect(element.querySelector('button.row--folder')?.getAttribute('aria-expanded')).toBe(
      'false',
    );
  });

  it('expands and collapses folders and remembers the state', async () => {
    const element = create();
    await update();
    (element.querySelector('button.row--folder') as HTMLButtonElement).click();
    await update();
    expect(labels(element)).toEqual(['docs', 'guide', 'Welcome']);
    expect(JSON.parse(sessionStorage.getItem('vf.tree.platform') ?? '[]')).toEqual(['docs']);

    (element.querySelector('button.row--folder') as HTMLButtonElement).click();
    await update();
    expect(labels(element)).toEqual(['docs']);
    expect(JSON.parse(sessionStorage.getItem('vf.tree.platform') ?? '[]')).toEqual([]);
  });

  it('restores the expanded folders from the session', async () => {
    sessionStorage.setItem('vf.tree.platform', JSON.stringify(['docs']));
    const element = create();
    await update();
    expect(labels(element)).toEqual(['docs', 'guide', 'Welcome']);
  });

  it('survives corrupted session data', async () => {
    sessionStorage.setItem('vf.tree.platform', '{not json');
    const element = create();
    await update();
    expect(labels(element)).toEqual(['docs']);
  });

  it('opens the ancestors of the active document and marks it as current', async () => {
    const element = create(TREE.roots, 'docs/guide/start.md');
    await update();
    expect(labels(element)).toEqual(['docs', 'guide', 'Getting started', 'Welcome']);
    const current = element.querySelector('a[aria-current="page"]');
    expect(current?.textContent?.trim()).toBe('Getting started');
    expect(current?.getAttribute('href')).toBe('/r/platform/docs/guide/start.md');
    expect(current?.getAttribute('title')).toBe('docs/guide/start.md');
  });

  it('does not mark other documents as current', async () => {
    const element = create(TREE.roots, 'docs/README.md');
    await update();
    expect(element.querySelectorAll('a[aria-current="page"]')).toHaveLength(1);
  });

  it('filters documents and expands the matching folders', async () => {
    const element = create();
    await update();
    const input = element.querySelector('input') as HTMLInputElement;
    input.value = 'start';
    input.dispatchEvent(new Event('input'));
    await update();

    expect(labels(element)).toEqual(['docs', 'guide', 'Getting started']);
    expect((element.querySelector('button.row--folder') as HTMLButtonElement).disabled).toBe(true);
  });

  it('matches every term in the title or path', async () => {
    const element = create();
    await update();
    const input = element.querySelector('input') as HTMLInputElement;
    input.value = 'guide  GETTING';
    input.dispatchEvent(new Event('input'));
    await update();
    expect(labels(element)).toContain('Getting started');

    input.value = 'guide welcome';
    input.dispatchEvent(new Event('input'));
    await update();
    expect(element.textContent).toContain('No documents match');
  });

  it('clears the filter with Escape', async () => {
    const element = create();
    await update();
    const input = element.querySelector('input') as HTMLInputElement;
    input.value = 'zzz';
    input.dispatchEvent(new Event('input'));
    await update();
    input.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    await update();
    expect(input.value).toBe('');
    expect(labels(element)).toEqual(['docs']);
  });

  it('says so when there are no documents', async () => {
    const element = create([{ name: 'docs', path: 'docs', kind: 'folder', children: [] }]);
    await update();
    expect(element.textContent).toContain('No documents here yet');
  });

  it('falls back to the file name when a document has no title', async () => {
    const element = create(
      [
        {
          name: 'docs',
          path: 'docs',
          kind: 'folder',
          children: [{ name: 'raw.md', path: 'docs/raw.md', kind: 'doc' }],
        },
      ],
      'docs/raw.md',
    );
    await update();
    expect(labels(element)).toEqual(['docs', 'raw.md']);
  });

  it('keeps state per repository', async () => {
    sessionStorage.setItem('vf.tree.other', JSON.stringify(['docs']));
    const element = create();
    await update();
    expect(labels(element)).toEqual(['docs']);
  });
});
