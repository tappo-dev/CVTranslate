import { afterEach, describe, expect, it, vi } from "vitest";
import { request, requestAudio } from "./api";

afterEach(() => vi.unstubAllGlobals());

describe("local API without browser credentials", () => {
  it("loads JSON state directly without a session exchange", async () => {
    const fetchMock = vi.fn().mockResolvedValue(Response.json({ version: "test" }));
    vi.stubGlobal("fetch", fetchMock);
    await expect(request("/state")).resolves.toEqual({ version: "test" });
    expect(fetchMock).toHaveBeenCalledExactlyOnceWith("/api/state", expect.objectContaining({
      method: "GET", credentials: "omit", headers: {},
    }));
  });

  it.each([
    { path: "/speech", body: { text: "Hello", language: "auto" } },
    { path: "/speech/preview", body: { language: "en", voiceId: "en-US-AriaNeural" } },
  ])("sends a cancellable 130 second $path request without cookies or CSRF tokens", async ({ path, body }) => {
    const timeout = vi.spyOn(AbortSignal, "timeout");
    const fetchMock = vi.fn()
      .mockResolvedValueOnce(new Response("audio", { headers: { "Content-Type": "audio/mpeg" } }));
    vi.stubGlobal("fetch", fetchMock);
    const controller = new AbortController();
    const audio = await requestAudio(path, { method: "POST", body, signal: controller.signal });
    expect(audio.type).toBe("audio/mpeg");
    expect(fetchMock).toHaveBeenCalledExactlyOnceWith(`/api${path}`, expect.objectContaining({
      credentials: "omit", method: "POST",
      headers: { "Content-Type": "application/json" },
      body: JSON.stringify(body),
    }));
    expect(timeout).toHaveBeenCalledWith(130000);
    controller.abort();
    expect((fetchMock.mock.lastCall?.[1].signal as AbortSignal).aborted).toBe(true);
    timeout.mockRestore();
  });

  it("preserves safe JSON errors and never retries speech", async () => {
    const fetchMock = vi.fn().mockResolvedValue(Response.json({ error: { code: "speech_unavailable", message: "Safe message" } }, { status: 503 }));
    vi.stubGlobal("fetch", fetchMock);
    await expect(requestAudio("/speech", { method: "POST", body: {} })).rejects.toMatchObject({ code: "speech_unavailable", message: "Safe message" });
    expect(fetchMock).toHaveBeenCalledOnce();
  });

  it.each([
    () => new Response("{}", { headers: { "Content-Type": "application/json" } }),
    () => new Response("", { headers: { "Content-Type": "audio/mpeg" } }),
    () => new Response("audio", { headers: { "Content-Type": "audio/mpeg", "Content-Length": String(16 * 1024 * 1024 + 1) } }),
    () => new Response(new Uint8Array(16 * 1024 * 1024 + 1), { headers: { "Content-Type": "audio/mpeg" } }),
  ])("rejects malformed or oversized responses", async (response) => {
    vi.stubGlobal("fetch", vi.fn().mockResolvedValue(response()));
    await expect(requestAudio("/speech", { method: "POST", body: {} })).rejects.toMatchObject({ code: "invalid_audio" });
  });
});
