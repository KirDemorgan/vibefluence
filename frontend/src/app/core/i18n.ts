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
