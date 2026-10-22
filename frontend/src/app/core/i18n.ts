import { Injectable, effect, signal } from '@angular/core';

export type Lang = 'ru' | 'en';

const STORAGE_KEY = 'vf.lang';

const en = {
  'app.name': 'Vibefluence',
  'app.tagline': 'Documentation that lives in your repositories',
  'nav.home': 'Home',
  'nav.repositories': 'Repositories',
  'nav.chooseRepo': 'Choose a repository',
  'nav.filter': 'Filter documents',
  'nav.noDocs': 'No documents here yet',
  'nav.noMatches': 'No documents match',
  'nav.open': 'Open navigation',
  'nav.close': 'Close navigation',
  'nav.documents': 'Documents',
  'nav.skip': 'Skip to content',
  'search.open': 'Search',
  'search.placeholder': 'Search documents…',
  'search.hint': 'Type to search titles and content',
  'search.empty': 'Nothing found for “{query}”',
  'search.error': 'Search is unavailable right now',
  'search.scopeAll': 'All repositories',
  'search.scopeRepo': 'This repository',
  'search.close': 'Close search',
  'search.results': 'Search results',
  'theme.label': 'Appearance',
  'theme.system': 'System',
  'theme.light': 'Light',
  'theme.dark': 'Dark',
  'lang.label': 'Language',
  'settings.open': 'Settings',
  'home.title': 'Your documentation',
  'home.subtitle': 'Markdown from your GitLab repositories, organised and searchable.',
  'home.empty.title': 'No repositories connected',
  'home.empty.text': 'Add a repository to the server configuration and it will show up here.',
  'home.unavailable': 'Unavailable',
  'home.stale': 'Showing saved data',
  'home.error.title': 'Cannot load repositories',
  'repo.branch': 'Branch',
  'repo.docs.one': '{n} document',
  'repo.docs.other': '{n} documents',
  'repo.drafts.one': '{n} draft hidden',
  'repo.drafts.other': '{n} drafts hidden',
  'repo.refresh': 'Refresh',
  'repo.refreshing': 'Refreshing…',
  'repo.refreshed': 'Repository refreshed',
  'repo.refreshFailed': 'Could not refresh the repository',
  'repo.openGitlab': 'Open in GitLab',
  'repo.empty.title': 'No documents yet',
  'repo.empty.text': 'There are no published Markdown files in the configured folders.',
  'repo.allDocs': 'All documents',
  'repo.unavailable.title': 'This repository is unavailable',
  'repo.unavailable.text': 'GitLab did not answer or the access token was rejected.',
  'doc.stale': 'GitLab is unreachable. You are reading a saved copy.',
  'doc.warnings': 'This document has problems',
  'doc.onThisPage': 'On this page',
  'doc.notFound.title': 'Document not found',
  'doc.notFound.text': 'It may have been moved, deleted or marked as a draft.',
  'doc.backToRepo': 'Back to the repository',
  'doc.error.title': 'Cannot load the document',
  'doc.retry': 'Try again',
  'doc.status.draft': 'Draft',
  'doc.status.published': 'Published',
  'doc.type': 'Type',
  'notFound.title': 'Page not found',
  'notFound.text': 'The page you are looking for does not exist.',
  'notFound.home': 'Go to the home page',
  'common.loading': 'Loading…',
  'common.close': 'Close',
  'common.cancel': 'Cancel',
  'common.retry': 'Try again',
  'nav.drafts': 'Drafts',
  'settings.name': 'Your name',
  'settings.namePlaceholder': 'Shown in commits',
  'repo.newDoc': 'New document',
  'doc.edit': 'Edit',
  'doc.draftExists': 'You have a draft of this document with unpublished changes.',
  'doc.continueEditing': 'Continue editing',
  'editor.newTitle': 'New document',
  'editor.editTitle': 'Edit document',
  'editor.title': 'Title',
  'editor.titlePlaceholder': 'Untitled document',
  'editor.type': 'Type',
  'editor.typeNone': 'No type',
  'editor.status': 'Status',
  'editor.statusHintDraft': 'Saved on this server only. Nothing is sent to GitLab.',
  'editor.statusHintPublished': 'Publishing creates a merge request into {branch}.',
  'editor.path': 'File path',
  'editor.pathHint': 'Inside: {roots}',
  'editor.pathInvalid': 'Use a .md path inside {roots}',
  'editor.pathFixed': 'The path of a published document cannot be changed here.',
  'editor.content': 'Markdown',
  'editor.write': 'Write',
  'editor.preview': 'Preview',
  'editor.save': 'Save draft',
  'editor.saving': 'Saving…',
  'editor.saved': 'Saved {time}',
  'editor.unsaved': 'Unsaved changes',
  'editor.saveFailed': 'Could not save the draft',
  'editor.publish': 'Publish',
  'editor.publishing': 'Publishing…',
  'editor.publishTitle': 'Publish this document?',
  'editor.publishText': 'A merge request will be created in {project} and merged into {branch}.',
  'editor.publishTextManual':
    'A merge request will be created in {project}. You will merge it in GitLab.',
  'editor.yourName': 'Your name, added to the commit',
  'editor.result.merged': 'Published',
  'editor.result.mergedText': 'The document is now in {branch}.',
  'editor.result.pending': 'Merge request created',
  'editor.result.pendingText': 'It was not merged automatically.',
  'editor.result.viewDoc': 'Open the document',
  'editor.result.viewMr': 'Open the merge request',
  'editor.result.back': 'Back to the repository',
  'editor.reason.auto_merge_disabled': 'Automatic merging is turned off for this repository.',
  'editor.reason.pipeline_started':
    'A CI pipeline was started for this change. Check the pipeline rules of the repository.',
  'editor.reason.pipeline_unknown': 'Could not verify whether CI pipelines were started.',
  'editor.reason.not_mergeable': 'GitLab does not allow merging yet ({detail}).',
  'editor.reason.forbidden': 'The access token is not allowed to merge.',
  'editor.reason.refused': 'GitLab refused to merge. Finish it in GitLab.',
  'editor.conflict.title': 'The document changed in the repository',
  'editor.conflict.exists': 'Someone changed or created this file after you started editing.',
  'editor.conflict.removed': 'This file was removed from the repository after you started editing.',
  'editor.conflict.overwrite': 'Overwrite with my version',
  'editor.conflict.discard': 'Discard my changes',
  'editor.conflict.keep': 'Keep editing',
  'editor.busy': 'This document is being published right now. Try again in a moment.',
  'editor.problems': 'Problems in the document header',
  'editor.loadFailed': 'Cannot open the editor',
  'editor.draftNotice': 'You are editing a saved draft. The published version is unchanged.',
  'editor.leave': 'You have unsaved changes. Leave anyway?',
  'editor.shortcuts': '⌘S saves · ⌘↵ publishes',
  'editor.mrNote': 'A merge request already exists for this draft.',
  'editor.nothingToPublish': 'Nothing to publish yet.',
  'drafts.title': 'Drafts',
  'drafts.subtitle':
    'Working copies stored on this server. They reach GitLab only when you publish.',
  'drafts.emptyTitle': 'No drafts',
  'drafts.emptyText':
    'Start a new document or edit an existing one. Your work is saved here automatically.',
  'drafts.edit': 'Edit',
  'drafts.delete': 'Delete',
  'drafts.deleteConfirm': 'Delete this draft? This cannot be undone.',
  'drafts.deleted': 'Draft deleted',
  'drafts.deleteFailed': 'Could not delete the draft',
  'drafts.updated': 'Updated {time}',
  'drafts.mergeRequest': 'Merge request',
  'drafts.willPublish': 'Will be published',
  'drafts.newBadge': 'New',
  'drafts.error': 'Cannot load drafts',
  'type.spec': 'Spec',
  'type.adr': 'ADR',
  'type.ticket': 'Ticket',
  'type.plan': 'Plan',
  'type.guide': 'Guide',
} as const;

export type MessageKey = keyof typeof en;

const ru: Record<string, string> = {
  'app.name': 'Vibefluence',
  'app.tagline': 'Документация, которая живёт в ваших репозиториях',
  'nav.home': 'Главная',
  'nav.repositories': 'Репозитории',
  'nav.chooseRepo': 'Выберите репозиторий',
  'nav.filter': 'Фильтр документов',
  'nav.noDocs': 'Документов пока нет',
  'nav.noMatches': 'Ничего не найдено',
  'nav.open': 'Открыть навигацию',
  'nav.close': 'Закрыть навигацию',
  'nav.documents': 'Документы',
  'nav.skip': 'Перейти к содержимому',
  'search.open': 'Поиск',
  'search.placeholder': 'Поиск по документам…',
  'search.hint': 'Введите запрос: ищем по заголовкам и тексту',
  'search.empty': 'По запросу «{query}» ничего не найдено',
  'search.error': 'Поиск сейчас недоступен',
  'search.scopeAll': 'Все репозитории',
  'search.scopeRepo': 'Этот репозиторий',
  'search.close': 'Закрыть поиск',
  'search.results': 'Результаты поиска',
  'theme.label': 'Оформление',
  'theme.system': 'Системное',
  'theme.light': 'Светлое',
  'theme.dark': 'Тёмное',
  'lang.label': 'Язык',
  'settings.open': 'Настройки',
  'home.title': 'Ваша документация',
  'home.subtitle': 'Markdown из ваших репозиториев GitLab: структурировано и с поиском.',
  'home.empty.title': 'Репозитории не подключены',
  'home.empty.text': 'Добавьте репозиторий в конфигурацию сервера, и он появится здесь.',
  'home.unavailable': 'Недоступен',
  'home.stale': 'Показаны сохранённые данные',
  'home.error.title': 'Не удалось загрузить репозитории',
  'repo.branch': 'Ветка',
  'repo.docs.one': '{n} документ',
  'repo.docs.few': '{n} документа',
  'repo.docs.many': '{n} документов',
  'repo.docs.other': '{n} документа',
  'repo.drafts.one': '{n} черновик скрыт',
  'repo.drafts.few': '{n} черновика скрыто',
  'repo.drafts.many': '{n} черновиков скрыто',
  'repo.drafts.other': '{n} черновика скрыто',
  'repo.refresh': 'Обновить',
  'repo.refreshing': 'Обновляем…',
  'repo.refreshed': 'Репозиторий обновлён',
  'repo.refreshFailed': 'Не удалось обновить репозиторий',
  'repo.openGitlab': 'Открыть в GitLab',
  'repo.empty.title': 'Документов пока нет',
  'repo.empty.text': 'В настроенных папках нет опубликованных Markdown-файлов.',
  'repo.allDocs': 'Все документы',
  'repo.unavailable.title': 'Репозиторий недоступен',
  'repo.unavailable.text': 'GitLab не ответил или токен доступа отклонён.',
  'doc.stale': 'GitLab недоступен. Вы читаете сохранённую копию.',
  'doc.warnings': 'В документе есть проблемы',
  'doc.onThisPage': 'На этой странице',
  'doc.notFound.title': 'Документ не найден',
  'doc.notFound.text': 'Возможно, его переместили, удалили или перевели в черновик.',
  'doc.backToRepo': 'Назад к репозиторию',
  'doc.error.title': 'Не удалось загрузить документ',
  'doc.retry': 'Повторить',
  'doc.status.draft': 'Черновик',
  'doc.status.published': 'Опубликован',
  'doc.type': 'Тип',
  'notFound.title': 'Страница не найдена',
  'notFound.text': 'Такой страницы не существует.',
  'notFound.home': 'На главную',
  'common.loading': 'Загрузка…',
  'common.close': 'Закрыть',
  'common.cancel': 'Отмена',
  'common.retry': 'Повторить',
  'nav.drafts': 'Черновики',
  'settings.name': 'Ваше имя',
  'settings.namePlaceholder': 'Попадёт в коммиты',
  'repo.newDoc': 'Новый документ',
  'doc.edit': 'Править',
  'doc.draftExists': 'У вас есть черновик этого документа с неопубликованными правками.',
  'doc.continueEditing': 'Продолжить правку',
  'editor.newTitle': 'Новый документ',
  'editor.editTitle': 'Правка документа',
  'editor.title': 'Заголовок',
  'editor.titlePlaceholder': 'Документ без названия',
  'editor.type': 'Тип',
  'editor.typeNone': 'Без типа',
  'editor.status': 'Статус',
  'editor.statusHintDraft': 'Хранится только на этом сервере. В GitLab ничего не уходит.',
  'editor.statusHintPublished': 'Публикация создаст merge request в ветку {branch}.',
  'editor.path': 'Путь к файлу',
  'editor.pathHint': 'Внутри: {roots}',
  'editor.pathInvalid': 'Нужен путь .md внутри: {roots}',
  'editor.pathFixed': 'Путь опубликованного документа здесь изменить нельзя.',
  'editor.content': 'Markdown',
  'editor.write': 'Текст',
  'editor.preview': 'Просмотр',
  'editor.save': 'Сохранить черновик',
  'editor.saving': 'Сохраняем…',
  'editor.saved': 'Сохранено {time}',
  'editor.unsaved': 'Есть несохранённые изменения',
  'editor.saveFailed': 'Не удалось сохранить черновик',
  'editor.publish': 'Опубликовать',
  'editor.publishing': 'Публикуем…',
  'editor.publishTitle': 'Опубликовать документ?',
  'editor.publishText': 'В {project} будет создан merge request и влит в ветку {branch}.',
  'editor.publishTextManual':
    'В {project} будет создан merge request. Влить его нужно будет в GitLab.',
  'editor.yourName': 'Ваше имя, попадёт в коммит',
  'editor.result.merged': 'Опубликовано',
  'editor.result.mergedText': 'Документ теперь в ветке {branch}.',
  'editor.result.pending': 'Merge request создан',
  'editor.result.pendingText': 'Автоматически он не влит.',
  'editor.result.viewDoc': 'Открыть документ',
  'editor.result.viewMr': 'Открыть merge request',
  'editor.result.back': 'Назад к репозиторию',
  'editor.reason.auto_merge_disabled': 'Автоматическое слияние выключено для этого репозитория.',
  'editor.reason.pipeline_started':
    'Для изменения запущен CI-пайплайн. Проверьте правила пайплайнов в репозитории.',
  'editor.reason.pipeline_unknown': 'Не удалось проверить, запускались ли CI-пайплайны.',
  'editor.reason.not_mergeable': 'GitLab пока не разрешает слияние ({detail}).',
  'editor.reason.forbidden': 'Токену доступа запрещено вливать изменения.',
  'editor.reason.refused': 'GitLab отказался вливать. Завершите слияние в GitLab.',
  'editor.conflict.title': 'Документ изменился в репозитории',
  'editor.conflict.exists': 'Кто-то изменил или создал этот файл после того, как вы начали правку.',
  'editor.conflict.removed': 'Этот файл удалили из репозитория после того, как вы начали правку.',
  'editor.conflict.overwrite': 'Перезаписать моей версией',
  'editor.conflict.discard': 'Отказаться от моих правок',
  'editor.conflict.keep': 'Продолжить правку',
  'editor.busy': 'Этот документ публикуется прямо сейчас. Повторите через минуту.',
  'editor.problems': 'Проблемы в заголовке документа',
  'editor.loadFailed': 'Не удалось открыть редактор',
  'editor.draftNotice': 'Вы правите сохранённый черновик. Опубликованная версия не изменена.',
  'editor.leave': 'Есть несохранённые изменения. Всё равно уйти?',
  'editor.shortcuts': '⌘S сохраняет · ⌘↵ публикует',
  'editor.mrNote': 'Для этого черновика уже создан merge request.',
  'editor.nothingToPublish': 'Пока нечего публиковать.',
  'drafts.title': 'Черновики',
  'drafts.subtitle':
    'Рабочие копии на этом сервере. В GitLab они попадают только после публикации.',
  'drafts.emptyTitle': 'Черновиков нет',
  'drafts.emptyText':
    'Создайте документ или отредактируйте существующий: работа сохраняется здесь автоматически.',
  'drafts.edit': 'Править',
  'drafts.delete': 'Удалить',
  'drafts.deleteConfirm': 'Удалить черновик? Это нельзя отменить.',
  'drafts.deleted': 'Черновик удалён',
  'drafts.deleteFailed': 'Не удалось удалить черновик',
  'drafts.updated': 'Обновлён {time}',
  'drafts.mergeRequest': 'Merge request',
  'drafts.willPublish': 'Будет опубликован',
  'drafts.newBadge': 'Новый',
  'drafts.error': 'Не удалось загрузить черновики',
  'type.spec': 'Спецификация',
  'type.adr': 'ADR',
  'type.ticket': 'Тикет',
  'type.plan': 'План',
  'type.guide': 'Руководство',
};

export const MESSAGES: Record<Lang, Record<string, string>> = { en, ru };

export type Params = Record<string, string | number>;

function detect(): Lang {
  try {
    const saved = localStorage.getItem(STORAGE_KEY);
    if (saved === 'ru' || saved === 'en') {
      return saved;
    }
  } catch {
    return fromNavigator();
  }
  return fromNavigator();
}

function fromNavigator(): Lang {
  const language = typeof navigator === 'undefined' ? 'en' : navigator.language;
  return language.toLowerCase().startsWith('ru') ? 'ru' : 'en';
}

@Injectable({ providedIn: 'root' })
export class I18n {
  readonly lang = signal<Lang>(detect());

  constructor() {
    effect(() => {
      document.documentElement.lang = this.lang();
    });
  }

  setLang(lang: Lang): void {
    this.lang.set(lang);
    try {
      localStorage.setItem(STORAGE_KEY, lang);
    } catch {
      return;
    }
  }

  t(key: MessageKey, params?: Params): string {
    const table = MESSAGES[this.lang()];
    return interpolate(table[key] ?? MESSAGES.en[key] ?? key, params);
  }

  plural(base: string, count: number): string {
    const table = MESSAGES[this.lang()];
    const category = new Intl.PluralRules(this.lang()).select(count);
    const template =
      table[`${base}.${category}`] ??
      table[`${base}.other`] ??
      MESSAGES.en[`${base}.other`] ??
      base;
    return interpolate(template, { n: count });
  }
}

function interpolate(template: string, params?: Params): string {
  if (!params) {
    return template;
  }
  return template.replace(/\{(\w+)\}/g, (match, name: string) =>
    name in params ? String(params[name]) : match,
  );
}
