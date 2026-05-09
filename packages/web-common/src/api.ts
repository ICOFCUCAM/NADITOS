// Tiny fetch wrapper used by all three Next.js apps. The API base is
// configured per-app via NEXT_PUBLIC_API_BASE; in dev we point each app
// directly at the relevant Go service port.

export class ApiError extends Error {
  status: number;
  code: string;
  constructor(status: number, code: string, message: string) {
    super(message);
    this.status = status;
    this.code = code;
  }
}

type FetchOpts = {
  method?: string;
  body?: unknown;
  token?: string | null;
  tenant?: string | null;
  headers?: Record<string, string>;
  base?: string;
};

const DEFAULT_BASE =
  typeof process !== "undefined"
    ? process.env.NEXT_PUBLIC_API_BASE || "http://localhost"
    : "http://localhost";

const DEFAULT_TENANT =
  typeof process !== "undefined"
    ? process.env.NEXT_PUBLIC_DEFAULT_TENANT || "demo"
    : "demo";

export async function apiFetch<T>(path: string, opts: FetchOpts = {}): Promise<T> {
  const base = opts.base ?? DEFAULT_BASE;
  const headers: Record<string, string> = {
    Accept: "application/json",
    "X-Tenant-Id": opts.tenant ?? DEFAULT_TENANT,
    ...opts.headers,
  };
  if (opts.body !== undefined) headers["Content-Type"] = "application/json";
  if (opts.token) headers.Authorization = `Bearer ${opts.token}`;

  const res = await fetch(base + path, {
    method: opts.method ?? "GET",
    headers,
    body: opts.body !== undefined ? JSON.stringify(opts.body) : undefined,
    cache: "no-store",
  });

  if (res.status === 204) return undefined as unknown as T;

  const text = await res.text();
  const data = text ? JSON.parse(text) : null;
  if (!res.ok) {
    const code = data?.code ?? "error";
    const message = data?.message ?? res.statusText;
    throw new ApiError(res.status, code, message);
  }
  return data as T;
}

// Per-service convenience wrappers — each app uses the gateway URL in prod,
// but in dev points at the individual service ports.
export const services = {
  auth:       (path: string, opts?: FetchOpts) => apiFetch(path, { ...opts, base: process.env.NEXT_PUBLIC_AUTH_BASE       ?? DEFAULT_BASE }),
  registry:   (path: string, opts?: FetchOpts) => apiFetch(path, { ...opts, base: process.env.NEXT_PUBLIC_REGISTRY_BASE   ?? DEFAULT_BASE }),
  fines:      (path: string, opts?: FetchOpts) => apiFetch(path, { ...opts, base: process.env.NEXT_PUBLIC_FINES_BASE      ?? DEFAULT_BASE }),
  audit:      (path: string, opts?: FetchOpts) => apiFetch(path, { ...opts, base: process.env.NEXT_PUBLIC_AUDIT_BASE      ?? DEFAULT_BASE }),
  license:    (path: string, opts?: FetchOpts) => apiFetch(path, { ...opts, base: process.env.NEXT_PUBLIC_LICENSE_BASE    ?? DEFAULT_BASE }),
  anpr:       (path: string, opts?: FetchOpts) => apiFetch(path, { ...opts, base: process.env.NEXT_PUBLIC_ANPR_BASE       ?? DEFAULT_BASE }),
  insurance:  (path: string, opts?: FetchOpts) => apiFetch(path, { ...opts, base: process.env.NEXT_PUBLIC_INSURANCE_BASE  ?? DEFAULT_BASE }),
  inspection: (path: string, opts?: FetchOpts) => apiFetch(path, { ...opts, base: process.env.NEXT_PUBLIC_INSPECTION_BASE ?? DEFAULT_BASE }),
  notify:     (path: string, opts?: FetchOpts) => apiFetch(path, { ...opts, base: process.env.NEXT_PUBLIC_NOTIFY_BASE     ?? DEFAULT_BASE }),
};
