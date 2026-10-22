import { TestBed } from '@angular/core/testing';
import { Author } from './author';

describe('Author', () => {
  beforeEach(() => localStorage.clear());
  afterEach(() => vi.restoreAllMocks());

  function create(): Author {
    TestBed.resetTestingModule();
    return TestBed.inject(Author);
  }

  it('starts empty', () => {
    expect(create().name()).toBe('');
  });

  it('stores a cleaned name', () => {
    const author = create();
    author.set('  Alex\nInjected\t ');
    expect(author.name()).toBe('AlexInjected');
    expect(localStorage.getItem('vf.author')).toBe('AlexInjected');
  });

  it('limits the length', () => {
    const author = create();
    author.set('я'.repeat(200));
    expect([...author.name()]).toHaveLength(80);
  });

  it('restores the stored name', () => {
    localStorage.setItem('vf.author', 'Sam');
    expect(create().name()).toBe('Sam');
  });

  it('works when storage is blocked', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('blocked');
    });
    const author = create();
    expect(author.name()).toBe('');
    expect(() => author.set('Alex')).not.toThrow();
    expect(author.name()).toBe('Alex');
  });
});
