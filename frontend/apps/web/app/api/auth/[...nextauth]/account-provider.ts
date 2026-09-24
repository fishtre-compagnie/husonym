import { NextRequest } from 'next/server';

/**
 * Which account a sign-in is for, and the provider it uses.
 *
 * The tenant has to be known before the flow starts: an OIDC flow begins with the client
 * id of the right connector, and at that point nobody has proved anything. The account is
 * therefore designated by the link that was followed -- the deterministic answer, and the
 * only one that asks nobody to prove ownership of an email domain.
 */

/**
 * The account a sign-in is asked for: set by the per-account link, read when the sign-in
 * starts. The callback does not read it -- anyone can make a browser follow the link and
 * rewrite it -- but the flow cookie sealed at the start (login-flow.ts).
 *
 * It holds a slug, which is public, never anything about a person.
 */
export const ACCOUNT_COOKIE = 'husonym.login-account';

export interface AccountLoginMethod {
  issuer: string;
  clientId: string;
}

/**
 * The provider id is deliberately the SAME for every account, and this is the decision the
 * rest depends on.
 *
 * Auth.js routes callbacks to `/api/auth/callback/<providerId>`, so an id that varied by
 * account would mean a different redirect URI per account -- and every account having to
 * register its own with its provider. One id, with the issuer and client varying behind
 * it, means one redirect URI for the whole deployment: the thing an operator registers
 * once and never touches again.
 *
 * It is the deployment's AUTH_PROVIDER_ID, as it was before accounts could bring their own
 * provider: a deployment keeps the redirect URI it registered, and the sign-in the app
 * starts (`signInProviderId`) names the provider that exists.
 */
export function getProviderId(): string {
  return process.env.AUTH_PROVIDER_ID || 'oidc';
}

/**
 * A slug reaches this from a URL or a cookie, and is about to be put in a request to the
 * backend. Constraining its shape here means nothing further down has to wonder.
 */
export function sanitizeSlug(value: string): string | null {
  const trimmed = value.trim();
  if (!/^[a-zA-Z0-9][a-zA-Z0-9_-]{0,99}$/.test(trimmed)) {
    return null;
  }
  return trimmed;
}

/**
 * Asks the backend which provider an account signs in with.
 *
 * Returns null for an account that has declared none, which is also the answer for a slug
 * the backend does not recognize — the caller then falls back on the deployment's own
 * provider. The two cases are deliberately indistinguishable: this runs before anybody has
 * authenticated, so it must not become a way to find out what exists.
 */
export async function fetchAccountLoginMethod(
  slug: string
): Promise<AccountLoginMethod | null> {
  const baseUrl = process.env.HUSONYM_API_BASE_URL;
  if (!baseUrl) {
    return null;
  }

  try {
    const res = await fetch(
      `${trimEnd(baseUrl, '/')}/mgmt.v1alpha1.AuthService/GetAccountLoginMethod`,
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ accountSlug: slug }),
        cache: 'no-store',
        // A sign-in waits on this: a backend that does not answer must not hold it.
        signal: AbortSignal.timeout(LOOKUP_TIMEOUT_MS),
      }
    );
    if (!res.ok) {
      return null;
    }
    const body = (await res.json()) as Partial<AccountLoginMethod>;
    if (!body.issuer || !body.clientId) {
      return null;
    }
    return { issuer: body.issuer, clientId: body.clientId };
  } catch (err) {
    // A sign-in must not fail because this lookup did: falling back on the deployment's
    // provider is what an account with no setting does anyway.
    console.warn('unable to resolve the account login method', err);
    return null;
  }
}

// How long the backend may take to say which provider an account uses.
const LOOKUP_TIMEOUT_MS = 5_000;

function trimEnd(val: string, chars: string): string {
  return val.endsWith(chars)
    ? val.substring(0, val.length - chars.length)
    : val;
}

/**
 * The deployment's public URL, as Auth.js reads it: AUTH_URL or NEXTAUTH_URL (an empty one
 * is unset), else -- when the deployment trusts its proxy (AUTH_TRUST_HOST) -- what the
 * proxy forwarded, else the request's own; behind a proxy, a request may carry an internal
 * origin.
 */
export function getPublicBaseUrl(req: NextRequest): URL {
  const configured = process.env.AUTH_URL || process.env.NEXTAUTH_URL;
  if (configured) {
    return new URL(configured);
  }
  const url = new URL(req.nextUrl.origin);
  if (process.env.AUTH_TRUST_HOST === 'true') {
    const proto = req.headers.get('x-forwarded-proto')?.split(',')[0].trim();
    const host = req.headers.get('x-forwarded-host')?.split(',')[0].trim();
    if (proto === 'https' || proto === 'http') {
      url.protocol = `${proto}:`;
    }
    if (host) {
      url.host = host;
    }
  }
  return url;
}

// isSecureRequest tells whether the deployment is reached over https, for its cookies.
export function isSecureRequest(req: NextRequest): boolean {
  return getPublicBaseUrl(req).protocol === 'https:';
}
