import { NextFetchEvent, NextRequest, NextResponse } from 'next/server';
import { auth } from './app/api/auth/[...nextauth]/auth';
import { PUBLIC_PATHNAME, getSystemAppConfig } from './app/api/config/config';

// The configuration is lazy (a function of the request): Auth.js then hands back the wrapped
// middleware as a promise, which Next.js does not take for a middleware.
const withAuth = auth((req) => {
  if (req.nextUrl.pathname.startsWith(PUBLIC_PATHNAME)) {
    const sysConfig = getSystemAppConfig();
    if (req.auth?.accessToken) {
      req.headers.set('Authorization', `Bearer ${req.auth.accessToken}`);
    }
    return NextResponse.rewrite(
      `${sysConfig.husonymApiBaseUrl}${trimPrefix(req.nextUrl.pathname, PUBLIC_PATHNAME)}${req.nextUrl.search}`,
      {
        request: req,
      }
    );
  }
  return NextResponse.next();
});

export default async function middleware(
  req: NextRequest,
  event: NextFetchEvent
): Promise<Response | undefined> {
  const handler = await withAuth;
  // Auth.js types its wrapper as a route handler; as a middleware it hands it the fetch
  // event, as Next.js gives it.
  return (
    (await handler(req, event as unknown as Parameters<typeof handler>[1])) ??
    undefined
  );
}

function trimPrefix(str: string, prefix: string): string {
  if (str.startsWith(prefix)) {
    return str.slice(prefix.length);
  }
  return str;
}
