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
  未声明来源: {
    en: 'Source not declared',
    'zh-TW': '未聲明來源',
    fr: 'Source non déclarée',
    ru: 'Источник не указан',
    ja: '提供元未申告',
    vi: 'Chưa khai báo nguồn',
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
  const label = sourceLabel || '未声明来源'
  const translated = localizedSourceLabel(label, language)
  return translated !== label ? name.replace(label, translated) : name
}
