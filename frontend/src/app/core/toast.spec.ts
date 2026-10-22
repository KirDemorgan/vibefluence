import { TestBed } from '@angular/core/testing';
import { TuiNotificationService } from '@taiga-ui/core';
import { of } from 'rxjs';
import { Toasts } from './toast';

describe('Toasts', () => {
  const open = vi.fn(() => of(undefined));

  beforeEach(() => {
    open.mockClear();
    TestBed.configureTestingModule({
      providers: [{ provide: TuiNotificationService, useValue: { open } }],
    });
  });

  it('shows a positive notification for successes', () => {
    TestBed.inject(Toasts).success('Saved');
    expect(open).toHaveBeenCalledWith('Saved', { appearance: 'positive' });
  });

  it('shows a negative notification for errors', () => {
    TestBed.inject(Toasts).error('Failed');
    expect(open).toHaveBeenCalledWith('Failed', { appearance: 'negative' });
  });
});
