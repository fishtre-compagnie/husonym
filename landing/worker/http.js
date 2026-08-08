// Small shared helpers for the JSON API surface.

export function json(body, status = 200, headers = {}) {
  return new Response(JSON.stringify(body), {
    status,
    headers: {
      'content-type': 'application/json; charset=utf-8',
      // Nothing here is cacheable and none of it should be sniffed.
      'cache-control': 'no-store',
      'x-content-type-options': 'nosniff',
      ...headers,
    },
  });
}

export function isJson(request) {
  return (request.headers.get('content-type') || '').toLowerCase().includes('application/json');
}

// Rejects on the declared length alone. A chunked request without content-length is
// let through: the body is read with a hard cap anyway (see readJson).
export function declaredTooLarge(request, maxBytes) {
  const len = Number(request.headers.get('content-length'));
  return Number.isFinite(len) && len > maxBytes;
}

// Reads the body with an actual byte ceiling, so a lying content-length cannot make
// the Worker buffer an unbounded payload.
export async function readJson(request, maxBytes) {
  const buffer = await request.arrayBuffer();
  if (buffer.byteLength > maxBytes) return undefined;
  try {
    return JSON.parse(new TextDecoder().decode(buffer));
  } catch {
    return undefined;
  }
}
