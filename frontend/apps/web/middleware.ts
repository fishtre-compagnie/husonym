import { NextFetchEvent, NextRequest, NextResponse } from 'next/server';
import { getToken } from 'next-auth/jwt';
import { isSecureRequest } from './app/api/auth/[...nextauth]/account-provider';
import { auth } from './app/api/auth/[...nextauth]/auth';
import { PUBLIC_PATHNAME, getSystemAppConfig } from './app/api/config/config';

// The middleware proxies the browser's calls under PUBLIC_PATHNAME to the API, with the
// session's access token; nothing else needs it. Run on every request, it read the session
// for each asset — refreshing an expired token as many times at once — and, since the
// configuration is per account, looked the account's login method up each time too.
// Next.js reads the matcher statically: it repeats PUBLIC_PATHNAME as a literal.
export const config = {
  matcher: ['/api/husonym/:path*'],
};

// The configuration is lazy (a function of the request): Auth.js then hands back the wrapped
// middleware as a promise, which Next.js does not take for a middleware.
const withAuth = auth(async (req) => {
  const target = getApiTarget(req.nextUrl);
  if (!target) {
    return new NextResponse(null, { status: 404 });
  }
  const unchanged = (await getAccessToken(req)) === req.auth?.accessToken;
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
 * Set by the wrapped middleware when the session's access token was not refreshed on the
 * way; read, and removed, before the response leaves.
 */
const SESSION_UNCHANGED = 'x-husonym-session-unchanged';

export default async function middleware(
  req: NextRequest,
  event: NextFetchEvent
): Promise<Response> {
  const handler = await withAuth;
  // Auth.js types its wrapper as a route handler; as a middleware it hands it the fetch
  // event, as Next.js gives it, and always answers.
  const res = (await handler(
    req,
    event as unknown as Parameters<typeof handler>[1]
  )) as Response;
  if (!res.headers.has(SESSION_UNCHANGED)) {
    return res;
  }
  // Auth.js sets the session cookie again on every answer. From here, a call still on its
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

// The access token of the session the request carries, before Auth.js refreshes it.
async function getAccessToken(req: NextRequest): Promise<string | undefined> {
  const secret = process.env.AUTH_SECRET ?? process.env.NEXTAUTH_SECRET;
  if (!secret) {
    return undefined;
  }
  const token = await getToken({
    req,
    secret,
    secureCookie: isSecureRequest(req),
  });
  return typeof token?.accessToken === 'string' ? token.accessToken : undefined;
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
