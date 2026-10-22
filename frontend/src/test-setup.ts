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
    Object.defineProperty(globalThis, name, { writable: true, value: ObserverStub });
  }
}
