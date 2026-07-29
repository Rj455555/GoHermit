export const MAX_RUN_MESSAGE_BYTES = 16 << 10

const encoder = new TextEncoder()

export function normalizedRunMessage(value: string): string {
  return value.trim()
}

export function runMessageByteLength(value: string): number {
  return encoder.encode(normalizedRunMessage(value)).byteLength
}
