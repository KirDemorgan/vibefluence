import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { BackendStatus, HealthService } from './health.service';

describe('HealthService', () => {
  let service: HealthService;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [provideHttpClient(), provideHttpClientTesting()],
    });
    service = TestBed.inject(HealthService);
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  function check(respond: (request: ReturnType<HttpTestingController['expectOne']>) => void) {
    let result: BackendStatus | undefined;
    service.check().subscribe((status) => (result = status));
    respond(http.expectOne('/healthz'));
    return result;
  }

  it('reports ok when the backend says ok', () => {
    expect(check((request) => request.flush({ status: 'ok' }))).toBe('ok');
  });

  it('reports unavailable when the backend reports another status', () => {
    expect(check((request) => request.flush({ status: 'unavailable' }))).toBe('unavailable');
  });

  it('reports unavailable on an HTTP error', () => {
    expect(
      check((request) => request.flush('boom', { status: 503, statusText: 'Service Unavailable' })),
    ).toBe('unavailable');
  });

  it('reports unavailable on a network error', () => {
    expect(check((request) => request.error(new ProgressEvent('error')))).toBe('unavailable');
  });

  it('uses GET', () => {
    service.check().subscribe();
    const request = http.expectOne('/healthz');
    expect(request.request.method).toBe('GET');
    request.flush({ status: 'ok' });
  });
});
