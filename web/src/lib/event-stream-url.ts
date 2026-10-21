// Keep bearer credentials out of same-origin stream URLs, which may be logged.
export function eventStreamUrl(
  path: string,
  base: string,
  pageUrl: string | undefined,
  token: string | null,
): string {
  const endpoint = `${base}${path}`;
  if (pageUrl && new URL(endpoint, pageUrl).origin === new URL(pageUrl).origin) {
    return endpoint;
  }
  if (!token) return endpoint;
  const separator = path.includes("?") ? "&" : "?";
  return `${endpoint}${separator}token=${encodeURIComponent(token)}`;
}
