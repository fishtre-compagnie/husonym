import { NextRequest, NextResponse } from 'next/server';
import {
  ACCOUNT_COOKIE,
  sanitizeSlug,
} from '../../[...nextauth]/account-provider';

/**
 * The per-account sign-in link: `/api/auth/account/<slug>`.
 *
 * It exists because the two legs of an OIDC flow have to agree on the account, and only
 * the first one carries it. The callback arrives with `code` and `state` and nothing else,
 * so the account is remembered here, in a cookie, before the flow starts.
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

  // The flow is not started from here: Auth.js starts a sign-in only on a POST that carries
  // its CSRF token, and answers a GET with an error. The app does that itself when it finds
  // no session, and the cookie set here tells that sign-in which account it is for. The
  // redirect and the cookie go by the deployment's public URL: behind a proxy, the request
  // may carry an internal origin.
  const publicUrl = new URL(getPublicBaseUrl(req));
  const res = NextResponse.redirect(new URL('/', publicUrl));
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

function getPublicBaseUrl(req: NextRequest): string {
  return process.env.AUTH_URL ?? process.env.NEXTAUTH_URL ?? req.nextUrl.origin;
}
