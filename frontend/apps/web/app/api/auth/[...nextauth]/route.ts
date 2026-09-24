import { NextRequest } from 'next/server';
import { ACCOUNT_COOKIE, getPublicBaseUrl } from './account-provider';
import { GET as handleGet, POST as handlePost } from './auth';
import {
  FLOW_COOKIE,
  FLOW_MAX_AGE_SECONDS,
  getAskedAccount,
  isCallback,
  isSignInStart,
  sealFlow,
} from './login-flow';

/**
 * Auth.js's routes, with the account of a sign-in bound to its flow: sealed when the
 * sign-in starts, and forgotten once its callback is done (login-flow.ts).
 */
export async function POST(req: NextRequest): Promise<Response> {
  const res = await handlePost(req);
  if (!isSignInStart(req)) {
    return res;
  }
  // A sign-in for no account seals nothing, and ends any flow sealed before it: the
  // callback must not use the account of a sign-in that was abandoned.
  const account = await getAskedAccount(req);
  const out = new Response(res.body, res);
  out.headers.append(
    'Set-Cookie',
    account
      ? cookie(req, FLOW_COOKIE, await sealFlow(account), FLOW_MAX_AGE_SECONDS)
      : cookie(req, FLOW_COOKIE, '', 0)
  );
  return out;
}

export async function GET(req: NextRequest): Promise<Response> {
  const res = await handleGet(req);
  if (!isCallback(req)) {
    return res;
  }
  // Done, or failed: either way the flow is over, and the account it was for no longer
  // steers any sign-in.
  const out = new Response(res.body, res);
  out.headers.append('Set-Cookie', cookie(req, FLOW_COOKIE, '', 0));
  out.headers.append('Set-Cookie', cookie(req, ACCOUNT_COOKIE, '', 0));
  return out;
}

function cookie(
  req: NextRequest,
  name: string,
  value: string,
  maxAge: number
): string {
  const parts = [
    `${name}=${value}`,
    'Path=/',
    `Max-Age=${maxAge}`,
    'HttpOnly',
    // The callback comes back from the provider as a top-level navigation.
    'SameSite=Lax',
  ];
  if (getPublicBaseUrl(req).protocol === 'https:') {
    parts.push('Secure');
  }
  return parts.join('; ');
}
