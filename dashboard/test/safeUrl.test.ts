import { describe, expect, it } from "vitest";
import { displayHostname, safeExternalURL } from "@/lib/safeUrl";

describe("safe external URLs", () => {
  it("accepts HTTP URLs and extracts their hostname", () => {
    expect(safeExternalURL(" https://docs.example.test/path ")).toEqual({ href: "https://docs.example.test/path", hostname: "docs.example.test" });
    expect(displayHostname("https://docs.example.test/path")).toBe("docs.example.test");
  });
  it("rejects unsafe, empty, and malformed URLs", () => {
    expect(safeExternalURL("javascript:alert(1)")).toBeNull();
    expect(safeExternalURL("")).toBeNull();
    expect(safeExternalURL("not a url")).toBeNull();
    expect(displayHostname("not a url")).toBe("not a url");
  });
});
