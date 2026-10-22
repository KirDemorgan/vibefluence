import { Location } from '@angular/common';
import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';
import { provideRouter } from '@angular/router';
import { RouterTestingHarness } from '@angular/router/testing';
import { provideTaiga } from '@taiga-ui/core';
import { routes } from './app.routes';
import { Home } from './features/home/home';

describe('routes', () => {
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        provideRouter(routes),
        provideHttpClient(),
        provideHttpClientTesting(),
        provideTaiga(),
      ],
    });
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  it('lazy loads the home page at the root', async () => {
    const harness = await RouterTestingHarness.create();
    const component = await harness.navigateByUrl('/', Home);
    http.expectOne('/healthz').flush({ status: 'ok' });

    expect(component).toBeInstanceOf(Home);
    expect(TestBed.inject(Location).path()).toBe('');
  });

  it('redirects unknown paths to the root', async () => {
    const harness = await RouterTestingHarness.create();
    const component = await harness.navigateByUrl('/does/not/exist', Home);
    http.expectOne('/healthz').flush({ status: 'ok' });

    expect(component).toBeInstanceOf(Home);
    expect(TestBed.inject(Location).path()).toBe('');
  });
});
