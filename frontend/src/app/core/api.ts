import { HttpClient, HttpErrorResponse, HttpParams } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable, catchError, map, throwError } from 'rxjs';
import { DocumentResponse, Repo, SearchResponse, TreeResponse } from './models';

export class ApiError extends Error {
  constructor(
    readonly status: number,
    readonly code: string,
    message: string,
  ) {
    super(message);
    this.name = 'ApiError';
  }

  get isNotFound(): boolean {
    return this.status === 404;
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
    return new ApiError(failure.status, code, message);
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

  private get<T>(url: string, params?: Record<string, string>): Observable<T> {
    return this.http
      .get<T>(url, { params: new HttpParams({ fromObject: params ?? {} }) })
      .pipe(catchError((failure) => throwError(() => toApiError(failure))));
  }
}
