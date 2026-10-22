import { HttpClient, HttpErrorResponse, HttpParams } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable, catchError, map, throwError } from 'rxjs';
import {
  Draft,
  DraftSummary,
  DocumentResponse,
  PreviewResponse,
  PublishResult,
  Repo,
  SaveDraftBody,
  SearchResponse,
  TreeResponse,
} from './models';

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
    readonly details: Record<string, unknown> = {},
  ) {
    super(message);
    this.name = 'ApiError';
  }

  get isNotFound(): boolean {
    return this.status === 404;
  }

  get isConflict(): boolean {
    return this.status === 409 && this.code === 'conflict';
  }

  get isBusy(): boolean {
    return this.status === 409 && this.code === 'busy';
  }

  get isUpstream(): boolean {
    return this.status === 502 || this.status === 504;
  }
}

interface ErrorBody {
  error?: string;
  message?: string;
}

function toApiError(failure: unknown): ApiError {
  if (failure instanceof HttpErrorResponse) {
    const body = (failure.error ?? {}) as ErrorBody;
    const code =
      typeof body.error === 'string' ? body.error : failure.status === 0 ? 'network' : 'unknown';
    const message = typeof body.message === 'string' ? body.message : failure.message;
    return new ApiError(
      failure.status,
      code,
      message,
      typeof failure.error === 'object' && failure.error
        ? (failure.error as Record<string, unknown>)
        : {},
    );
  }
  return new ApiError(0, 'unknown', failure instanceof Error ? failure.message : 'unknown error');
}

const API = '/api/v1';

@Injectable({ providedIn: 'root' })
export class Api {
  private readonly http = inject(HttpClient);

  repos(): Observable<Repo[]> {
    return this.http.get<{ repos: Repo[] }>(`${API}/repos`).pipe(
      map((body) => body.repos),
      catchError((failure) => throwError(() => toApiError(failure))),
    );
  }

  tree(repo: string): Observable<TreeResponse> {
    return this.get<TreeResponse>(`${API}/repos/${encodeURIComponent(repo)}/tree`);
  }

  document(repo: string, path: string): Observable<DocumentResponse> {
    return this.get<DocumentResponse>(`${API}/repos/${encodeURIComponent(repo)}/doc`, { path });
  }

  search(query: string, repo?: string, limit?: number): Observable<SearchResponse> {
    const params: Record<string, string> = { q: query };
    if (repo) {
      params['repo'] = repo;
    }
    if (limit) {
      params['limit'] = String(limit);
    }
    return this.get<SearchResponse>(`${API}/search`, params);
  }

  refresh(repo: string): Observable<Repo> {
    return this.http
      .post<Repo>(`${API}/repos/${encodeURIComponent(repo)}/refresh`, null)
      .pipe(catchError((failure) => throwError(() => toApiError(failure))));
  }

  allDrafts(): Observable<DraftSummary[]> {
    return this.get<{ drafts: DraftSummary[] }>(`${API}/drafts`).pipe(map((body) => body.drafts));
  }

  drafts(repo: string): Observable<DraftSummary[]> {
    return this.get<{ drafts: DraftSummary[] }>(
      `${API}/repos/${encodeURIComponent(repo)}/drafts`,
    ).pipe(map((body) => body.drafts));
  }

  draft(repo: string, id: string): Observable<Draft> {
    return this.get<Draft>(
      `${API}/repos/${encodeURIComponent(repo)}/drafts/${encodeURIComponent(id)}`,
    );
  }

  saveDraft(repo: string, body: SaveDraftBody): Observable<Draft> {
    return this.write<Draft>('put', `${API}/repos/${encodeURIComponent(repo)}/drafts`, body);
  }

  deleteDraft(repo: string, id: string): Observable<void> {
    return this.http
      .delete<void>(`${API}/repos/${encodeURIComponent(repo)}/drafts/${encodeURIComponent(id)}`)
      .pipe(catchError((failure) => throwError(() => toApiError(failure))));
  }

  publish(
    repo: string,
    id: string,
    body: { author: string; force: boolean },
  ): Observable<PublishResult> {
    return this.write<PublishResult>(
      'post',
      `${API}/repos/${encodeURIComponent(repo)}/drafts/${encodeURIComponent(id)}/publish`,
      body,
    );
  }

  preview(repo: string, body: { path: string; content: string }): Observable<PreviewResponse> {
    return this.write<PreviewResponse>(
      'post',
      `${API}/repos/${encodeURIComponent(repo)}/preview`,
      body,
    );
  }

  private write<T>(method: 'put' | 'post', url: string, body: object): Observable<T> {
    const request = method === 'put' ? this.http.put<T>(url, body) : this.http.post<T>(url, body);
    return request.pipe(catchError((failure) => throwError(() => toApiError(failure))));
  }

  private get<T>(url: string, params?: Record<string, string>): Observable<T> {
    return this.http
      .get<T>(url, { params: new HttpParams({ fromObject: params ?? {} }) })
      .pipe(catchError((failure) => throwError(() => toApiError(failure))));
  }
}
