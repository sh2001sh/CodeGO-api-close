import { api, unwrap } from './api'

function decode(value: string): ArrayBuffer {
  const binary = atob(value.replace(/-/g, '+').replace(/_/g, '/'))
  return Uint8Array.from(binary, (character) => character.charCodeAt(0)).buffer
}
function encode(value: ArrayBuffer): string {
  return btoa(String.fromCharCode(...new Uint8Array(value)))
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '')
}
function object(value: unknown): Record<string, unknown> {
  if (!value || typeof value !== 'object' || Array.isArray(value))
    throw new Error('通行密钥选项无效')
  return Object.fromEntries(Object.entries(value))
}
function text(value: unknown): string {
  if (typeof value !== 'string') throw new Error('通行密钥选项无效')
  return value
}
function verification(value: unknown): UserVerificationRequirement | undefined {
  if (value === undefined) return undefined
  if (value === 'required' || value === 'preferred' || value === 'discouraged') return value
  throw new Error('通行密钥验证选项无效')
}
function transport(value: unknown): AuthenticatorTransport {
  if (
    value === 'usb' ||
    value === 'nfc' ||
    value === 'ble' ||
    value === 'internal' ||
    value === 'hybrid'
  )
    return value
  throw new Error('通行密钥传输选项无效')
}
function descriptors(value: unknown): PublicKeyCredentialDescriptorJSON[] | undefined {
  if (value === undefined) return undefined
  if (!Array.isArray(value)) throw new Error('通行密钥选项无效')
  return value.map((item) => {
    const data = object(item)
    if (
      data.type !== 'public-key' ||
      (data.transports !== undefined && !Array.isArray(data.transports))
    )
      throw new Error('通行密钥选项无效')
    return { id: text(data.id), type: 'public-key', transports: data.transports?.map(transport) }
  })
}
function timeout(value: unknown): number | undefined {
  if (value === undefined) return undefined
  if (typeof value !== 'number' || !Number.isFinite(value) || value <= 0)
    throw new Error('通行密钥超时选项无效')
  return value
}
function requestOptions(value: unknown): PublicKeyCredentialRequestOptionsJSON {
  const data = object(object(value).publicKey)
  return {
    challenge: text(data.challenge),
    rpId: data.rpId === undefined ? undefined : text(data.rpId),
    timeout: timeout(data.timeout),
    userVerification: verification(data.userVerification),
    allowCredentials: descriptors(data.allowCredentials),
  }
}
function creationOptions(value: unknown): PublicKeyCredentialCreationOptionsJSON {
  const data = object(object(value).publicKey)
  const rp = object(data.rp)
  const user = object(data.user)
  if (!Array.isArray(data.pubKeyCredParams)) throw new Error('通行密钥算法选项无效')
  const pubKeyCredParams: PublicKeyCredentialParameters[] = data.pubKeyCredParams.map((item) => {
    const parameter = object(item)
    if (
      parameter.type !== 'public-key' ||
      typeof parameter.alg !== 'number' ||
      !Number.isSafeInteger(parameter.alg)
    )
      throw new Error('通行密钥算法选项无效')
    return { type: 'public-key', alg: parameter.alg }
  })
  let authenticatorSelection: AuthenticatorSelectionCriteria | undefined
  if (data.authenticatorSelection !== undefined) {
    const criteria = object(data.authenticatorSelection)
    const attachment = criteria.authenticatorAttachment
    const residentKey = criteria.residentKey
    if (attachment !== undefined && attachment !== 'platform' && attachment !== 'cross-platform')
      throw new Error('通行密钥验证器选项无效')
    if (
      residentKey !== undefined &&
      residentKey !== 'discouraged' &&
      residentKey !== 'preferred' &&
      residentKey !== 'required'
    )
      throw new Error('通行密钥验证器选项无效')
    if (
      criteria.requireResidentKey !== undefined &&
      typeof criteria.requireResidentKey !== 'boolean'
    )
      throw new Error('通行密钥验证器选项无效')
    authenticatorSelection = {
      authenticatorAttachment: attachment,
      residentKey,
      requireResidentKey: criteria.requireResidentKey,
      userVerification: verification(criteria.userVerification),
    }
  }
  const attestation = data.attestation
  if (
    attestation !== undefined &&
    attestation !== 'none' &&
    attestation !== 'direct' &&
    attestation !== 'indirect' &&
    attestation !== 'enterprise'
  )
    throw new Error('通行密钥证明选项无效')
  return {
    challenge: text(data.challenge),
    rp: { name: text(rp.name), id: rp.id === undefined ? undefined : text(rp.id) },
    user: { id: text(user.id), name: text(user.name), displayName: text(user.displayName) },
    pubKeyCredParams,
    timeout: timeout(data.timeout),
    excludeCredentials: descriptors(data.excludeCredentials),
    authenticatorSelection,
    attestation,
  }
}
function serialize(value: PublicKeyCredential) {
  const response = value.response
  const common = {
    id: value.id,
    rawId: encode(value.rawId),
    type: value.type,
    authenticatorAttachment: value.authenticatorAttachment,
    clientExtensionResults: value.getClientExtensionResults(),
  }
  if (response instanceof AuthenticatorAttestationResponse)
    return {
      ...common,
      response: {
        clientDataJSON: encode(response.clientDataJSON),
        attestationObject: encode(response.attestationObject),
        transports: response.getTransports(),
      },
    }
  if (response instanceof AuthenticatorAssertionResponse)
    return {
      ...common,
      response: {
        clientDataJSON: encode(response.clientDataJSON),
        authenticatorData: encode(response.authenticatorData),
        signature: encode(response.signature),
        userHandle: response.userHandle ? encode(response.userHandle) : null,
      },
    }
  throw new Error('通行密钥响应无效')
}
function supported(): void {
  if (!window.isSecureContext || !navigator.credentials || !window.PublicKeyCredential)
    throw new Error('当前浏览器或连接不支持通行密钥')
}
function nativeDescriptors(
  value: PublicKeyCredentialDescriptorJSON[] | undefined,
): PublicKeyCredentialDescriptor[] | undefined {
  return value?.map((item) => {
    if (item.type !== 'public-key') throw new Error('通行密钥选项无效')
    return { id: decode(item.id), type: 'public-key', transports: item.transports?.map(transport) }
  })
}
function nativeCreation(
  options: PublicKeyCredentialCreationOptionsJSON,
): PublicKeyCredentialCreationOptions {
  const attestation = options.attestation
  if (
    attestation !== undefined &&
    attestation !== 'none' &&
    attestation !== 'direct' &&
    attestation !== 'indirect' &&
    attestation !== 'enterprise'
  )
    throw new Error('通行密钥证明选项无效')
  const selection = options.authenticatorSelection
  const attachment = selection?.authenticatorAttachment
  const residentKey = selection?.residentKey
  if (attachment !== undefined && attachment !== 'platform' && attachment !== 'cross-platform')
    throw new Error('通行密钥验证器选项无效')
  if (
    residentKey !== undefined &&
    residentKey !== 'discouraged' &&
    residentKey !== 'preferred' &&
    residentKey !== 'required'
  )
    throw new Error('通行密钥验证器选项无效')
  return {
    challenge: decode(options.challenge),
    user: { ...options.user, id: decode(options.user.id) },
    rp: options.rp,
    timeout: options.timeout,
    attestation,
    excludeCredentials: nativeDescriptors(options.excludeCredentials),
    pubKeyCredParams: options.pubKeyCredParams.map((item) => {
      if (item.type !== 'public-key') throw new Error('通行密钥算法选项无效')
      return { type: 'public-key', alg: item.alg }
    }),
    authenticatorSelection: selection
      ? {
          authenticatorAttachment: attachment,
          residentKey,
          requireResidentKey: selection.requireResidentKey,
          userVerification: verification(selection.userVerification),
        }
      : undefined,
  }
}
export async function passkeyLogin(): Promise<void> {
  supported()
  const options = requestOptions(unwrap(await api.POST('/api/passkey/login/begin')))
  const publicKey =
    typeof PublicKeyCredential.parseRequestOptionsFromJSON === 'function'
      ? PublicKeyCredential.parseRequestOptionsFromJSON(options)
      : {
          challenge: decode(options.challenge),
          rpId: options.rpId,
          timeout: options.timeout,
          userVerification: verification(options.userVerification),
          allowCredentials: nativeDescriptors(options.allowCredentials),
        }
  const value = await navigator.credentials.get({ publicKey })
  if (!(value instanceof PublicKeyCredential)) throw new Error('通行密钥验证已取消')
  await api.POST('/api/passkey/login/finish', { body: serialize(value) })
}
export async function passkeyRegister(): Promise<void> {
  supported()
  const options = creationOptions(unwrap(await api.POST('/api/passkey/register/begin')))
  const publicKey =
    typeof PublicKeyCredential.parseCreationOptionsFromJSON === 'function'
      ? PublicKeyCredential.parseCreationOptionsFromJSON(options)
      : nativeCreation(options)
  const value = await navigator.credentials.create({ publicKey })
  if (!(value instanceof PublicKeyCredential)) throw new Error('通行密钥验证已取消')
  await api.POST('/api/passkey/register/finish', { body: serialize(value) })
}
