class ObserverStub {
  observe(): void {
    return undefined;
  }
  unobserve(): void {
    return undefined;
  }
  disconnect(): void {
    return undefined;
  }
  takeRecords(): unknown[] {
    return [];
  }
}

function matchMediaStub(query: string): MediaQueryList {
  return {
    matches: false,
    media: query,
    onchange: null,
    addEventListener: () => undefined,
    removeEventListener: () => undefined,
    addListener: () => undefined,
    removeListener: () => undefined,
    dispatchEvent: () => false,
  };
}

Object.defineProperty(window, 'matchMedia', { writable: true, value: matchMediaStub });

for (const name of ['ResizeObserver', 'IntersectionObserver']) {
  if (!(name in globalThis)) {
    Object.defineProperty(globalThis, name, {
      writable: true,
      configurable: true,
      value: ObserverStub,
    });
  }
}

function dispatchToggle(element: HTMLElement, newState: 'open' | 'closed'): void {
  element.dispatchEvent(Object.assign(new Event('toggle'), { newState }));
}

HTMLElement.prototype.showPopover = function showPopover(this: HTMLElement): void {
  this.setAttribute('data-popover-open', '');
  dispatchToggle(this, 'open');
};

HTMLElement.prototype.hidePopover = function hidePopover(this: HTMLElement): void {
  this.removeAttribute('data-popover-open');
  dispatchToggle(this, 'closed');
};

HTMLDialogElement.prototype.showModal = function showModal(this: HTMLDialogElement): void {
  this.setAttribute('open', '');
};

HTMLDialogElement.prototype.close = function close(this: HTMLDialogElement): void {
  this.removeAttribute('open');
  this.dispatchEvent(new Event('close'));
};

Element.prototype.scrollIntoView = function scrollIntoView(): void {
  return undefined;
};
