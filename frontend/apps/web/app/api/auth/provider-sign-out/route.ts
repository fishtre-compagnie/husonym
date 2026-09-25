import { getSafePath } from '@/libs/safe-path';
import { NextRequest, NextResponse } from 'next/server';
import { auth, getLogoutUrl } from '../[...nextauth]/auth';

export const dynamic = 'force-dynamic';

/**
 * Where to end the session at the provider it came from, for the session of the request.
 *
 * It is asked while the session is still there, before the app signs out: the session is
 * what says which provider issued it — the deployment's or an account's — and nothing the
 * browser sends is taken for it. Signed out first, the app used to end the session at the
 * deployment's provider whatever provider it came from.
 *
 * `returnTo`, a path of this app, is handed to the provider as the `state` of the logout,
 * which it gives back to the app's root: the app then goes on there (session-provider).
 */
export async function GET(req: NextRequest): Promise<NextResponse> {
  const nextauthUrl = process.env.NEXTAUTH_URL!;
  try {
    const session = await auth();
    if (!session?.idToken) {
      return NextResponse.json({ url: null });
    }
    const logoutUrl = await getLogoutUrl(session.accountIssuer);
    if (!logoutUrl) {
      return NextResponse.json({ url: null });
    }
    const qp = new URLSearchParams({
      id_token_hint: session.idToken,
      post_logout_redirect_uri: nextauthUrl,
    });
    const returnTo = getSafePath(req.nextUrl.searchParams.get('returnTo'));
    if (returnTo) {
      qp.set('state', returnTo);
    }
    return NextResponse.json({ url: `${logoutUrl}?${qp.toString()}` });
  } catch (error) {
    console.error('unable to find the provider logout url', 'error: ', error);
    return NextResponse.json({ url: null });
  }
}
