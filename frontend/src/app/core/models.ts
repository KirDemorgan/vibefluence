export type DocStatus = 'draft' | 'published';

export interface Repo {
  id: string;
  name: string;
  project: string;
  branch: string;
  paths: string[];
  autoMerge: boolean;
  readOnly: boolean;
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

export interface DraftSummary {
  id: string;
  repo: string;
  path: string;
  title: string;
  status: DocStatus;
  isNew: boolean;
  author: string;
  updatedAt: string;
  baseBlobId: string;
  mergeRequestUrl?: string;
}

export interface Draft extends DraftSummary {
  content: string;
}

export interface SaveDraftBody {
  path: string;
  content: string;
  baseBlobId: string;
  author: string;
}

export type PublishReason =
  | 'auto_merge_disabled'
  | 'pipeline_started'
  | 'pipeline_unknown'
  | 'not_mergeable'
  | 'forbidden'
  | 'refused';

export interface PublishResult {
  outcome: 'merged' | 'pending';
  reason?: PublishReason;
  detail?: string;
  mergeRequestUrl?: string;
  mergeRequestIid?: number;
  created: boolean;
  path: string;
  draftRemoved: boolean;
}

export interface PreviewResponse {
  title: string;
  status: DocStatus;
  type?: string;
  warnings: string[];
  html: string;
  headings: Heading[];
}
