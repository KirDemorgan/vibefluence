import { Component, DestroyRef, inject, signal } from '@angular/core';
import { takeUntilDestroyed } from '@angular/core/rxjs-interop';
import { TuiButton } from '@taiga-ui/core';
import { BackendStatus, HealthService } from '../../core/health.service';

type ConnectionState = BackendStatus | 'checking';

@Component({
  selector: 'app-home',
  imports: [TuiButton],
  templateUrl: './home.html',
  styleUrl: './home.scss',
})
export class Home {
  private readonly health = inject(HealthService);
  private readonly destroyRef = inject(DestroyRef);

  protected readonly state = signal<ConnectionState>('checking');

  constructor() {
    this.refresh();
  }

  protected refresh(): void {
    this.state.set('checking');
    this.health
      .check()
      .pipe(takeUntilDestroyed(this.destroyRef))
      .subscribe((status) => this.state.set(status));
  }
}
