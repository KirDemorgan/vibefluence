export type DocStatus = 'draft' | 'published';

export interface Repo {
  id: string;
  name: string;
  project: string;
  branch: string;
  paths: string[];
  autoMerge: boolean;
  docCount: number;
  hiddenCount: number;
  stale: boolean;
  available: boolean;
  webUrl: string;
}

export interface TreeNode {
  name: string;
  path: string;
  kind: 'folder' | 'doc';
  title?: string;
  type?: string;
  children?: TreeNode[];
}

export interface TreeResponse {
  repo: string;
  branch: string;
  stale: boolean;
  roots: TreeNode[];
}

export interface Heading {
  level: number;
  text: string;
  id: string;
}

export interface DocumentResponse {
  repo: string;
  path: string;
  title: string;
  status: DocStatus;
  type?: string;
  warnings: string[];
  html: string;
  headings: Heading[];
  raw: string;
  blobId: string;
  webUrl: string;
  stale: boolean;
}

export interface SearchResult {
  repo: string;
  path: string;
  title: string;
  type?: string;
  snippet: string;
}

export interface SearchResponse {
  query: string;
  results: SearchResult[];
}
