import { Injectable, signal } from '@angular/core';

@Injectable({ providedIn: 'root' })
export class Ui {
  readonly navOpen = signal(false);
  readonly searchOpen = signal(false);

  openNav(): void {
    this.navOpen.set(true);
  }

  closeNav(): void {
    this.navOpen.set(false);
  }

  openSearch(): void {
    this.searchOpen.set(true);
  }

  closeSearch(): void {
    this.searchOpen.set(false);
  }
}
