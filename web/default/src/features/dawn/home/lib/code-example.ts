/* Copyright (C) 2023-2026 QuantumNous. AGPL-3.0-or-later. */
import { z } from 'zod'

export const DEFAULT_ENDPOINT = 'https://YOUR_CODEGO_HOST/v1'
export type CodeLanguage = 'curl' | 'python' | 'javascript'

export const endpointFormSchema = z.object({
  endpoint: z
    .string()
    .trim()
    .transform((value, context) => {
      function fail(message: string): never {
        context.addIssue({ code: 'custom', message })
        return z.NEVER
      }
      if (!value) return fail('marketHome.endpointRequired')
      let url: URL
      try {
        url = new URL(value)
      } catch {
        return fail('marketHome.endpointInvalid')
      }
      const local = ['localhost', '127.0.0.1', '[::1]'].includes(url.hostname)
      if (url.protocol !== 'https:' && !(url.protocol === 'http:' && local))
        return fail('marketHome.endpointProtocol')
      if (url.username || url.password || url.search || url.hash)
        return fail('marketHome.endpointPrivate')
      const path = url.pathname.replace(/\/+$/, '')
      url.pathname = path.endsWith('/v1') ? path : `${path}/v1`
      return url.href.replace(/\/$/, '')
    }),
})

export function createCodeExample(
  language: CodeLanguage,
  endpoint: string,
  comment: string,
  modelId?: string
): string {
  const literal = (value: string): string =>
    JSON.stringify(value)
      .replace(/\u2028/g, '\\u2028')
      .replace(/\u2029/g, '\\u2029')
  const configured = endpoint !== DEFAULT_ENDPOINT
  const heading = comment
    ? `${language === 'javascript' ? '//' : '#'} ${comment}\n`
    : ''
  if (language === 'python') {
    const base = configured
      ? literal(endpoint)
      : 'os.environ["CODEGO_BASE_URL"]'
    const action = modelId
      ? `MODEL_ID = ${modelId === 'MODEL_ID' ? 'os.environ["MODEL_ID"]' : literal(modelId)}\n\nresponse = client.chat.completions.create(\n    model=MODEL_ID,\n    messages=[{"role": "user", "content": "Hello"}],\n)\nprint(response.choices[0].message.content)`
      : 'for model in client.models.list():\n    print(model.id)'
    return `${heading}import os\nfrom openai import OpenAI\n\nCODEGO_BASE_URL = ${base}\nclient = OpenAI(\n    base_url=CODEGO_BASE_URL,\n    api_key=os.environ["CODEGO_API_KEY"],\n)\n\n${action}`
  }
  if (language === 'javascript') {
    const base = configured ? literal(endpoint) : 'process.env.CODEGO_BASE_URL'
    const action = modelId
      ? `const MODEL_ID = ${modelId === 'MODEL_ID' ? 'process.env.MODEL_ID' : literal(modelId)};\n\nconst response = await client.chat.completions.create({\n  model: MODEL_ID,\n  messages: [{ role: "user", content: "Hello" }],\n});\nconsole.log(response.choices[0].message.content);`
      : 'for await (const model of client.models.list()) {\n  console.log(model.id);\n}'
    return `${heading}import OpenAI from 'openai';\n\nconst CODEGO_BASE_URL = ${base};\nconst client = new OpenAI({\n  baseURL: CODEGO_BASE_URL,\n  apiKey: process.env.CODEGO_API_KEY,\n});\n\n${action}`
  }
  const quoteShell = (value: string): string =>
    "'" + value.replace(/'/g, "'\\''") + "'"
  const setup = configured ? `CODEGO_BASE_URL=${quoteShell(endpoint)}\n\n` : ''
  const path = modelId ? 'chat/completions' : 'models'
  let body = quoteShell(
    JSON.stringify({
      model: modelId,
      messages: [{ role: 'user', content: 'Hello' }],
    })
  )
  if (modelId === 'MODEL_ID') {
    body =
      '"{\\"model\\":\\"$MODEL_ID\\",\\"messages\\":[{\\"role\\":\\"user\\",\\"content\\":\\"Hello\\"}]}"'
  }
  const payload = modelId ? ` \\\n  --data ${body}` : ''
  return `${heading}${setup}curl "$CODEGO_BASE_URL/${path}" \\\n  --header "Authorization: Bearer $CODEGO_API_KEY" \\\n  --header "Content-Type: application/json"${payload}`
}
