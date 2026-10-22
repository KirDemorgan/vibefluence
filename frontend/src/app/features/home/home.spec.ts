import { provideHttpClient } from '@angular/common/http';
import { HttpTestingController, provideHttpClientTesting } from '@angular/common/http/testing';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { provideTaiga } from '@taiga-ui/core';
import { Home } from './home';

describe('Home', () => {
  let fixture: ComponentFixture<Home>;
  let http: HttpTestingController;

  beforeEach(() => {
    TestBed.configureTestingModule({
      imports: [Home],
      providers: [provideHttpClient(), provideHttpClientTesting(), provideTaiga()],
    });
    http = TestBed.inject(HttpTestingController);
  });

  afterEach(() => http.verify());

  const element = () => fixture.nativeElement as HTMLElement;
  const statusText = () => element().querySelector('[role="status"]')?.textContent?.trim();
  const statusState = () => element().querySelector('[role="status"]')?.getAttribute('data-state');

  async function settle(): Promise<void> {
    fixture.detectChanges();
    await fixture.whenStable();
  }

  async function open() {
    fixture = TestBed.createComponent(Home);
    const request = http.expectOne('/healthz');
    await settle();
    return request;
  }

  it('shows the product name as the main heading', async () => {
    const request = await open();
    request.flush({ status: 'ok' });

    expect(element().querySelector('h1')?.textContent).toContain('Vibefluence');
  });

  it('starts in the checking state', async () => {
    const request = await open();

    expect(statusState()).toBe('checking');
    expect(statusText()).toContain('Checking');
    request.flush({ status: 'ok' });
  });

  it('shows that the service is up', async () => {
    const request = await open();
    request.flush({ status: 'ok' });
    await settle();

    expect(statusState()).toBe('ok');
    expect(statusText()).toContain('Service is up');
  });

  it('shows that the service is unavailable on failure', async () => {
    const request = await open();
    request.flush('down', { status: 502, statusText: 'Bad Gateway' });
    await settle();

    expect(statusState()).toBe('unavailable');
    expect(statusText()).toContain('unavailable');
  });

  it('checks again when the button is pressed', async () => {
    const first = await open();
    first.flush({ status: 'unavailable' });
    await settle();

    element().querySelector<HTMLButtonElement>('button')?.click();
    const second = http.expectOne('/healthz');
    await settle();
    expect(statusState()).toBe('checking');

    second.flush({ status: 'ok' });
    await settle();
    expect(statusState()).toBe('ok');
  });
});
