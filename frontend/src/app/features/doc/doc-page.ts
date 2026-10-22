import {
  Component,
  ElementRef,
  afterRenderEffect,
  computed,
  inject,
  input,
  viewChild,
} from '@angular/core';
import { rxResource } from '@angular/core/rxjs-interop';
import { DomSanitizer, SafeHtml } from '@angular/platform-browser';
import { Router, RouterLink } from '@angular/router';
import { TuiButton, TuiIcon } from '@taiga-ui/core';
import { signal } from '@angular/core';
import { Api, ApiError } from '../../core/api';
import { I18n } from '../../core/i18n';
import { repoLink } from '../../core/links';
import { DocumentResponse, Heading } from '../../core/models';
import { usePageTitle } from '../../core/page-title';
import { Repos } from '../../core/repos';
import { RouteState } from '../../core/route-state';

interface DocRequest {
  repo: string;
  path: string;
}

@Component({
  selector: 'vf-doc-page',
  imports: [RouterLink, TuiIcon, TuiButton],
  templateUrl: './doc-page.html',
  styleUrl: './doc-page.scss',
  host: {
    '(click)': 'onClick($event)',
  },
})
export class DocPage {
  protected readonly i18n = inject(I18n);
  private readonly api = inject(Api);
  private readonly repos = inject(Repos);
  private readonly router = inject(Router);
  private readonly route = inject(RouteState);
  private readonly sanitizer = inject(DomSanitizer);

  readonly repo = input.required<string>();
  readonly path = input.required<string>();

  protected readonly resource = rxResource<DocumentResponse, DocRequest>({
    params: () => ({ repo: this.repo(), path: this.path() }),
    stream: ({ params }) => this.api.document(params.repo, params.path),
  });

  protected readonly doc = computed(() =>
    this.resource.hasValue() ? this.resource.value() : undefined,
  );
  protected readonly html = computed<SafeHtml | null>(() => {
    const doc = this.doc();
    return doc ? this.sanitizer.bypassSecurityTrustHtml(doc.html) : null;
  });
  protected readonly showTitle = computed(() => {
    const doc = this.doc();
    return !!doc && !doc.headings.some((heading) => heading.level === 1);
  });
  protected readonly toc = computed<Heading[]>(() =>
    (this.doc()?.headings ?? []).filter((heading) => heading.level === 2 || heading.level === 3),
  );
  protected readonly error = computed(() => {
    const error = this.resource.error();
    return error instanceof ApiError
      ? error
      : error
        ? new ApiError(0, 'unknown', String(error))
        : undefined;
  });
  protected readonly notFound = computed(() => this.error()?.isNotFound === true);
  protected readonly loading = computed(() => this.resource.isLoading() && !this.doc());
  protected readonly repoName = computed(() => this.repos.byId(this.repo())?.name ?? this.repo());
  protected readonly activeHeading = signal<string | null>(null);
  protected readonly placeholders = [92, 78, 86, 64, 90, 72];

  private readonly prose = viewChild<ElementRef<HTMLElement>>('prose');

  constructor() {
    usePageTitle(() => [this.doc()?.title, this.repoName()]);

    afterRenderEffect((onCleanup) => {
      const container = this.prose()?.nativeElement;
      if (!container || !this.doc()) {
        return;
      }
      const fragment = this.route.fragment();
      if (fragment) {
        scrollToId(fragment);
      }
      const headings = Array.from(container.querySelectorAll<HTMLElement>('h2[id], h3[id]'));
      this.activeHeading.set(headings[0]?.id ?? null);
      const observer = new IntersectionObserver(
        (entries) => {
          const visible = entries.filter((entry) => entry.isIntersecting);
          if (visible.length > 0) {
            this.activeHeading.set(visible[0].target.id);
          }
        },
        { rootMargin: '-72px 0px -70% 0px' },
      );
      headings.forEach((heading) => observer.observe(heading));
      onCleanup(() => observer.disconnect());
    });
  }

  protected backLink(): string[] {
    return repoLink(this.repo());
  }

  protected goTo(id: string, event: Event): void {
    event.preventDefault();
    scrollToId(id);
    this.activeHeading.set(id);
  }

  protected retry(): void {
    this.resource.reload();
  }

  protected onClick(event: MouseEvent): void {
    if (
      event.defaultPrevented ||
      event.button !== 0 ||
      event.metaKey ||
      event.ctrlKey ||
      event.shiftKey ||
      event.altKey
    ) {
      return;
    }
    const target = event.target as HTMLElement;
    if (!target.closest('.vf-prose')) {
      return;
    }
    const anchor = target.closest('a');
    const href = anchor?.getAttribute('href');
    if (!anchor || !href) {
      return;
    }
    if (href.startsWith('#')) {
      event.preventDefault();
      scrollToId(href.slice(1));
      return;
    }
    if (href.startsWith('/r/')) {
      event.preventDefault();
      void this.router.navigateByUrl(href);
    }
  }
}

function scrollToId(id: string): void {
  const element = document.getElementById(id);
  if (!element) {
    return;
  }
  element.scrollIntoView({ block: 'start', behavior: 'auto' });
}
