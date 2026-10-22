import { HttpClient } from '@angular/common/http';
import { Injectable, inject } from '@angular/core';
import { Observable, catchError, map, of } from 'rxjs';

export type BackendStatus = 'ok' | 'unavailable';

interface HealthResponse {
  status: string;
}

@Injectable({ providedIn: 'root' })
export class HealthService {
  private readonly http = inject(HttpClient);

  check(): Observable<BackendStatus> {
    return this.http.get<HealthResponse>('/healthz').pipe(
      map((body): BackendStatus => (body.status === 'ok' ? 'ok' : 'unavailable')),
      catchError(() => of<BackendStatus>('unavailable')),
    );
  }
}
