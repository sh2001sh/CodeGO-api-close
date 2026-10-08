// Builds curl/python equivalents of the current playground request for the "查看代码" dialog.
import type { ChatMessage, PlaygroundSettings } from './types'

function messagesPayload(history: ChatMessage[], settings: PlaygroundSettings) {
  const messages: { role: string; content: string }[] = []
  if (settings.systemPrompt.trim())
    messages.push({ role: 'system', content: settings.systemPrompt })
  for (const message of history) {
    if (message.role === 'system') continue
    messages.push({ role: message.role, content: message.content })
  }
  return messages
}

function extraParams(settings: PlaygroundSettings): Record<string, unknown> {
  const extra: Record<string, unknown> = {}
  if (settings.temperature.trim()) extra.temperature = Number(settings.temperature)
  if (settings.maxTokens.trim()) extra.max_tokens = Number(settings.maxTokens)
  return extra
}

export function curlSample(
  origin: string,
  history: ChatMessage[],
  settings: PlaygroundSettings,
): string {
  const body = {
    model: settings.model,
    messages: messagesPayload(history, settings),
    stream: true,
    ...extraParams(settings),
  }
  const groupHeader = settings.group
    ? `  -H ${shellQuote(`X-CodeGo-Group: ${settings.group}`)} \\\n`
    : ''
  return `curl ${origin}/v1/chat/completions \\
  -H "Authorization: Bearer $API_KEY" \\
  -H "Content-Type: application/json" \\
${groupHeader}  -d ${shellQuote(JSON.stringify(body, null, 2))}`
}

function shellQuote(value: string): string {
  return `'${value.replaceAll("'", `'"'"'`)}'`
}

export function pythonSample(
  origin: string,
  history: ChatMessage[],
  settings: PlaygroundSettings,
): string {
  const body = {
    model: settings.model,
    messages: messagesPayload(history, settings),
    stream: true,
    ...extraParams(settings),
  }
  const groupHeader = settings.group
    ? `, extra_headers={"X-CodeGo-Group": ${JSON.stringify(settings.group)}}`
    : ''
  return `import json
from openai import OpenAI

client = OpenAI(base_url="${origin}/v1", api_key="$API_KEY")

payload = json.loads(${JSON.stringify(JSON.stringify(body))})
response = client.chat.completions.create(**payload${groupHeader})
for chunk in response:
    print(chunk.choices[0].delta.content or "", end="")`
}
