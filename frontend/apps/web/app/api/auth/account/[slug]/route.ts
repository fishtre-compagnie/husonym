import { NextRequest, NextResponse } from 'next/server';
import {
  ACCOUNT_COOKIE,
  getPublicBaseUrl,
  getSafeCallbackPath,
  sanitizeSlug,
} from '../../[...nextauth]/account-provider';

/**
 * The per-account sign-in link: `/api/auth/account/<slug>`, with an optional
 * `callbackUrl`, a path of this app to come back to.
 *
 * It remembers, in a cookie, the account the next sign-in is for; the sign-in reads it
 * when it starts, and binds it to its flow (login-flow.ts).
 *
 * Nothing here is a secret. A slug is public, and this route is reachable without
 * authenticating -- by necessity, since the point is to find out where to authenticate.
 */
export async function GET(
  req: NextRequest,
  { params }: { params: Promise<{ slug: string }> }
): Promise<NextResponse> {
  const { slug: rawSlug } = await params;
  const slug = sanitizeSlug(rawSlug);
  if (!slug) {
    return NextResponse.json({ error: 'invalid account' }, { status: 400 });
  }

  // The flow is not started from here: the page this sends to says which provider the
  // account signs in with, and what becomes of a session already open, before anything
  // starts. The redirect and the cookie go by the deployment's public URL: behind a proxy,
  // the request may carry an internal origin.
  const publicUrl = getPublicBaseUrl(req);
  const page = new URL(`/account-login/${slug}`, publicUrl);
  // Tells the page the cookie was just set: it does not send back here a second time.
  page.searchParams.set('set', '1');
  const callbackPath = getSafeCallbackPath(
    req.nextUrl.searchParams.get('callbackUrl')
  );
  if (callbackPath) {
    page.searchParams.set('callbackUrl', callbackPath);
  }
  const res = NextResponse.redirect(page);
  res.cookies.set({
    name: ACCOUNT_COOKIE,
    value: slug,
    httpOnly: true,
    sameSite: 'lax',
    secure: publicUrl.protocol === 'https:',
    path: '/',
    // Long enough for a sign-in, including a detour through a provider that asks for a
    // second factor. Not a session: it says which door was used, never who came through.
    maxAge: 15 * 60,
  });
  return res;
}
