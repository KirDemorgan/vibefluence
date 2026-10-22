import { TestBed } from '@angular/core/testing';
import { provideApp } from '../../../testing';
import { I18n } from '../../core/i18n';
import { NotFoundPage } from './not-found-page';

describe('NotFoundPage', () => {
  beforeEach(() => {
    localStorage.clear();
    TestBed.configureTestingModule({ imports: [NotFoundPage], providers: provideApp() });
    TestBed.inject(I18n).setLang('en');
  });

  it('explains the problem and links home', () => {
    const fixture = TestBed.createComponent(NotFoundPage);
    fixture.detectChanges();
    TestBed.tick();
    const element = fixture.nativeElement as HTMLElement;
    expect(element.querySelector('h1')?.textContent).toBe('Page not found');
    expect(element.querySelector('a')?.getAttribute('href')).toBe('/');
    expect(document.title).toBe('Page not found · Vibefluence');
  });
});
