import { describe, expect, it } from 'vitest'
import english from './en'
import hk from './zh-HK.json'
import ja from './ja.json'
import ru from './ru.json'
import ko from './ko.json'
import fr from './fr.json'
import de from './de.json'
import ar from './ar.json'

describe('shipped language dictionaries', () => {
  const keys = Object.keys(english).sort()
  for (const [locale, dictionary] of Object.entries({ hk, ja, ru, ko, fr, de, ar })) {
    it(`${locale} covers every English source key with a nonempty translation`, () => {
      expect(Object.keys(dictionary).sort()).toEqual(keys)
      for (const value of Object.values(dictionary)) {
        expect(typeof value).toBe('string')
        expect(value.trim()).not.toBe('')
      }
      const localized: Record<string, string> = dictionary
      for (const key of keys) {
        const parameters = (key.match(/\{\w+\}/g) ?? []).sort()
        expect((localized[key].match(/\{\w+\}/g) ?? []).sort(), `${locale}: ${key}`).toEqual(
          parameters,
        )
      }
    })
  }
})
