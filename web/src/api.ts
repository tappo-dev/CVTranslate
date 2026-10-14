export class ApiError extends Error {
  constructor(
    message: string,
    public status: number,
    public code: string,
  ) {
    super(message);
  }
}

interface RequestOptions {
  method?: string;
  body?: unknown;
  signal?: AbortSignal;
}

async function fetchResponse(
  path: string,
  options: RequestOptions,
): Promise<Response> {
  const method = options.method ?? "GET";
  const speechRequest = path === "/speech" || path === "/speech/preview";
  const timeout = AbortSignal.timeout(speechRequest ? 130000 : path.endsWith('/test') ? 310000 : 30000);
  const signal = options.signal ? AbortSignal.any([options.signal, timeout]) : timeout;
  const response = await fetch(`/api${path}`, {
    method,
    credentials: "omit",
    signal,
    headers: {
      ...(options.body !== undefined
        ? { "Content-Type": "application/json" }
        : {}),
    },
    body: options.body !== undefined ? JSON.stringify(options.body) : undefined,
  });
  if (!response.ok) {
    const payload = (await response.json().catch(() => null)) as {
      error?: { message?: string; code?: string };
    } | null;
    throw new ApiError(
      payload?.error?.message || `请求失败 (${response.status})`,
      response.status,
      payload?.error?.code || "request_failed",
    );
  }
  return response;
}

export async function request<Result>(path: string, options: RequestOptions = {}): Promise<Result> {
  const response = await fetchResponse(path, options);
  return response.status === 204
    ? (undefined as Result)
    : ((await response.json()) as Result);
}

export async function requestAudio(path: string, options: RequestOptions): Promise<Blob> {
  const response = await fetchResponse(path, options);
  const maximumBytes = 16 * 1024 * 1024;
  const invalidAudio = () => new ApiError("朗读服务返回了无效或过大的音频，请重试。", 502, "invalid_audio");
  if (response.headers.get("Content-Type")?.split(";")[0].trim().toLowerCase() !== "audio/mpeg" ||
      Number(response.headers.get("Content-Length")) > maximumBytes || !response.body) {
    await response.body?.cancel();
    throw invalidAudio();
  }
  const reader = response.body.getReader();
  const chunks: Uint8Array<ArrayBuffer>[] = [];
  let totalBytes = 0;
  try {
    while (true) {
      const { done, value } = await reader.read();
      if (done) break;
      totalBytes += value.byteLength;
      if (totalBytes > maximumBytes) {
        await reader.cancel();
        throw invalidAudio();
      }
      chunks.push(new Uint8Array(value));
    }
  } finally {
    reader.releaseLock();
  }
  if (!totalBytes) throw invalidAudio();
  return new Blob(chunks, { type: "audio/mpeg" });
}

export function errorMessage(error: unknown): string {
  if (error instanceof DOMException && error.name === 'TimeoutError') {
    return '请求超时。操作可能已在服务端完成，请先刷新状态；不会自动重试。';
  }
  return error instanceof Error ? error.message : "操作未完成，请重试。";
}
