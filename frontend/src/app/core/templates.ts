import { Lang } from './i18n';

export const DOC_TYPES = ['spec', 'adr', 'ticket', 'plan', 'guide'] as const;
export type DocType = (typeof DOC_TYPES)[number];

const templates: Record<Lang, Record<DocType, string>> = {
  en: {
    spec: '## Context\n\n\n## Requirements\n\n- \n\n## Open questions\n\n- \n',
    adr: '## Status\n\nProposed\n\n## Context\n\n\n## Decision\n\n\n## Consequences\n\n',
    ticket: '## Description\n\n\n## Acceptance criteria\n\n- [ ] \n',
    plan: '## Goal\n\n\n## Steps\n\n1. \n\n## Risks\n\n- \n',
    guide: '## Overview\n\n\n## Steps\n\n1. \n',
  },
  ru: {
    spec: '## Контекст\n\n\n## Требования\n\n- \n\n## Открытые вопросы\n\n- \n',
    adr: '## Статус\n\nПредложено\n\n## Контекст\n\n\n## Решение\n\n\n## Последствия\n\n',
    ticket: '## Описание\n\n\n## Критерии приёмки\n\n- [ ] \n',
    plan: '## Цель\n\n\n## Шаги\n\n1. \n\n## Риски\n\n- \n',
    guide: '## Обзор\n\n\n## Шаги\n\n1. \n',
  },
};

export function isDocType(value: string): value is DocType {
  return (DOC_TYPES as readonly string[]).includes(value);
}

export function bodyTemplate(type: string, lang: Lang): string {
  return isDocType(type) ? templates[lang][type] : '';
}
