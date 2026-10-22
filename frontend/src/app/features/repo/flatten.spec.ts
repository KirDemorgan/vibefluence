import { TREE } from '../../../testing';
import { flattenDocs, groupByFolder } from './flatten';

describe('flatten', () => {
  it('collects documents from nested folders in tree order', () => {
    const docs = flattenDocs(TREE.roots);
    expect(docs.map((entry) => entry.path)).toEqual(['docs/guide/start.md', 'docs/README.md']);
    expect(docs[0]).toEqual({
      path: 'docs/guide/start.md',
      title: 'Getting started',
      type: 'guide',
      folder: 'docs/guide',
    });
  });

  it('falls back to the file name and an empty folder', () => {
    const docs = flattenDocs([{ name: 'loose.md', path: 'loose.md', kind: 'doc' }]);
    expect(docs).toEqual([{ path: 'loose.md', title: 'loose.md', type: undefined, folder: '' }]);
  });

  it('tolerates folders without children', () => {
    expect(flattenDocs([{ name: 'empty', path: 'empty', kind: 'folder' }])).toEqual([]);
  });

  it('groups documents by folder in alphabetical order', () => {
    const groups = groupByFolder(flattenDocs(TREE.roots));
    expect(groups.map((group) => group.folder)).toEqual(['docs', 'docs/guide']);
    expect(groups[0].docs.map((entry) => entry.title)).toEqual(['Welcome']);
  });
});
