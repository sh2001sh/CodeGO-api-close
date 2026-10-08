// Generates env/config snippets for common client tools, using the current
// origin and a key prefix placeholder (the real secret is never embedded here
// unless the caller explicitly passes it in after a reveal).
export interface ClientConfigSnippet {
  id: string
  label: string
  code: string
}

export function clientConfigSnippets(origin: string, apiKey: string): ClientConfigSnippet[] {
  const baseURL = `${origin}/v1`
  return [
    {
      id: 'openai-env',
      label: 'OpenAI SDK (env)',
      code: `export OPENAI_BASE_URL="${baseURL}"\nexport OPENAI_API_KEY="${apiKey}"`,
    },
    {
      id: 'claude-code-env',
      label: 'Claude Code (env)',
      code: `export ANTHROPIC_BASE_URL="${origin}"\nexport ANTHROPIC_API_KEY="${apiKey}"`,
    },
    {
      id: 'curl',
      label: 'curl',
      code: `curl ${baseURL}/chat/completions \\\n  -H "Authorization: Bearer ${apiKey}" \\\n  -H "Content-Type: application/json" \\\n  -d '{"model": "gpt-4o-mini", "messages": [{"role": "user", "content": "Hello"}]}'`,
    },
  ]
}
