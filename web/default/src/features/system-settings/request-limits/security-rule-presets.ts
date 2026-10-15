export const RECOMMENDED_HARD_BLOCK_RULES = [
  'contains:credential stuffing',
  'contains:steal browser cookies',
  'contains:sql injection payload',
  'contains:bypass rate limit',
  'contains:bypass moderation',
  'contains:evade safety filter',
  'contains:reverse shell',
  'contains:privilege escalation',
  'contains:keylogger',
  'contains:ransomware',
  'contains:malware loader',
  'contains:crack password',
  'contains:phishing page',
] as const

export const RECOMMENDED_GUARD_REVIEW_RULES = [
  'contains:crack password',
  'contains:cracking tool',
  'contains:credential stuffing',
  'contains:exploit chain',
  'contains:jailbreak prompt',
  'contains:payload delivery',
  'contains:privilege escalation',
  'contains:reverse shell',
  'contains:sql injection payload',
  'contains:steal browser cookies',
  'contains:bypass moderation',
  'contains:bypass rate limit',
  'contains:evade safety filter',
  'contains:phishing page',
  'contains:keylogger',
  'contains:ransomware',
  'contains:malware loader',
  're:(?i)\\b(ctf|pentest|red team|exploit|shellcode|reverse engineering|malware analysis)\\b',
] as const

export function presetRulesToTextareaValue(rules: readonly string[]) {
  return rules.join('\n')
}
