import { describe, it, expect } from "vitest";
import { bytes, active, type Selection } from "./api";
describe("network presentation", () => {
  it("formats recorded bytes without changing units", () => {
    expect(bytes(1048576)).toBe("1.0 MiB");
    expect(bytes(1073741824)).toBe("1.0 GiB");
  });
  it("expired selections cannot appear active", () => {
    expect(
      active({
        ip: "1.1.1.1",
        valid_until: new Date(Date.now() - 1000).toISOString(),
      } as Selection),
    ).toBe(false);
    expect(
      active({
        ip: "1.1.1.1",
        valid_until: new Date(Date.now() + 60000).toISOString(),
      } as Selection),
    ).toBe(true);
  });
});
