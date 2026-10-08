export type SampleLanguage = 'curl' | 'python' | 'node'
export function docSample(
  sample: 'quickstart' | 'models' | 'responses' | 'responses-fast' | 'embeddings',
  language: SampleLanguage,
  base: string,
  selectedModel: string,
) {
  const model = selectedModel.trim() || 'YOUR_MODEL_ID'
  const jsonModel = JSON.stringify(model)
  if (sample === 'models')
    return [
      `curl ${JSON.stringify(`${base}/v1/models`)} ` + '\\',
      '  -H "Authorization: Bearer $CODEGO_API_KEY"',
    ].join('\n')
  if (sample === 'quickstart' && language === 'python')
    return `import os\nfrom openai import OpenAI\n\nclient = OpenAI(\n    base_url=${JSON.stringify(`${base}/v1`)},\n    api_key=os.environ["CODEGO_API_KEY"],\n)\nreply = client.chat.completions.create(\n    model=${jsonModel},\n    messages=[{"role": "user", "content": "Hello"}],\n)\nprint(reply.choices[0].message.content)`
  if (sample === 'quickstart' && language === 'node')
    return `import OpenAI from 'openai'\n\nconst client = new OpenAI({\n  baseURL: ${JSON.stringify(`${base}/v1`)},\n  apiKey: process.env.CODEGO_API_KEY,\n})\nconst reply = await client.chat.completions.create({\n  model: ${jsonModel},\n  messages: [{ role: 'user', content: 'Hello' }],\n})\nconsole.log(reply.choices[0].message.content)`
  const endpoint =
    sample === 'quickstart'
      ? 'chat/completions'
      : sample === 'responses-fast'
        ? 'responses'
        : sample
  const body =
    sample === 'quickstart'
      ? { model, messages: [{ role: 'user', content: 'Hello' }], stream: true }
      : sample === 'responses-fast'
        ? { model, input: 'Hello', service_tier: 'fast' }
        : { model, input: 'Hello' }
  return [
    `curl -N ${JSON.stringify(`${base}/v1/${endpoint}`)} ` + '\\',
    '  -H "Authorization: Bearer $CODEGO_API_KEY" ' + '\\',
    '  -H "Content-Type: application/json" ' + '\\',
    "  --data-binary @- <<'CODEGO_JSON'",
    JSON.stringify(body, null, 2),
    'CODEGO_JSON',
  ].join('\n')
}
