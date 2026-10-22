import { effect, inject } from '@angular/core';
import { Title } from '@angular/platform-browser';

const SUFFIX = 'Vibefluence';

export function usePageTitle(parts: () => (string | null | undefined)[]): void {
  const title = inject(Title);
  effect(() => {
    const names = parts().filter((part): part is string => Boolean(part));
    title.setTitle([...names, SUFFIX].join(' · '));
  });
}
