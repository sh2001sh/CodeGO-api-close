export type SampleLanguage = 'curl' | 'python' | 'node'
export type DocSample =
  | 'quickstart'
  | 'models'
  | 'responses'
  | 'responses-fast'
  | 'responses-ultrafast'
  | 'responses-streaming'
  | 'environment'
  | 'embeddings'
  | 'streaming'
  | 'tools'
  | 'tool-result'
  | 'structured'
  | 'vision'

function pythonLiteral(value: unknown, level = 0): string {
  if (value === null) return 'None'
  if (typeof value === 'boolean') return value ? 'True' : 'False'
  if (typeof value === 'string' || typeof value === 'number')
    return JSON.stringify(value)
  const indent = '    '.repeat(level)
  const childIndent = `${indent}    `
  if (Array.isArray(value))
    return `[\n${value.map((item) => `${childIndent}${pythonLiteral(item, level + 1)}`).join(',\n')}\n${indent}]`
  if (typeof value === 'object')
    return `{\n${Object.entries(value)
      .map(
        ([key, item]) =>
          `${childIndent}${JSON.stringify(key)}: ${pythonLiteral(item, level + 1)}`
      )
      .join(',\n')}\n${indent}}`
  throw new Error('Unsupported documentation sample value')
}

function sampleRequest(sample: DocSample, model: string) {
  if (sample.startsWith('responses')) {
    const body: Record<string, unknown> = { model, input: 'Hello' }
    if (sample === 'responses-streaming') body.stream = true
    if (sample === 'responses-fast') body.service_tier = 'priority'
    if (sample === 'responses-ultrafast') body.service_tier = 'ultrafast'
    return { endpoint: 'responses', body }
  }
  if (sample === 'embeddings')
    return { endpoint: 'embeddings', body: { model, input: 'Hello' } }

  const body: Record<string, unknown> = {
    model,
    messages: [{ role: 'user', content: 'Hello' }],
  }
  if (sample === 'streaming') {
    body.stream = true
    body.stream_options = { include_usage: true }
  }
  if (sample === 'tools') {
    body.messages = [{ role: 'user', content: 'What time is it in Hong Kong?' }]
    body.tools = [
      {
        type: 'function',
        function: {
          name: 'get_time',
          description: 'Get the current time in a timezone',
          parameters: {
            type: 'object',
            properties: { timezone: { type: 'string' } },
            required: ['timezone'],
            additionalProperties: false,
          },
        },
      },
    ]
    body.tool_choice = 'auto'
  }
  if (sample === 'tool-result') {
    body.messages = [
      { role: 'user', content: 'What time is it in Hong Kong?' },
      {
        role: 'assistant',
        content: null,
        tool_calls: [
          {
            id: 'call_example',
            type: 'function',
            function: {
              name: 'get_time',
              arguments: '{"timezone":"Asia/Hong_Kong"}',
            },
          },
        ],
      },
      {
        role: 'tool',
        tool_call_id: 'call_example',
        content: '{"time":"12:00","timezone":"Asia/Hong_Kong"}',
      },
    ]
  }
  if (sample === 'structured') {
    body.messages = [
      { role: 'user', content: 'Return a JSON object with a greeting.' },
    ]
    body.response_format = {
      type: 'json_schema',
      json_schema: {
        name: 'greeting',
        strict: true,
        schema: {
          type: 'object',
          properties: { greeting: { type: 'string' } },
          required: ['greeting'],
          additionalProperties: false,
        },
      },
    }
  }
  if (sample === 'vision') {
    body.messages = [
      {
        role: 'user',
        content: [
          { type: 'text', text: 'Describe this image.' },
          {
            type: 'image_url',
            image_url: { url: 'https://YOUR_IMAGE_HOST/example.jpg' },
          },
        ],
      },
    ]
  }
  return { endpoint: 'chat/completions', body }
}

export function docSample(
  sample: DocSample,
  language: SampleLanguage,
  base: string,
  selectedModel: string
) {
  const model = selectedModel.trim() || 'YOUR_MODEL_ID'
  const apiBase = `${base.replace(/\/+$/, '')}/v1`
  if (sample === 'environment') {
    if (language === 'python')
      return 'python -m pip install --upgrade openai\n\n# Set CODEGO_API_KEY in your server environment.\n# Then run the Python request example.\npython app.py'
    if (language === 'node')
      return 'npm install openai\n\n# Set CODEGO_API_KEY in your server environment.\n# Save the Node.js request example as app.mjs.\nnode app.mjs'
    return '# macOS / Linux / server shell\n# Replace the placeholder locally. Keep it out of source control.\nexport CODEGO_API_KEY="YOUR_API_KEY"\n\n# PowerShell\n$env:CODEGO_API_KEY = "YOUR_API_KEY"'
  }
  const isModels = sample === 'models'
  const { endpoint, body } = sampleRequest(sample, model)
  if (language === 'python') {
    const setup = `import os\nfrom openai import OpenAI\n\nclient = OpenAI(\n    base_url=${JSON.stringify(apiBase)},\n    api_key=os.environ["CODEGO_API_KEY"],\n    timeout=60.0,\n    max_retries=0,\n)\n`
    if (isModels)
      return `${setup}\nfor model in client.models.list():\n    print(model.id)`
    const args = `payload = ${pythonLiteral(body)}\n`
    let method = 'chat.completions.create'
    if (endpoint === 'responses') method = 'responses.create'
    if (endpoint === 'embeddings') method = 'embeddings.create'
    if (sample === 'responses-streaming')
      return `${setup}\n${args}\ncompleted = False\nwith client.responses.create(**payload) as stream:\n    for event in stream:\n        if event.type == "response.output_text.delta":\n            print(event.delta, end="", flush=True)\n        elif event.type == "response.completed":\n            completed = True\n            print("\\nUsage:", event.response.usage)\n        elif event.type in ("response.failed", "response.incomplete", "error"):\n            raise RuntimeError(event.model_dump_json())\nif not completed:\n    raise RuntimeError("Stream ended without response.completed")\n`
    if (sample === 'streaming')
      return `${setup}\n${args}\nwith client.${method}(**payload) as stream:\n    for event in stream:\n        if event.choices:\n            print(event.choices[0].delta.content or "", end="", flush=True)\n        if event.usage:\n            print("\\nUsage:", event.usage)\n`
    return `${setup}\n${args}\nreply = client.${method}(**payload)\nprint(reply.model_dump_json(indent=2))`
  }
  if (language === 'node') {
    const setup = `import OpenAI from 'openai'\n\nconst client = new OpenAI({\n  baseURL: ${JSON.stringify(apiBase)},\n  apiKey: process.env.CODEGO_API_KEY,\n  timeout: 60000,\n  maxRetries: 0,\n})\n`
    if (isModels)
      return `${setup}\nfor await (const model of client.models.list()) {\n  console.log(model.id)\n}`
    let method = 'chat.completions.create'
    if (endpoint === 'responses') method = 'responses.create'
    if (endpoint === 'embeddings') method = 'embeddings.create'
    const payload = JSON.stringify(body, null, 2)
    if (sample === 'responses-streaming')
      return `${setup}\nconst stream = await client.responses.create(${payload})\nlet completed = false\ntry {\n  for await (const event of stream) {\n    if (event.type === 'response.output_text.delta') {\n      process.stdout.write(event.delta)\n    } else if (event.type === 'response.completed') {\n      completed = true\n      console.log('\\nUsage:', event.response.usage)\n    } else if (['response.failed', 'response.incomplete', 'error'].includes(event.type)) {\n      throw new Error(JSON.stringify(event))\n    }\n  }\n} finally {\n  stream.controller.abort()\n}\nif (!completed) throw new Error('Stream ended without response.completed')\n`
    if (sample === 'streaming')
      return `${setup}\nconst stream = await client.${method}(${payload})\nfor await (const event of stream) {\n  process.stdout.write(event.choices[0]?.delta.content ?? '')\n  if (event.usage) console.log('\\nUsage:', event.usage)\n}`
    return `${setup}\nconst reply = await client.${method}(${payload})\nconsole.log(JSON.stringify(reply, null, 2))`
  }
  if (isModels)
    return [
      `curl ${JSON.stringify(`${apiBase}/models`)} \\`,
      '  -H "Authorization: Bearer $CODEGO_API_KEY"',
    ].join('\n')
  const flags =
    sample === 'streaming' || sample === 'responses-streaming' ? '-N ' : ''
  return [
    `curl ${flags}${JSON.stringify(`${apiBase}/${endpoint}`)} \\`,
    '  -H "Authorization: Bearer $CODEGO_API_KEY" \\',
    '  -H "Content-Type: application/json" \\',
    "  --data-binary @- <<'CODEGO_JSON'",
    JSON.stringify(body, null, 2),
    'CODEGO_JSON',
  ].join('\n')
}
