import { getSafePath } from '@/libs/safe-path';
import { NextRequest, NextResponse } from 'next/server';
import {
  getPublicBaseUrl,
  sanitizeSlug,
} from '../../[...nextauth]/account-provider';

/**
 * The per-account sign-in link: `/api/auth/account/<slug>`, with an optional
 * `callbackUrl`, a path of this app to come back to.
 *
 * It leads to the page that names the provider the account signs in with; the sign-in
 * starts from there, on a click, and that click is what chooses the account
 * (login-flow.ts). Links sent out before that page existed keep working.
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
  // By the deployment's public URL: behind a proxy, the request may carry an internal
  // origin.
  const page = new URL(`/account-login/${slug}`, getPublicBaseUrl(req));
  const callbackPath = getSafePath(req.nextUrl.searchParams.get('callbackUrl'));
  if (callbackPath) {
    page.searchParams.set('callbackUrl', callbackPath);
  }
  return NextResponse.redirect(page);
}
