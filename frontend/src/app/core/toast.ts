import { Injectable, inject } from '@angular/core';
import { TuiNotificationService } from '@taiga-ui/core';

@Injectable({ providedIn: 'root' })
export class Toasts {
  private readonly notifications = inject(TuiNotificationService);

  success(message: string): void {
    this.notifications.open(message, { appearance: 'positive' }).subscribe();
  }

  error(message: string): void {
    this.notifications.open(message, { appearance: 'negative' }).subscribe();
  }
}
