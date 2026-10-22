import { Component, computed, effect, inject, input, signal } from '@angular/core';
import { RouterLink } from '@angular/router';
import { TuiIcon } from '@taiga-ui/core';
import { I18n } from '../../core/i18n';
import { ancestorsOf, docLink } from '../../core/links';
import { TreeNode } from '../../core/models';

interface VisibleNode {
  node: TreeNode;
  depth: number;
  open: boolean;
}

const STORAGE_PREFIX = 'vf.tree.';

function loadExpanded(repo: string): Set<string> {
  try {
    const raw = sessionStorage.getItem(STORAGE_PREFIX + repo);
    return new Set(raw ? (JSON.parse(raw) as string[]) : []);
  } catch {
    return new Set();
  }
}

function saveExpanded(repo: string, expanded: Set<string>): void {
  try {
    sessionStorage.setItem(STORAGE_PREFIX + repo, JSON.stringify([...expanded]));
  } catch {
    return;
  }
}

function matches(node: TreeNode, terms: string[]): boolean {
  const haystack = `${node.title ?? ''} ${node.name} ${node.path}`.toLowerCase();
  return terms.every((term) => haystack.includes(term));
}

function pruned(nodes: TreeNode[], terms: string[]): TreeNode[] {
  const result: TreeNode[] = [];
  for (const node of nodes) {
    if (node.kind === 'doc') {
      if (matches(node, terms)) {
        result.push(node);
      }
      continue;
    }
    const children = pruned(node.children ?? [], terms);
    if (children.length > 0) {
      result.push({ ...node, children });
    }
  }
  return result;
}

@Component({
  selector: 'vf-doc-tree',
  imports: [RouterLink, TuiIcon],
  templateUrl: './doc-tree.html',
  styleUrl: './doc-tree.scss',
})
export class DocTree {
  protected readonly i18n = inject(I18n);

  readonly repo = input.required<string>();
  readonly roots = input.required<TreeNode[]>();
  readonly activePath = input<string | null>(null);

  protected readonly filter = signal('');
  private readonly expanded = signal<Set<string>>(new Set());

  private readonly terms = computed(() => this.filter().toLowerCase().split(/\s+/).filter(Boolean));
  protected readonly filtering = computed(() => this.terms().length > 0);

  protected readonly rows = computed<VisibleNode[]>(() => {
    const terms = this.terms();
    const roots = terms.length > 0 ? pruned(this.roots(), terms) : this.roots();
    const expanded = this.expanded();
    const rows: VisibleNode[] = [];
    const walk = (nodes: TreeNode[], depth: number): void => {
      for (const node of nodes) {
        const open = node.kind === 'folder' && (terms.length > 0 || expanded.has(node.path));
        rows.push({ node, depth, open });
        if (open) {
          walk(node.children ?? [], depth + 1);
        }
      }
    };
    walk(roots, 0);
    return rows;
  });

  protected readonly empty = computed(() =>
    this.roots().every((root) => (root.children ?? []).length === 0),
  );

  constructor() {
    effect(() => {
      this.expanded.set(loadExpanded(this.repo()));
    });
    effect(() => {
      const active = this.activePath();
      const roots = this.roots();
      if (!active) {
        return;
      }
      const wanted = ancestorsOf(active).filter((path) => hasFolder(roots, path));
      const current = this.expanded();
      if (wanted.every((path) => current.has(path))) {
        return;
      }
      const next = new Set(current);
      wanted.forEach((path) => next.add(path));
      this.expanded.set(next);
    });
  }

  protected link(path: string): string[] {
    return docLink(this.repo(), path);
  }

  protected toggle(path: string): void {
    const next = new Set(this.expanded());
    if (next.has(path)) {
      next.delete(path);
    } else {
      next.add(path);
    }
    this.expanded.set(next);
    saveExpanded(this.repo(), next);
  }

  protected setFilter(event: Event): void {
    this.filter.set((event.target as HTMLInputElement).value);
  }

  protected clearFilter(): void {
    this.filter.set('');
  }

  protected label(node: TreeNode): string {
    return node.kind === 'doc' ? node.title || node.name : node.name;
  }
}

function hasFolder(nodes: TreeNode[], path: string): boolean {
  return nodes.some(
    (node) =>
      node.kind === 'folder' && (node.path === path || hasFolder(node.children ?? [], path)),
  );
}
