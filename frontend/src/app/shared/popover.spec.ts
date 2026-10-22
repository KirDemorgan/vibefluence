import { Component, signal, viewChild } from '@angular/core';
import { ComponentFixture, TestBed } from '@angular/core/testing';
import { Popover } from './popover';

@Component({
  imports: [Popover],
  template: `
    <button #anchor type="button" (click)="popover.toggle(anchor)">open</button>
    <div
      vfPopover
      #popover="vfPopover"
      [align]="align()"
      [placement]="placement()"
      [matchWidth]="match()"
    >
      menu
    </div>
  `,
})
class Host {
  readonly align = signal<'start' | 'end'>('start');
  readonly placement = signal<'auto' | 'down' | 'up'>('auto');
  readonly match = signal(false);
  readonly popover = viewChild.required(Popover);
}

function rect(overrides: Partial<DOMRect>): DOMRect {
  return {
    left: 20,
    right: 120,
    top: 100,
    bottom: 130,
    width: 100,
    height: 30,
    x: 20,
    y: 100,
    toJSON: () => ({}),
    ...overrides,
  };
}

describe('Popover', () => {
  let fixture: ComponentFixture<Host>;
  let button: HTMLButtonElement;
  let panel: HTMLElement;

  beforeEach(() => {
    TestBed.configureTestingModule({ imports: [Host] });
    fixture = TestBed.createComponent(Host);
    fixture.detectChanges();
    button = fixture.nativeElement.querySelector('button');
    panel = fixture.nativeElement.querySelector('[vfPopover]');
    vi.spyOn(button, 'getBoundingClientRect').mockReturnValue(rect({}));
  });

  afterEach(() => vi.restoreAllMocks());

  it('declares itself as an automatic popover', () => {
    expect(panel.getAttribute('popover')).toBe('auto');
  });

  it('opens and closes through toggle', () => {
    button.click();
    expect(fixture.componentInstance.popover().open()).toBe(true);
    expect(panel.hasAttribute('data-popover-open')).toBe(true);

    button.click();
    expect(fixture.componentInstance.popover().open()).toBe(false);
    expect(panel.hasAttribute('data-popover-open')).toBe(false);
  });

  it('close() does nothing when already closed', () => {
    const spy = vi.spyOn(panel, 'hidePopover');
    fixture.componentInstance.popover().close();
    expect(spy).not.toHaveBeenCalled();
  });

  it('close() hides an open popover', () => {
    button.click();
    fixture.componentInstance.popover().close();
    expect(fixture.componentInstance.popover().open()).toBe(false);
  });

  it('places the panel below the anchor aligned to its start', () => {
    button.click();
    expect(panel.style.position).toBe('fixed');
    expect(panel.style.top).toBe('136px');
    expect(panel.style.left).toBe('20px');
    expect(panel.style.bottom).toBe('auto');
  });

  it('aligns to the end of the anchor', () => {
    fixture.componentInstance.align.set('end');
    fixture.detectChanges();
    button.click();
    expect(panel.style.left).toBe('auto');
    expect(panel.style.right).toBe(`${window.innerWidth - 120}px`);
  });

  it('keeps a margin from the viewport edge', () => {
    vi.spyOn(button, 'getBoundingClientRect').mockReturnValue(
      rect({ left: -50, right: window.innerWidth + 50 }),
    );
    button.click();
    expect(panel.style.left).toBe('12px');
  });

  it('opens upwards when requested', () => {
    fixture.componentInstance.placement.set('up');
    fixture.detectChanges();
    button.click();
    expect(panel.style.top).toBe('auto');
    expect(panel.style.bottom).toBe(`${window.innerHeight - 100 + 6}px`);
  });

  it('flips upwards automatically when there is no room below', () => {
    vi.spyOn(button, 'getBoundingClientRect').mockReturnValue(
      rect({ top: window.innerHeight - 40, bottom: window.innerHeight - 10 }),
    );
    button.click();
    expect(panel.style.top).toBe('auto');
    expect(panel.style.bottom).not.toBe('auto');
  });

  it('forces downwards when requested even without room', () => {
    fixture.componentInstance.placement.set('down');
    fixture.detectChanges();
    vi.spyOn(button, 'getBoundingClientRect').mockReturnValue(
      rect({ top: window.innerHeight - 40, bottom: window.innerHeight - 10 }),
    );
    button.click();
    expect(panel.style.bottom).toBe('auto');
  });

  it('matches the anchor width on request', () => {
    fixture.componentInstance.match.set(true);
    fixture.detectChanges();
    button.click();
    expect(panel.style.minWidth).toBe('100px');
  });
});
