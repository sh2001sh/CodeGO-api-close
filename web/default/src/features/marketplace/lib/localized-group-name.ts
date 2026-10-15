import { normalizeInterfaceLanguage } from '@/i18n/languages'

const sourceNames: Record<string, Record<string, string>> = {
  'Codex 混合号池': {
    en: 'Codex Mixed Pool',
    'zh-TW': 'Codex 混合號池',
    fr: 'Pool mixte Codex',
    ru: 'Смешанный пул Codex',
    ja: 'Codex 混合プール',
    vi: 'Nhóm hỗn hợp Codex',
  },
  CC其它: {
    en: 'CC Other',
    'zh-TW': 'CC 其他',
    fr: 'CC Autres',
    ru: 'CC Другие',
    ja: 'CC その他',
    vi: 'CC Khác',
  },
  国产模型: {
    en: 'Chinese Models',
    'zh-TW': '國產模型',
    fr: 'Modèles chinois',
    ru: 'Китайские модели',
    ja: '中国製モデル',
    vi: 'Mô hình Trung Quốc',
  },
  来源待审核: {
    en: 'Source pending review',
    'zh-TW': '來源待審核',
    fr: 'Source en attente',
    ru: 'Источник на проверке',
    ja: '審査待ち',
    vi: 'Nguồn đang xét duyệt',
  },
  官方: {
    en: 'Official',
    'zh-TW': '官方',
    fr: 'Officiel',
    ru: 'Официальный',
    ja: '公式',
    vi: 'Chính thức',
  },
  社区贡献: {
    en: 'Community',
    'zh-TW': '社群貢獻',
    fr: 'Communauté',
    ru: 'Сообщество',
    ja: 'コミュニティ',
    vi: 'Cộng đồng',
  },
  第三方市场: {
    en: 'Marketplace',
    'zh-TW': '第三方市場',
    fr: 'Marché tiers',
    ru: 'Сторонний рынок',
    ja: '外部マーケット',
    vi: 'Thị trường bên thứ ba',
  },
}

export function localizedSourceLabel(label: string, language: string): string {
  const locale = normalizeInterfaceLanguage(language)
  if (locale === 'zh') return label
  const translation = sourceNames[label]
  return translation ? (translation[locale] ?? translation.en) : label
}

export function localizedGroupName(
  name: string,
  sourceLabel: string,
  language: string
): string {
  const translated = localizedSourceLabel(sourceLabel, language)
  return sourceLabel && translated !== sourceLabel
    ? name.replace(sourceLabel, translated)
    : name
}
