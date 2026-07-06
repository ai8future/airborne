const SAFE_EXTERNAL_PROTOCOLS = new Set(["http:", "https:"]);

export interface SafeExternalURL {
  href: string;
  hostname: string;
}

export function safeExternalURL(raw: string | null | undefined): SafeExternalURL | null {
  const value = raw?.trim();
  if (!value) return null;

  try {
    const parsed = new URL(value);
    if (!SAFE_EXTERNAL_PROTOCOLS.has(parsed.protocol)) {
      return null;
    }
    return {
      href: parsed.href,
      hostname: parsed.hostname,
    };
  } catch {
    return null;
  }
}

export function displayHostname(raw: string | null | undefined): string {
  return safeExternalURL(raw)?.hostname || raw?.trim() || "";
}
