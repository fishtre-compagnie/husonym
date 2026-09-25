import { NextRequest } from 'next/server';
import { isSecureRequest } from './account-provider';
import { GET as handleGet, POST as handlePost } from './auth';
import {
  FLOW_HEADER,
  FLOW_MAX_AGE_SECONDS,
  flowCookieName,
  getAskedAccount,
  getCallbackAccount,
  hasFlowCookie,
  isCallback,
  isSignInStart,
  sealForFlow,
  sealForHeader,
} from './login-flow';

/**
 * Auth.js's routes, with the account of a sign-in bound to its flow (login-flow.ts).
 */
export async function POST(req: NextRequest): Promise<Response> {
  if (!isSignInStart(req)) {
    return handlePost(req);
  }
  // The account is resolved once, here, from the body Auth.js checks the CSRF token of,
  // and handed to the configuration sealed; whatever the browser sent under that header is
  // dropped.
  const body = await req.text();
  const account = await getAskedAccount(new URLSearchParams(body));
  const headers = new Headers(req.headers);
  headers.delete(FLOW_HEADER);
  if (account) {
    headers.set(FLOW_HEADER, await sealForHeader(account));
  }
  // Rebuilt from its parts: Next.js's own request object does not survive being copied.
  const res = await handlePost(
    new NextRequest(req.url, {
      method: req.method,
      headers,
      body,
    })
  );

  // Only a flow Auth.js did start -- a CSRF-checked POST sending the browser to the
  // provider -- is sealed, or ends the one sealed before it: a refused request touches
  // nothing.
  const authorization = await getAuthorizationUrl(res);
  if (!authorization) {
    return res;
  }
  const state = authorization.searchParams.get('state');
  const out = new Response(res.body, res);
  out.headers.append(
    'Set-Cookie',
    account && state
      ? cookie(
          req,
          flowCookieName(req),
          await sealForFlow(account, state),
          FLOW_MAX_AGE_SECONDS
        )
      : // A sign-in for no account ends any flow sealed before it. An account's provider
        // sending no state gets no seal either: Auth.js requires the state of it, so its
        // flow fails at the callback anyway.
        cookie(req, flowCookieName(req), '', 0)
  );
  return out;
}

export async function GET(req: NextRequest): Promise<Response> {
  const res = await handleGet(req);
  if (!isCallback(req)) {
    return res;
  }
  // The flow this callback ends is over. A callback for another flow -- one anybody can
  // make the browser request -- leaves the sealed flow alone.
  if (!hasFlowCookie(req) || !(await getCallbackAccount(req))) {
    return res;
  }
  const out = new Response(res.body, res);
  out.headers.append('Set-Cookie', cookie(req, flowCookieName(req), '', 0));
  return out;
}

/**
 * The authorization URL a sign-in response sends the browser to, if it started a flow: as
 * a redirect, or as the JSON the client-side signIn() asks for.
 */
async function getAuthorizationUrl(res: Response): Promise<URL | null> {
  let target = res.headers.get('Location');
  if (!target && res.headers.get('Content-Type')?.includes('json')) {
    try {
      target = ((await res.clone().json()) as { url?: string }).url ?? null;
    } catch {
      target = null;
    }
  }
  if (!target) {
    return null;
  }
  try {
    const url = new URL(target);
    return url.searchParams.get('response_type') === 'code' ? url : null;
  } catch {
    return null;
  }
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
  if (isSecureRequest(req)) {
    parts.push('Secure');
  }
  return parts.join('; ');
}
