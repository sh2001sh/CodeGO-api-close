/*
Copyright (C) 2023-2026 QuantumNous

This program is free software: you can redistribute it and/or modify
it under the terms of the GNU Affero General Public License as
published by the Free Software Foundation, either version 3 of the
License, or (at your option) any later version.

This program is distributed in the hope that it will be useful,
but WITHOUT ANY WARRANTY; without even the implied warranty of
MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE. See the
GNU Affero General Public License for more details.

You should have received a copy of the GNU Affero General Public License
along with this program. If not, see <https://www.gnu.org/licenses/>.

For commercial licensing, please contact support@quantumnous.com
*/
import dayjs from 'dayjs'
import i18n from '@/i18n/config'
import { normalizeInterfaceLanguage } from '@/i18n/languages'
import 'dayjs/locale/fr'
import 'dayjs/locale/ja'
import 'dayjs/locale/ru'
import 'dayjs/locale/vi'
import 'dayjs/locale/zh-cn'
import 'dayjs/locale/zh-tw'
import relativeTime from 'dayjs/plugin/relativeTime'

dayjs.extend(relativeTime)
const updateLocale = (language: string) => {
  const normalized = normalizeInterfaceLanguage(language)
  const locale =
    normalized === 'zh-TW'
      ? 'zh-tw'
      : normalized === 'zh'
        ? 'zh-cn'
        : normalized
  dayjs.locale(locale)
}
i18n.on('languageChanged', updateLocale)
updateLocale(i18n.language)

export default dayjs
