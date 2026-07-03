const E2E_PREFIX = '🔒'

function base64ToBuf(b64: string): Uint8Array {
  const bin = atob(b64.replace(/-/g, '+').replace(/_/g, '/'))
  const buf = new Uint8Array(bin.length)
  for (let i = 0; i < bin.length; i++) buf[i] = bin.charCodeAt(i)
  return buf
}

async function rawKeyToCryptoKey(rawB64: string): Promise<CryptoKey> {
  const raw = base64ToBuf(rawB64)
  return crypto.subtle.importKey('raw', raw, 'AES-GCM', false, ['decrypt'])
}

/** Try to decrypt an E2E message. Uses group key if available, otherwise derives from code. */
export async function tryDecrypt(content: string, groupKey: string, groupCode?: string): Promise<string> {
  if (!content.startsWith(E2E_PREFIX)) return content
  const cipherB64 = content.slice(E2E_PREFIX.length)

  // Try direct key first, fallback to derived
  const keys: string[] = []
  if (groupKey) keys.push(groupKey)
  if (groupCode) {
    const enc = new TextEncoder().encode('lanlink-group:' + groupCode)
    const hash = await crypto.subtle.digest('SHA-256', enc)
    const rawDerived = new Uint8Array(hash).slice(0, 32)
    const derivedB64 = btoa(String.fromCharCode(...rawDerived)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')
    keys.push(derivedB64)
  }

  for (const k of keys) {
    try {
      const key = await rawKeyToCryptoKey(k)
      const raw = base64ToBuf(cipherB64)
      const nonce = raw.slice(0, 12)
      const ct = raw.slice(12)
      const pt = await crypto.subtle.decrypt({ name: 'AES-GCM', iv: nonce }, key, ct)
      return new TextDecoder().decode(pt)
    } catch { continue }
  }
  return '[🔒]'
}

export function isEncrypted(content: string): boolean {
  return content.startsWith(E2E_PREFIX)
}
