import { useEffect } from 'react'

type SiteSeoProps = {
  title: string
  description: string
  keywords?: string
  canonicalPath?: string
  ogType?: string
  robots?: string
  jsonLd?: Record<string, unknown> | Array<Record<string, unknown>>
}

const SITE_NAME = 'CodeGo'
const SITE_ALTERNATE_NAME = 'Code Go'
const SITE_ORIGIN = 'https://shu26.cfd'
const DEFAULT_OG_IMAGE = `${SITE_ORIGIN}/code-go-logo.svg`

function ensureMeta(selector: string, attribute: 'name' | 'property', value: string) {
  let element = document.head.querySelector(selector) as HTMLMetaElement | null
  if (!element) {
    element = document.createElement('meta')
    element.setAttribute(attribute, value)
    document.head.appendChild(element)
  }
  return element
}

function ensureLink(selector: string, rel: string) {
  let element = document.head.querySelector(selector) as HTMLLinkElement | null
  if (!element) {
    element = document.createElement('link')
    element.setAttribute('rel', rel)
    document.head.appendChild(element)
  }
  return element
}

export function SiteSeo(props: SiteSeoProps) {
  useEffect(() => {
    const fullTitle =
      props.title.includes(SITE_NAME) || props.title.includes(SITE_ALTERNATE_NAME)
      ? props.title
      : `${props.title} | ${SITE_NAME}`
    const canonicalUrl = `${SITE_ORIGIN}${props.canonicalPath || ''}`
    const previousTitle = document.title

    document.title = fullTitle

    ensureMeta('meta[name="description"]', 'name', 'description').content =
      props.description
    ensureMeta('meta[name="keywords"]', 'name', 'keywords').content =
      props.keywords ||
      'CodeGo, Code Go, AI Coding, Codex, Claude Code, AI API, OpenAI compatible API, Claude API, Gemini API'
    ensureMeta('meta[name="robots"]', 'name', 'robots').content =
      props.robots || 'index,follow'
    ensureMeta('meta[property="og:title"]', 'property', 'og:title').content =
      fullTitle
    ensureMeta(
      'meta[property="og:description"]',
      'property',
      'og:description'
    ).content = props.description
    ensureMeta('meta[property="og:type"]', 'property', 'og:type').content =
      props.ogType || 'website'
    ensureMeta('meta[property="og:url"]', 'property', 'og:url').content =
      canonicalUrl
    ensureMeta('meta[property="og:site_name"]', 'property', 'og:site_name').content =
      SITE_NAME
    ensureMeta('meta[property="og:image"]', 'property', 'og:image').content =
      DEFAULT_OG_IMAGE
    ensureMeta('meta[name="twitter:card"]', 'name', 'twitter:card').content =
      'summary_large_image'
    ensureMeta('meta[name="twitter:title"]', 'name', 'twitter:title').content =
      fullTitle
    ensureMeta(
      'meta[name="twitter:description"]',
      'name',
      'twitter:description'
    ).content = props.description
    ensureMeta('meta[name="twitter:image"]', 'name', 'twitter:image').content =
      DEFAULT_OG_IMAGE

    ensureLink('link[rel="canonical"]', 'canonical').href = canonicalUrl

    let script = document.getElementById('code-go-jsonld')
    if (props.jsonLd) {
      if (!script) {
        script = document.createElement('script')
        script.id = 'code-go-jsonld'
        script.setAttribute('type', 'application/ld+json')
        document.head.appendChild(script)
      }
      script.textContent = JSON.stringify(props.jsonLd)
    } else if (script) {
      script.remove()
    }

    return () => {
      document.title = previousTitle
    }
  }, [props.canonicalPath, props.description, props.jsonLd, props.keywords, props.ogType, props.robots, props.title])

  return null
}
