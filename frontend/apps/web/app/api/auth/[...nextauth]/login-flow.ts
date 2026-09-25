import { NextRequest } from 'next/server';
import {
  ACCOUNT_COOKIE,
  AccountLoginMethod,
  fetchAccountLoginMethod,
  isSecureRequest,
  sanitizeSlug,
} from './account-provider';

/**
 * The account a sign-in in progress is for, bound to that flow.
 *
 * The account a sign-in starts with is read from a cookie the per-account link sets, and
 * anyone can make a browser follow that link: rewritten between the start of a flow and
 * its callback, it made the callback exchange the code with another account's provider.
 * So the account is read once, when the sign-in starts, and sealed -- signed with the
 * deployment's secret -- with the `state` of the flow Auth.js started, for that flow's
 * callback to use and nothing else. A seal set in the browser by someone else, for their
 * own account, does not carry the state of the victim's flow.
 */
const FLOW_COOKIE = 'husonym.login-flow';

// As long as the account cookie: a sign-in with a detour through a second factor.
export const FLOW_MAX_AGE_SECONDS = 15 * 60;

/**
 * The header the route hands the account of a starting sign-in to Auth.js's configuration
 * with, sealed: resolved once, and never taken from the browser, which the route strips it
 * from.
 */
export const FLOW_HEADER = 'x-husonym-login-account';

// Where Auth.js answers.
const AUTH_BASE_PATH = '/api/auth';

// What each seal is for, so that one cannot be taken for the other or for another use of
// the secret.
const COOKIE_PURPOSE = 'husonym.login-flow.cookie.v1';
const HEADER_PURPOSE = 'husonym.login-flow.header.v1';

export interface FlowAccount extends AccountLoginMethod {
  slug: string;
}

interface FlowPayload extends FlowAccount {
  // When the seal expires, in seconds since the epoch.
  exp: number;
  // The digest of the state of the flow the seal is for; cookie seals only.
  state?: string;
}

/**
 * The name of the flow cookie: with the __Host- prefix over https, so that no sibling
 * domain may set it.
 */
export function flowCookieName(req: NextRequest): string {
  return isSecureRequest(req) ? `__Host-${FLOW_COOKIE}` : FLOW_COOKIE;
}

export function isSignInStart(req: NextRequest): boolean {
  return (
    req.method === 'POST' &&
    req.nextUrl.pathname.startsWith(`${AUTH_BASE_PATH}/signin/`)
  );
}

export function isCallback(req: NextRequest): boolean {
  return req.nextUrl.pathname.startsWith(`${AUTH_BASE_PATH}/callback/`);
}

/**
 * The account whose provider a request to Auth.js is made with: the one the route resolved
 * when a sign-in starts; the one sealed for this flow, on its callback; the deployment's
 * (null) everywhere else. A session is refreshed with the provider its token names
 * (auth.ts), whatever the request.
 */
export async function getRequestAccount(
  req: NextRequest | undefined
): Promise<FlowAccount | null> {
  if (!req) {
    return null;
  }
  if (isCallback(req)) {
    return getCallbackAccount(req);
  }
  if (isSignInStart(req)) {
    const sealed = await open(
      req.headers.get(FLOW_HEADER) ?? undefined,
      HEADER_PURPOSE
    );
    return sealed ? toAccount(sealed) : null;
  }
  return null;
}

/**
 * The account sealed for the flow a callback ends, or null when there is none, or when the
 * seal is for another flow than the one the callback's state names.
 */
export async function getCallbackAccount(
  req: NextRequest
): Promise<FlowAccount | null> {
  const sealed = await open(
    req.cookies.get(flowCookieName(req))?.value,
    COOKIE_PURPOSE
  );
  const state = req.nextUrl.searchParams.get('state');
  if (!sealed?.state || !state) {
    return null;
  }
  return sealed.state === (await digest(state)) ? toAccount(sealed) : null;
}

/** Whether the request carries a flow seal at all, whatever flow it is for. */
export function hasFlowCookie(req: NextRequest): boolean {
  return !!req.cookies.get(flowCookieName(req))?.value;
}

// getAskedAccount is the account the per-account link asked a sign-in for, if any.
export async function getAskedAccount(
  req: NextRequest
): Promise<FlowAccount | null> {
  const slug = sanitizeSlug(req.cookies.get(ACCOUNT_COOKIE)?.value ?? '');
  if (!slug) {
    return null;
  }
  const method = await fetchAccountLoginMethod(slug);
  return method ? { ...method, slug } : null;
}

/** The account, sealed for the route to hand to Auth.js's configuration. */
export async function sealForHeader(account: FlowAccount): Promise<string> {
  return seal({ ...account }, HEADER_PURPOSE);
}

/** The account, sealed for the flow whose state is given. */
export async function sealForFlow(
  account: FlowAccount,
  state: string,
  now: number = Date.now()
): Promise<string> {
  return seal({ ...account, state: await digest(state) }, COOKIE_PURPOSE, now);
}

function toAccount(payload: FlowPayload): FlowAccount {
  return {
    slug: payload.slug,
    issuer: payload.issuer,
    clientId: payload.clientId,
  };
}

async function seal(
  fields: Omit<FlowPayload, 'exp'>,
  purpose: string,
  now: number = Date.now()
): Promise<string> {
  const payload: FlowPayload = {
    ...fields,
    exp: Math.floor(now / 1000) + FLOW_MAX_AGE_SECONDS,
  };
  const body = toBase64Url(new TextEncoder().encode(JSON.stringify(payload)));
  return `${body}.${await sign(body, purpose)}`;
}

/**
 * What a seal holds, or null when it is absent, altered, expired, sealed for another
 * purpose or under another secret.
 */
async function open(
  sealed: string | undefined,
  purpose: string,
  now: number = Date.now()
): Promise<FlowPayload | null> {
  if (!sealed) {
    return null;
  }
  const [body, signature, ...rest] = sealed.split('.');
  if (!body || !signature || rest.length > 0) {
    return null;
  }
  if (!(await verify(body, signature, purpose))) {
    return null;
  }
  try {
    const payload = JSON.parse(
      new TextDecoder().decode(fromBase64Url(body))
    ) as Partial<FlowPayload>;
    if (
      typeof payload.slug !== 'string' ||
      typeof payload.issuer !== 'string' ||
      typeof payload.clientId !== 'string' ||
      typeof payload.exp !== 'number' ||
      (payload.state !== undefined && typeof payload.state !== 'string') ||
      payload.exp < Math.floor(now / 1000)
    ) {
      return null;
    }
    return payload as FlowPayload;
  } catch {
    return null;
  }
}

async function digest(value: string): Promise<string> {
  const hash = await crypto.subtle.digest(
    'SHA-256',
    new TextEncoder().encode(value)
  );
  return toBase64Url(new Uint8Array(hash));
}

async function key(): Promise<CryptoKey> {
  // The secret Auth.js signs with, read the way it reads it.
  const secret =
    process.env.AUTH_SECRET ??
    process.env.NEXTAUTH_SECRET ??
    process.env.AUTH_SECRET_1;
  if (!secret) {
    throw new Error('no secret to sign the sign-in flow with');
  }
  return crypto.subtle.importKey(
    'raw',
    new TextEncoder().encode(secret),
    { name: 'HMAC', hash: 'SHA-256' },
    false,
    ['sign', 'verify']
  );
}

async function sign(body: string, purpose: string): Promise<string> {
  const signature = await crypto.subtle.sign(
    'HMAC',
    await key(),
    new TextEncoder().encode(`${purpose}.${body}`)
  );
  return toBase64Url(new Uint8Array(signature));
}

async function verify(
  body: string,
  signature: string,
  purpose: string
): Promise<boolean> {
  try {
    return await crypto.subtle.verify(
      'HMAC',
      await key(),
      fromBase64Url(signature),
      new TextEncoder().encode(`${purpose}.${body}`)
    );
  } catch {
    return false;
  }
}

// base64url without Buffer, which the edge runtime the middleware runs in lacks.
function toBase64Url(bytes: Uint8Array): string {
  let binary = '';
  bytes.forEach((b) => {
    binary += String.fromCharCode(b);
  });
  return btoa(binary)
    .replace(/\+/g, '-')
    .replace(/\//g, '_')
    .replace(/=+$/, '');
}

function fromBase64Url(value: string): Uint8Array<ArrayBuffer> {
  const binary = atob(value.replace(/-/g, '+').replace(/_/g, '/'));
  const out = new Uint8Array(new ArrayBuffer(binary.length));
  for (let i = 0; i < binary.length; i++) {
    out[i] = binary.charCodeAt(i);
  }
  return out;
}
