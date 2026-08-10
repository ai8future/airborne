import { afterEach, describe, expect, it, vi } from "vitest";

import { generateUUID } from "@/lib/uuid";

describe("generateUUID", () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.useRealTimers();
  });

  it("prefers the platform randomUUID implementation", () => {
    const randomUUID = vi.fn(() => "11111111-1111-4111-8111-111111111111");
    const getRandomValues = vi.fn((values: Uint8Array) => values);

    expect(generateUUID({ randomUUID, getRandomValues })).toBe(
      "11111111-1111-4111-8111-111111111111",
    );
    expect(getRandomValues).not.toHaveBeenCalled();
  });

  it("builds an RFC 4122 UUID with getRandomValues when randomUUID is absent", () => {
    const getRandomValues = vi.fn((values: Uint8Array) => {
      values.forEach((_, index) => { values[index] = index; });
      return values;
    });

    expect(generateUUID({ getRandomValues })).toBe(
      "00010203-0405-4607-8809-0a0b0c0d0e0f",
    );
  });

  it("produces distinct RFC 4122 UUIDs when browser crypto is unavailable", () => {
    vi.useFakeTimers();
    vi.setSystemTime(new Date("2026-07-14T00:00:00Z"));
    vi.spyOn(Math, "random").mockReturnValue(0.25);

    const first = generateUUID(null);
    const second = generateUUID(null);
    const versionFour = /^[0-9a-f]{8}-[0-9a-f]{4}-4[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$/;
    expect(first).toMatch(versionFour);
    expect(second).toMatch(versionFour);
    expect(second).not.toBe(first);
  });
});
