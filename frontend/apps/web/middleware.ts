import { NextFetchEvent, NextRequest, NextResponse } from 'next/server';
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
const withAuth = auth((req) => {
  const target = getApiTarget(req.nextUrl);
  if (!target) {
    return new NextResponse(null, { status: 404 });
  }
  // The session cookie is the browser's to this app: the API is given the access token, and
  // nothing more.
  req.headers.delete('cookie');
  if (req.auth?.accessToken) {
    req.headers.set('Authorization', `Bearer ${req.auth.accessToken}`);
  }
  return NextResponse.rewrite(target, { request: req });
});

export default async function middleware(
  req: NextRequest,
  event: NextFetchEvent
): Promise<Response> {
  const handler = await withAuth;
  // Auth.js types its wrapper as a route handler; as a middleware it hands it the fetch
  // event, as Next.js gives it, and always answers.
  return (await handler(
    req,
    event as unknown as Parameters<typeof handler>[1]
  )) as Response;
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
