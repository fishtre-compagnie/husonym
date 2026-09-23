import { NextRequest, NextResponse } from 'next/server';
import {
  ACCOUNT_COOKIE,
  PROVIDER_ID,
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

  const signIn = new URL(`/api/auth/signin/${PROVIDER_ID}`, req.nextUrl.origin);
  signIn.searchParams.set('account', slug);

  const res = NextResponse.redirect(signIn);
  res.cookies.set({
    name: ACCOUNT_COOKIE,
    value: slug,
    httpOnly: true,
    sameSite: 'lax',
    secure: req.nextUrl.protocol === 'https:',
    path: '/',
    // Long enough for a sign-in, including a detour through a provider that asks for a
    // second factor. Not a session: it says which door was used, never who came through.
    maxAge: 15 * 60,
  });
  return res;
}
