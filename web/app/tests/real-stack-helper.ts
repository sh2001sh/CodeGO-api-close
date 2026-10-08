import { execFileSync } from 'node:child_process'
import type { Page } from '@playwright/test'

// Real tests create fixture accounts and may alter synthetic subscription data.
// Check the named Compose project actually publishes the selected local origin.
export async function prepareRealStack(page: Page) {
  const origin = process.env.V3_REAL_URL
  const project = process.env.V3_REAL_PROJECT
  if (!origin || !project || !/^codego-v3-[a-z0-9-]+$/.test(project))
    throw new Error('Set V3_REAL_URL and V3_REAL_PROJECT to an isolated CodeGo test project')
  const url = new URL(origin)
  if (
    url.protocol !== 'http:' ||
    !['localhost', '127.0.0.1'].includes(url.hostname) ||
    !url.port ||
    url.origin !== origin
  )
    throw new Error('Real tests require an explicit local HTTP origin with a port')
  const [container] = JSON.parse(
    execFileSync('docker', ['inspect', `${project}-web-1`], { encoding: 'utf8' }),
  )
  const bindings = container.HostConfig.PortBindings['80/tcp'] ?? []
  if (
    container.Config.Labels['com.docker.compose.project'] !== project ||
    !bindings.some(
      (binding: { HostIp: string; HostPort: string }) =>
        binding.HostIp === '127.0.0.1' && binding.HostPort === url.port,
    )
  )
    throw new Error('The selected URL does not belong to the named isolated Compose project')
  await page.addInitScript(() => localStorage.setItem('codego.locale', 'zh-CN'))
}
