import { Injectable, signal } from '@angular/core';
import { hasControlCharacter } from './frontmatter';

const STORAGE_KEY = 'vf.author';
const MAX_LENGTH = 80;

function stored(): string {
  try {
    return localStorage.getItem(STORAGE_KEY) ?? '';
  } catch {
    return '';
  }
}

function clean(value: string): string {
  const printable = [...value].filter((char) => !hasControlCharacter(char)).join('');
  return printable.trim().slice(0, MAX_LENGTH);
}

@Injectable({ providedIn: 'root' })
export class Author {
  readonly name = signal(clean(stored()));

  set(value: string): void {
    const cleaned = clean(value);
    this.name.set(cleaned);
    try {
      localStorage.setItem(STORAGE_KEY, cleaned);
    } catch {
      return;
    }
  }
}
