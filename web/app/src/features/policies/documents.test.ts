import { describe, expect, it } from 'vitest'
import { languages } from '../../lib/locales'
import {
  getPolicyDocument,
  policyContentVersion,
  policyDocuments,
  policyInterface,
  policyLanguage,
} from './documents'

describe('policy language selection', () => {
  it('keeps both Chinese policy editions and the English edition', () => {
    expect(policyLanguage('zh-HK')).toBe('zh-HK')
    expect(policyLanguage('zh-CN')).toBe('zh-CN')
    expect(policyLanguage('en')).toBe('en')
  })

  it('explicitly identifies English fallback in every unsupported UI language', () => {
    for (const { code } of languages) {
      const selected = policyLanguage(code)
      if (selected !== code) {
        expect(selected).toBe('en')
        expect(policyInterface[code].fallback.length).toBeGreaterThan(30)
      } else {
        expect(policyInterface[code].fallback).toBe('')
      }
    }
  })
})

describe('public policy content', () => {
  it('provides the same navigable clauses in all available policy editions', () => {
    for (const document of policyDocuments) {
      expect(document.sections.length).toBeGreaterThanOrEqual(4)
      const ids = document.sections.map((section) => section.id)
      expect(new Set(ids).size).toBe(ids.length)
      for (const language of ['zh-HK', 'zh-CN', 'en'] as const) {
        expect(document.title[language].length).toBeGreaterThan(0)
        expect(document.introduction[language].length).toBeGreaterThan(20)
        for (const section of document.sections) {
          expect(section.title[language].length).toBeGreaterThan(0)
          expect(section.paragraphs.length).toBeGreaterThan(0)
          for (const paragraph of section.paragraphs)
            expect(paragraph[language].length).toBeGreaterThan(40)
        }
      }
    }
  })

  it('states wallet settlement and procurement exclusions instead of implying profit or withdrawal', () => {
    const clause = getPolicyDocument('supplier').sections.find(
      (section) => section.id === 'settlement',
    )!
    const english = clause.paragraphs.map((paragraph) => paragraph.en).join(' ')
    expect(english).toContain('not profit')
    expect(english).toContain('does not offer bank or cash withdrawal')
    expect(english).toContain('Reconcile each settlement')
  })

  it('keeps privacy limits and ranking provenance visible for consumer decisions', () => {
    const privacy = getPolicyDocument('privacy')
      .sections.flatMap((section) => section.paragraphs)
      .map((paragraph) => paragraph.en)
      .join(' ')
    expect(privacy).toContain('does not mean that data is processed only in Hong Kong')
    expect(privacy).toContain('self-declarations')
    const market = getPolicyDocument('market')
      .sections.flatMap((section) => section.paragraphs)
      .map((paragraph) => paragraph.en)
      .join(' ')
    expect(market).toContain('Wilson lower bound')
    expect(market).toContain('excluded from success-rate accounting')
    expect(market).toContain('weight consumers equally')
    expect(policyContentVersion).toBe('2026-10-07')
  })
})
