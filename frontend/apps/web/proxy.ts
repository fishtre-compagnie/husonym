import { NextFetchEvent, NextRequest, NextResponse } from 'next/server';
import { getToken } from 'next-auth/jwt';
import { auth } from './app/api/auth/[...nextauth]/auth';
import { PUBLIC_PATHNAME, getSystemAppConfig } from './app/api/config/config';

// The proxy (Next.js's name for middleware, which runs on Node.js) relays the browser's
// calls under PUBLIC_PATHNAME to the API, with the session's access token; nothing else
// needs it. Run on every request, it read the session
// for each asset — refreshing an expired token as many times at once — and, since the
// configuration is per account, looked the account's login method up each time too.
// Next.js reads the matcher statically: it repeats PUBLIC_PATHNAME as a literal.
export const config = {
  matcher: ['/api/husonym/:path*'],
};

// The configuration is lazy (a function of the request): Auth.js then hands back the wrapped
// handler as a promise, which Next.js does not take for a proxy.
const withAuth = auth(async (req) => {
  const target = getApiTarget(req.nextUrl);
  if (!target) {
    return new NextResponse(null, { status: 404 });
  }
  // The session was refreshed on the way when the tokens it holds now were obtained later
  // than those the request came with.
  const unchanged = (await getRefreshedAt(req)) === req.auth?.refreshedAt;
  // The session cookie is the browser's to this app: the API is given the access token, and
  // nothing more.
  req.headers.delete('cookie');
  if (req.auth?.accessToken) {
    req.headers.set('Authorization', `Bearer ${req.auth.accessToken}`);
  }
  const res = NextResponse.rewrite(target, { request: req });
  if (unchanged) {
    res.headers.set(SESSION_UNCHANGED, '1');
  }
  return res;
});

/**
 * Set by the wrapped handler when the session's tokens were not refreshed on the way;
 * read, and removed, before the response leaves.
 */
const SESSION_UNCHANGED = 'x-husonym-session-unchanged';

export default async function proxy(
  req: NextRequest,
  event: NextFetchEvent
): Promise<Response> {
  const handler = await withAuth;
  // Auth.js types its wrapper as a route handler; as a proxy it hands it the fetch
  // event, as Next.js gives it, and always answers.
  const res = (await handler(
    req,
    event as unknown as Parameters<typeof handler>[1]
  )) as Response;
  if (!res.headers.has(SESSION_UNCHANGED)) {
    return res;
  }
  // Auth.js sets the session cookie again on every answer. Through here, a call still on its
  // way when the user signs out came back after the session was cleared and set it again:
  // signed out, the user was signed back in. The session is set again only when this call
  // refreshed its access token, which must be kept; /api/auth/session keeps it from
  // expiring.
  const out = new Response(res.body, res);
  out.headers.delete(SESSION_UNCHANGED);
  const cookies = out.headers.getSetCookie();
  out.headers.delete('Set-Cookie');
  for (const cookie of cookies) {
    if (!/^(__Secure-)?authjs\.session-token(\.\d+)?=/.test(cookie)) {
      out.headers.append('Set-Cookie', cookie);
    }
  }
  return out;
}

// When the tokens of the session the request carries were obtained, before Auth.js
// refreshes them; read from the session cookie the request has, with the secrets Auth.js
// accepts.
async function getRefreshedAt(req: NextRequest): Promise<number | undefined> {
  const secret = [
    process.env.AUTH_SECRET ?? process.env.NEXTAUTH_SECRET,
    process.env.AUTH_SECRET_1,
    process.env.AUTH_SECRET_2,
    process.env.AUTH_SECRET_3,
  ].filter((s): s is string => !!s);
  if (secret.length === 0) {
    return undefined;
  }
  const secureCookie = req.cookies
    .getAll()
    .some((c) => c.name.startsWith('__Secure-authjs.session-token'));
  const token = await getToken({ req, secret, secureCookie });
  return typeof token?.refreshedAt === 'number' ? token.refreshedAt : undefined;
}

// getApiTarget is where a call under PUBLIC_PATHNAME goes on the API, or nothing when the
// path is not under it or would lead elsewhere. Pasted after the API's base, the rest of a
// path such as /api/husonym@evil.com/x made the host evil.com, which then received the
// access token and the session cookie of whoever followed the link.
function getApiTarget(url: URL): URL | undefined {
  if (!url.pathname.startsWith(`${PUBLIC_PATHNAME}/`)) {
    return undefined;
  }
  const base = new URL(getSystemAppConfig().husonymApiBaseUrl);
  const target = new URL(base);
  target.pathname = `${base.pathname.replace(/\/+$/, '')}${url.pathname.slice(PUBLIC_PATHNAME.length)}`;
  target.search = url.search;
  if (target.origin !== base.origin) {
    return undefined;
  }
  return target;
}
