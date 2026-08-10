type CryptoUUIDSource = {
  randomUUID?: () => string;
  getRandomValues?: (values: Uint8Array) => Uint8Array;
};

let fallbackSequence = 0;

function fillWithoutCrypto(values: Uint8Array): void {
  fallbackSequence = (fallbackSequence + 1) >>> 0;
  let state = (
    Date.now() ^
    Math.floor(Math.random() * 0xffff_ffff) ^
    fallbackSequence
  ) >>> 0;

  for (let index = 0; index < values.length; index++) {
    state ^= state << 13;
    state ^= state >>> 17;
    state ^= state << 5;
    values[index] = (state + fallbackSequence + index * 31) & 0xff;
  }
}

/**
 * Generate an RFC 4122 version-4 UUID across secure, non-secure, and legacy
 * browser contexts. The final fallback is collision-resistant for UI request
 * correlation, but is not suitable as a security token.
 */
export function generateUUID(
  cryptoSource: CryptoUUIDSource | null = typeof globalThis.crypto === "undefined"
    ? null
    : globalThis.crypto,
): string {
  if (typeof cryptoSource?.randomUUID === "function") {
    try {
      return cryptoSource.randomUUID();
    } catch {
      // Some browser contexts expose the method but reject its use.
    }
  }

  const bytes = new Uint8Array(16);
  let cryptographicallyFilled = false;
  if (typeof cryptoSource?.getRandomValues === "function") {
    try {
      cryptoSource.getRandomValues(bytes);
      cryptographicallyFilled = true;
    } catch {
      // Continue with the non-cryptographic correlation-ID fallback.
    }
  }
  if (!cryptographicallyFilled) {
    fillWithoutCrypto(bytes);
  }

  bytes[6] = (bytes[6] & 0x0f) | 0x40;
  bytes[8] = (bytes[8] & 0x3f) | 0x80;
  const hex = Array.from(bytes, value => value.toString(16).padStart(2, "0")).join("");
  return [
    hex.slice(0, 8),
    hex.slice(8, 12),
    hex.slice(12, 16),
    hex.slice(16, 20),
    hex.slice(20),
  ].join("-");
}
