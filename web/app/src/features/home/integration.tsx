import { useState } from 'react'
import { useTranslation } from '../../lib/i18n'
import { CopyButton, CopyField, Segmented } from '../../components/ui'

type Lang = 'python' | 'node' | 'curl'
const langs: readonly { value: Lang; label: string }[] = [
  { value: 'python', label: 'Python' },
  { value: 'node', label: 'Node.js' },
  { value: 'curl', label: 'cURL' },
]

function sample(lang: Lang, base: string, model: string): string {
  if (lang === 'curl')
    return `curl ${base}/chat/completions \\
  -H "Authorization: Bearer $CODEGO_API_KEY" \\
  -H "Content-Type: application/json" \\
  -d '{
    "model": "${model}",
    "messages": [{"role": "user", "content": "Hello"}]
  }'`
  if (lang === 'node')
    return `import OpenAI from 'openai'

const client = new OpenAI({
  baseURL: '${base}',
  apiKey: process.env.CODEGO_API_KEY,
})
const reply = await client.chat.completions.create({
  model: '${model}',
  messages: [{ role: 'user', content: 'Hello' }],
})`
  return `import os
from openai import OpenAI

client = OpenAI(
    base_url="${base}",
    api_key=os.environ["CODEGO_API_KEY"],
)
reply = client.chat.completions.create(
    model="${model}",
    messages=[{"role": "user", "content": "Hello"}],
)`
}

/** Base URL plus a copyable request in the visitor's language, using a model from the board. */
export function Integration(props: { model: string }) {
  const { t } = useTranslation()
  const [lang, setLang] = useState<Lang>('python')
  const base = `${window.location.origin}/v1`
  const code = sample(lang, base, props.model)
  return (
    <section className="integration" aria-labelledby="integration-title">
      <div className="integration-text">
        <h2 id="integration-title">{t('OpenAI 兼容接口')}</h2>
        <dl className="integration-facts">
          <div>
            <dt>Base URL</dt>
            <dd>
              <CopyField value={base} label="复制地址" />
            </dd>
          </div>
          <div>
            <dt>{t('认证')}</dt>
            <dd>
              <code>Authorization: Bearer sk-…</code>
            </dd>
          </div>
          <div>
            <dt>{t('计费')}</dt>
            <dd>{t('按模型计价规则、用量与分组倍率结算')}</dd>
          </div>
        </dl>
      </div>
      <div className="integration-code">
        <div className="integration-code-bar">
          <Segmented label="代码语言" value={lang} options={langs} onChange={setLang} />
          <CopyButton value={code} label="复制示例" />
        </div>
        <pre>
          <code>{code}</code>
        </pre>
      </div>
    </section>
  )
}
