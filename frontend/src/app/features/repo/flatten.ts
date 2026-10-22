import { TreeNode } from '../../core/models';

export interface FlatDoc {
  path: string;
  title: string;
  type?: string;
  folder: string;
}

export function flattenDocs(roots: TreeNode[]): FlatDoc[] {
  const docs: FlatDoc[] = [];
  const walk = (nodes: TreeNode[]): void => {
    for (const node of nodes) {
      if (node.kind === 'doc') {
        const index = node.path.lastIndexOf('/');
        docs.push({
          path: node.path,
          title: node.title || node.name,
          type: node.type,
          folder: index < 0 ? '' : node.path.slice(0, index),
        });
      } else {
        walk(node.children ?? []);
      }
    }
  };
  walk(roots);
  return docs;
}

export interface DocGroup {
  folder: string;
  docs: FlatDoc[];
}

export function groupByFolder(docs: FlatDoc[]): DocGroup[] {
  const groups = new Map<string, FlatDoc[]>();
  for (const doc of docs) {
    const bucket = groups.get(doc.folder) ?? [];
    bucket.push(doc);
    groups.set(doc.folder, bucket);
  }
  return [...groups.entries()]
    .sort(([a], [b]) => a.localeCompare(b))
    .map(([folder, items]) => ({ folder, docs: items }));
}
