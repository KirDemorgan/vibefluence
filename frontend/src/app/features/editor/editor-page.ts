import {
  Component,
  ElementRef,
  computed,
  effect,
  inject,
  input,
  signal,
  untracked,
  viewChild,
} from '@angular/core';
import { rxResource, takeUntilDestroyed, toObservable } from '@angular/core/rxjs-interop';
import { DomSanitizer, SafeHtml } from '@angular/platform-browser';
import { Router, RouterLink } from '@angular/router';
import { TuiButton, TuiIcon } from '@taiga-ui/core';
import {
  EMPTY,
  Observable,
  catchError,
  combineLatest,
  debounceTime,
  filter,
  map,
  of,
  switchMap,
  tap,
  throwError,
} from 'rxjs';
import { Api, ApiError } from '../../core/api';
import { Author } from '../../core/author';
import { Drafts } from '../../core/drafts';
import {
  bodyOf,
  isPristineBody,
  isValidPath,
  readMeta,
  setMeta,
  suggestPath,
} from '../../core/frontmatter';
import { I18n, MessageKey } from '../../core/i18n';
import { docLink, repoLink } from '../../core/links';
import {
  Draft,
  DocumentResponse,
  PreviewResponse,
  PublishReason,
  PublishResult,
} from '../../core/models';
import { usePageTitle } from '../../core/page-title';
import { Repos } from '../../core/repos';
import { DOC_TYPES, bodyTemplate } from '../../core/templates';
import { Trees } from '../../core/trees';

type Loaded =
  | { kind: 'new' }
  | { kind: 'blank'; path: string }
  | { kind: 'doc'; doc: DocumentResponse }
  | { kind: 'draft'; draft: Draft };

interface EditorRequest {
  repo: string;
  path: string | undefined;
}

interface Conflict {
  exists: boolean;
  currentBlobId: string;
}

type SaveState = 'idle' | 'saving' | 'saved' | 'error';
type View = 'write' | 'preview';

const AUTOSAVE_MS = 1500;
const PREVIEW_MS = 400;

const REASON_KEYS: Record<PublishReason, MessageKey> = {
  auto_merge_disabled: 'editor.reason.auto_merge_disabled',
  pipeline_started: 'editor.reason.pipeline_started',
  pipeline_unknown: 'editor.reason.pipeline_unknown',
  not_mergeable: 'editor.reason.not_mergeable',
  forbidden: 'editor.reason.forbidden',
  refused: 'editor.reason.refused',
};

@Component({
  selector: 'vf-editor-page',
  imports: [RouterLink, TuiIcon, TuiButton],
  templateUrl: './editor-page.html',
  styleUrl: './editor-page.scss',
  host: {
    '(click)': 'onClick($event)',
    '(document:keydown)': 'onKeydown($event)',
    '(window:beforeunload)': 'onBeforeUnload($event)',
  },
})
export class EditorPage {
  protected readonly i18n = inject(I18n);
  protected readonly authorName = inject(Author);
  private readonly api = inject(Api);
  private readonly repos = inject(Repos);
  private readonly trees = inject(Trees);
  private readonly drafts = inject(Drafts);
  private readonly router = inject(Router);
  private readonly sanitizer = inject(DomSanitizer);

  readonly repo = input.required<string>();
  readonly path = input<string | undefined>(undefined);

  protected readonly types = DOC_TYPES;

  protected readonly resource = rxResource<Loaded, EditorRequest>({
    params: () => ({ repo: this.repo(), path: this.path() }),
    stream: ({ params }) => this.load(params.repo, params.path),
  });

  protected readonly ready = signal(false);
  protected readonly content = signal('');
  protected readonly newPath = signal('');
  protected readonly originalPath = signal('');
  protected readonly isNew = signal(true);
  protected readonly baseBlobId = signal('');
  protected readonly draftId = signal<string | null>(null);
  protected readonly mergeRequestUrl = signal<string | null>(null);
  protected readonly savedSignature = signal('');
  protected readonly saveState = signal<SaveState>('idle');
  protected readonly lastSaved = signal<Date | null>(null);
  protected readonly view = signal<View>('write');
  protected readonly confirming = signal(false);
  protected readonly publishing = signal(false);
  protected readonly result = signal<PublishResult | null>(null);
  protected readonly conflict = signal<Conflict | null>(null);
  protected readonly problem = signal<string | null>(null);
  protected readonly preview = signal<PreviewResponse | null>(null);
  private readonly pathTouched = signal(false);

  protected readonly repoInfo = computed(() => this.repos.byId(this.repo()));
  protected readonly roots = computed(() => this.repoInfo()?.paths ?? ['docs']);
  protected readonly branch = computed(() => this.repoInfo()?.branch || 'default');
  protected readonly meta = computed(() => readMeta(this.content()));
  protected readonly status = computed<'draft' | 'published'>(() =>
    this.meta().status.toLowerCase() === 'draft' ? 'draft' : 'published',
  );
  protected readonly currentPath = computed(() =>
    this.isNew() ? this.newPath() : this.originalPath(),
  );
  protected readonly validPath = computed(() => isValidPath(this.currentPath(), this.roots()));
  protected readonly signature = computed(() => `${this.currentPath()}\u0000${this.content()}`);
  protected readonly dirty = computed(
    () => this.ready() && this.signature() !== this.savedSignature(),
  );
  protected readonly empty = computed(() => isPristineBody(this.content(), this.meta().title));
  protected readonly canSave = computed(
    () => this.ready() && this.validPath() && this.saveState() !== 'saving' && !this.publishing(),
  );
  protected readonly canPublish = computed(() => this.canSave() && !this.empty());
  protected readonly autoMerge = computed(() => this.repoInfo()?.autoMerge ?? true);
  protected readonly previewHtml = computed<SafeHtml>(() =>
    this.sanitizer.bypassSecurityTrustHtml(this.preview()?.html ?? ''),
  );
  protected readonly problems = computed(() => this.preview()?.warnings ?? []);
  protected readonly loading = computed(() => this.resource.isLoading() && !this.ready());
  protected readonly loadFailed = computed(() => !this.ready() && !!this.resource.error());
  protected readonly editing = computed(() => this.draftId() !== null && !this.isNew());
  protected readonly stateLabel = computed(() => {
    switch (this.saveState()) {
      case 'saving':
        return this.i18n.t('editor.saving');
      case 'error':
        return this.i18n.t('editor.saveFailed');
      case 'saved':
        if (!this.dirty() && this.lastSaved()) {
          return this.i18n.t('editor.saved', { time: this.clock(this.lastSaved() as Date) });
        }
        return this.i18n.t('editor.unsaved');
      default:
        return this.dirty() ? this.i18n.t('editor.unsaved') : '';
    }
  });
  protected readonly resultReason = computed(() => {
    const result = this.result();
    if (!result?.reason) {
      return '';
    }
    return this.i18n.t(REASON_KEYS[result.reason], { detail: result.detail ?? '' });
  });
  protected readonly documentLink = computed(() =>
    docLink(this.repo(), this.result()?.path ?? this.currentPath()),
  );
  protected readonly titleModel = computed(() => this.meta().title);
  protected readonly typeModel = computed(() => this.meta().type);
  protected readonly pathHint = computed(() => this.roots().join(', '));

  private readonly dialog = viewChild.required<ElementRef<HTMLDialogElement>>('dialog');

  constructor() {
    usePageTitle(() => [
      this.meta().title || this.i18n.t(this.isNew() ? 'editor.newTitle' : 'editor.editTitle'),
    ]);

    effect(() => {
      const loaded = this.resource.hasValue() ? this.resource.value() : undefined;
      if (loaded) {
        untracked(() => this.initialise(loaded));
      }
    });

    effect(() => {
      const dialog = this.dialog().nativeElement;
      if (this.confirming() && !dialog.open) {
        dialog.showModal();
      } else if (!this.confirming() && dialog.open) {
        dialog.close();
      }
    });

    combineLatest([toObservable(this.content), toObservable(this.currentPath)])
      .pipe(
        filter(() => this.dirty() && this.validPath() && this.result()?.outcome !== 'merged'),
        debounceTime(AUTOSAVE_MS),
        filter(() => this.dirty() && this.validPath() && !this.publishing()),
        switchMap(() => this.persist().pipe(catchError(() => EMPTY))),
        takeUntilDestroyed(),
      )
      .subscribe();

    combineLatest([toObservable(this.content), toObservable(this.currentPath)])
      .pipe(
        filter(() => this.ready()),
        debounceTime(PREVIEW_MS),
        switchMap(([content, path]) =>
          this.api.preview(this.repo(), { path, content }).pipe(catchError(() => of(null))),
        ),
        takeUntilDestroyed(),
      )
      .subscribe((preview) => {
        if (preview) {
          this.preview.set(preview);
        }
      });
  }

  private load(repo: string, path: string | undefined): Observable<Loaded> {
    if (!path) {
      return of<Loaded>({ kind: 'new' });
    }
    return this.api.drafts(repo).pipe(
      switchMap((list) => {
        const found = list.find((draft) => draft.path === path);
        if (found) {
          return this.api
            .draft(repo, found.id)
            .pipe(map((draft): Loaded => ({ kind: 'draft', draft })));
        }
        return this.api.document(repo, path).pipe(
          map((doc): Loaded => ({ kind: 'doc', doc })),
          catchError((error: unknown) =>
            error instanceof ApiError && error.isNotFound
              ? of<Loaded>({ kind: 'blank', path })
              : throwError(() => error),
          ),
        );
      }),
    );
  }

  private initialise(loaded: Loaded): void {
    this.result.set(null);
    this.conflict.set(null);
    this.problem.set(null);
    this.confirming.set(false);
    this.saveState.set('idle');
    this.lastSaved.set(null);
    this.preview.set(null);
    this.draftId.set(null);
    this.mergeRequestUrl.set(null);
    this.pathTouched.set(false);

    switch (loaded.kind) {
      case 'new':
        this.start('', true, '');
        break;
      case 'blank':
        this.start(loaded.path, true, loaded.path);
        this.pathTouched.set(true);
        break;
      case 'doc':
        this.content.set(loaded.doc.raw);
        this.originalPath.set(loaded.doc.path);
        this.newPath.set(loaded.doc.path);
        this.isNew.set(false);
        this.baseBlobId.set(loaded.doc.blobId);
        break;
      case 'draft':
        this.content.set(loaded.draft.content);
        this.originalPath.set(loaded.draft.path);
        this.newPath.set(loaded.draft.path);
        this.isNew.set(loaded.draft.isNew);
        this.baseBlobId.set(loaded.draft.baseBlobId);
        this.draftId.set(loaded.draft.id);
        this.mergeRequestUrl.set(loaded.draft.mergeRequestUrl ?? null);
        this.pathTouched.set(true);
        this.saveState.set('saved');
        this.lastSaved.set(new Date(loaded.draft.updatedAt));
        break;
    }
    this.savedSignature.set(this.signature());
    this.ready.set(true);
  }

  private start(path: string, isNew: boolean, original: string): void {
    this.content.set(setMeta('', 'status', 'draft') + '# \n');
    this.newPath.set(path);
    this.originalPath.set(original);
    this.isNew.set(isNew);
    this.baseBlobId.set('');
  }

  protected onSource(event: Event): void {
    this.content.set((event.target as HTMLTextAreaElement).value);
  }

  protected onTitle(event: Event): void {
    const title = (event.target as HTMLInputElement).value;
    const pristine = isPristineBody(this.content(), this.meta().title);
    let next = setMeta(this.content(), 'title', title);
    if (pristine) {
      next = this.withBody(next, title ? `# ${title}\n` : '# \n');
    }
    this.content.set(next);
    if (this.isNew() && !this.pathTouched()) {
      this.newPath.set(title.trim() ? suggestPath(this.roots()[0], title) : '');
    }
  }

  private withBody(raw: string, body: string): string {
    const head = raw.slice(0, raw.length - bodyOf(raw).length);
    if (head === '') {
      return body;
    }
    return `${head}${head.endsWith('\n') ? '' : '\n'}\n${body}`;
  }

  protected onType(event: Event): void {
    const type = (event.target as HTMLSelectElement).value;
    const title = this.meta().title;
    const pristine = isPristineBody(this.content(), title);
    let next = setMeta(this.content(), 'type', type);
    if (type && pristine) {
      const heading = title ? `# ${title}\n` : '# \n';
      next = this.withBody(next, `${heading}\n${bodyTemplate(type, this.i18n.lang())}`);
    }
    this.content.set(next);
  }

  protected setStatus(status: 'draft' | 'published'): void {
    this.content.set(setMeta(this.content(), 'status', status));
  }

  protected onPath(event: Event): void {
    this.pathTouched.set(true);
    this.newPath.set((event.target as HTMLInputElement).value.trim());
  }

  protected setView(view: View): void {
    this.view.set(view);
  }

  protected save(): void {
    if (!this.canSave()) {
      return;
    }
    this.persist()
      .pipe(catchError(() => EMPTY))
      .subscribe();
  }

  private persist(): Observable<Draft> {
    const previousId = this.draftId();
    this.saveState.set('saving');
    return this.api
      .saveDraft(this.repo(), {
        path: this.currentPath(),
        content: this.content(),
        baseBlobId: this.baseBlobId(),
        author: this.authorName.name(),
      })
      .pipe(
        tap((draft) => {
          this.draftId.set(draft.id);
          this.mergeRequestUrl.set(draft.mergeRequestUrl ?? null);
          this.savedSignature.set(`${draft.path}\u0000${draft.content}`);
          this.lastSaved.set(new Date());
          this.saveState.set('saved');
          if (previousId !== draft.id) {
            this.drafts.reload();
          }
        }),
        switchMap((draft) => {
          if (previousId && previousId !== draft.id) {
            return this.api.deleteDraft(this.repo(), previousId).pipe(
              catchError(() => of(undefined)),
              map(() => draft),
            );
          }
          return of(draft);
        }),
        catchError((error: unknown) => {
          this.saveState.set('error');
          return throwError(() => error);
        }),
      );
  }

  protected openPublish(): void {
    if (this.canPublish()) {
      this.result.set(null);
      this.conflict.set(null);
      this.problem.set(null);
      this.confirming.set(true);
    }
  }

  protected cancelPublish(): void {
    this.confirming.set(false);
  }

  protected onAuthorInput(event: Event): void {
    this.authorName.set((event.target as HTMLInputElement).value);
  }

  protected publish(force = false): void {
    if (!this.canPublish() && !force) {
      return;
    }
    this.publishing.set(true);
    this.conflict.set(null);
    this.problem.set(null);
    this.persist()
      .pipe(
        switchMap((draft) =>
          this.api.publish(this.repo(), draft.id, { author: this.authorName.name(), force }),
        ),
      )
      .subscribe({
        next: (result) => this.published(result),
        error: (error: unknown) => this.publishFailed(error),
      });
  }

  private published(result: PublishResult): void {
    this.publishing.set(false);
    this.confirming.set(false);
    this.result.set(result);
    if (result.draftRemoved) {
      this.draftId.set(null);
      this.savedSignature.set(this.signature());
    }
    this.drafts.reload();
    this.repos.reload();
    this.trees.reload();
  }

  private publishFailed(error: unknown): void {
    this.publishing.set(false);
    this.confirming.set(false);
    if (error instanceof ApiError && error.isConflict) {
      this.conflict.set({
        exists: error.details['exists'] === true,
        currentBlobId:
          typeof error.details['currentBlobId'] === 'string' ? error.details['currentBlobId'] : '',
      });
      return;
    }
    if (error instanceof ApiError && error.isBusy) {
      this.problem.set(this.i18n.t('editor.busy'));
      return;
    }
    this.problem.set(
      error instanceof ApiError && error.status === 400
        ? error.message
        : this.i18n.t('editor.saveFailed'),
    );
  }

  protected overwrite(): void {
    this.conflict.set(null);
    this.publish(true);
  }

  protected keepEditing(): void {
    this.conflict.set(null);
  }

  protected discard(): void {
    const id = this.draftId();
    const target = this.isNew() ? repoLink(this.repo()) : docLink(this.repo(), this.originalPath());
    this.conflict.set(null);
    const leave = () => {
      this.savedSignature.set(this.signature());
      this.drafts.reload();
      void this.router.navigate(target);
    };
    if (!id) {
      leave();
      return;
    }
    this.api
      .deleteDraft(this.repo(), id)
      .pipe(catchError(() => of(undefined)))
      .subscribe(leave);
  }

  canLeave(): boolean {
    if (!this.dirty() || this.result()?.outcome === 'merged') {
      return true;
    }
    return window.confirm(this.i18n.t('editor.leave'));
  }

  protected onBeforeUnload(event: Event): void {
    if (this.dirty() && this.result()?.outcome !== 'merged') {
      event.preventDefault();
    }
  }

  protected onKeydown(event: KeyboardEvent): void {
    if (!(event.metaKey || event.ctrlKey)) {
      return;
    }
    if (event.key.toLowerCase() === 's') {
      event.preventDefault();
      this.save();
    } else if (event.key === 'Enter') {
      event.preventDefault();
      this.openPublish();
    }
  }

  protected onClick(event: MouseEvent): void {
    const target = event.target as HTMLElement;
    if (target.closest('.preview') && target.closest('a')) {
      event.preventDefault();
    }
  }

  protected typeLabel(type: string): string {
    return this.i18n.t(`type.${type}` as MessageKey);
  }

  protected backLink(): string[] {
    return repoLink(this.repo());
  }

  private clock(date: Date): string {
    return date.toLocaleTimeString(this.i18n.lang(), { hour: '2-digit', minute: '2-digit' });
  }
}
