import { Directive, ElementRef, inject, input, signal } from '@angular/core';

const GAP = 6;
const VIEWPORT_MARGIN = 12;
const MIN_HEIGHT = 200;

@Directive({
  selector: '[vfPopover]',
  exportAs: 'vfPopover',
  host: {
    popover: 'auto',
    '(toggle)': 'onToggle($event)',
  },
})
export class Popover {
  private readonly element = inject<ElementRef<HTMLElement>>(ElementRef).nativeElement;

  readonly align = input<'start' | 'end'>('start');
  readonly placement = input<'auto' | 'down' | 'up'>('auto');
  readonly matchWidth = input(false);

  readonly open = signal(false);

  toggle(anchor: HTMLElement): void {
    if (this.open()) {
      this.close();
      return;
    }
    this.place(anchor);
    this.element.showPopover();
  }

  close(): void {
    if (this.open()) {
      this.element.hidePopover();
    }
  }

  protected onToggle(event: Event): void {
    this.open.set((event as ToggleEvent).newState === 'open');
  }

  private place(anchor: HTMLElement): void {
    const rect = anchor.getBoundingClientRect();
    const style = this.element.style;
    style.position = 'fixed';
    style.inset = 'auto';
    style.margin = '0';
    const below = window.innerHeight - rect.bottom - GAP - VIEWPORT_MARGIN;
    const above = rect.top - GAP - VIEWPORT_MARGIN;
    const openUp =
      this.placement() === 'up' ||
      (this.placement() === 'auto' && below < MIN_HEIGHT && above > below);
    if (openUp) {
      style.top = 'auto';
      style.bottom = `${window.innerHeight - rect.top + GAP}px`;
      style.maxHeight = `${Math.max(MIN_HEIGHT, above)}px`;
    } else {
      style.bottom = 'auto';
      style.top = `${rect.bottom + GAP}px`;
      style.maxHeight = `${Math.max(MIN_HEIGHT, below)}px`;
    }
    if (this.matchWidth()) {
      style.minWidth = `${rect.width}px`;
    }
    if (this.align() === 'end') {
      style.left = 'auto';
      style.right = `${Math.max(VIEWPORT_MARGIN, window.innerWidth - rect.right)}px`;
    } else {
      style.right = 'auto';
      style.left = `${Math.max(VIEWPORT_MARGIN, rect.left)}px`;
    }
  }
}
