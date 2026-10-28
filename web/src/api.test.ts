import { describe, it, expect } from "vitest";
import { bytes, active, downloadSpeed, type Selection } from "./api";
describe("network presentation", () => {
  it("measures proxy byte deltas over the actual sample interval", () => {
    const previous = {
      downloaded_bytes: 2000000,
      uptime_seconds: 10,
      started_at: "first",
    };
    const current = {
      ...previous,
      downloaded_bytes: 7000000,
      uptime_seconds: 12.5,
    };
    expect(downloadSpeed(previous, current)).toBe(2000000);
    expect(
      downloadSpeed(previous, { ...current, downloaded_bytes: 2000000 }),
    ).toBe(0);
    expect(
      downloadSpeed(previous, { ...current, downloaded_bytes: 1 }),
    ).toBeNull();
    expect(
      downloadSpeed(previous, { ...current, uptime_seconds: 10 }),
    ).toBeNull();
    expect(
      downloadSpeed(previous, { ...current, uptime_seconds: 9 }),
    ).toBeNull();
    expect(
      downloadSpeed(previous, { ...current, started_at: "restarted" }),
    ).toBeNull();
  });
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
