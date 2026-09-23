import { type TokenSet } from '@auth/core/types';
import { addSeconds, isAfter } from 'date-fns';
import NextAuth, { NextAuthConfig } from 'next-auth';
import { NextRequest } from 'next/server';
import {
  AccountLoginMethod,
  PROVIDER_ID,
  fetchAccountLoginMethod,
  getAccountSlug,
} from './account-provider';

function getProviders(
  accountMethod: AccountLoginMethod | null
): NextAuthConfig['providers'] {
  const providers: NextAuthConfig['providers'] = [];
  const authConfig = getOAuthConfig(accountMethod);
  if (authConfig) {
    providers.push({
      id: authConfig.id,
      name: authConfig.name,
      type: authConfig.type,
      issuer: authConfig.expectedissuer,
      clientId: authConfig.clientId,
      clientSecret: authConfig.clientSecret ?? '',
      authorization: {
        url: authConfig.authorizeUrl,
        params: {
          audience: authConfig.audience,
          scope: authConfig.scope,
        },
      },
      userinfo: authConfig.userInfoUrl,
      token: authConfig.tokenUrl,

      wellKnown: getWellKnown(authConfig.issuer),
    });
  }

  return providers;
}

function getWellKnown(issuerUrl: string): string {
  return `${trimEnd(issuerUrl, '/')}/.well-known/openid-configuration`;
}

function trimEnd(val: string, chars: string): string {
  return val.endsWith(chars)
    ? val.substring(0, val.length - chars.length)
    : val;
}

interface OAuthConfig {
  id: string;
  name: string;
  type: 'oidc';

  issuer: string;
  expectedissuer: string;

  authorizeUrl?: string;
  userInfoUrl?: string;
  tokenUrl?: string;
  logoutUrl?: string;

  clientId: string;
  clientSecret?: string;
  audience: string;
  scope: string;
}

export async function getLogoutUrl(): Promise<string | undefined> {
  const oauthconfig = getOAuthConfig(null);
  if (!oauthconfig) {
    console.warn('there is no oauthconfig defined, unable to find logout url');
    return undefined;
  }
  if (oauthconfig.logoutUrl) {
    return oauthconfig.logoutUrl;
  }
  const oidcConfig = await getOpenIdConfiguration(oauthconfig.issuer);
  if (oidcConfig.end_session_endpoint) {
    return oidcConfig.end_session_endpoint;
  }
  console.warn(
    'oidc configuration well known did not provide an end session endpoint'
  );
  return undefined;
}

/**
 * Builds the provider, from the account's declaration when there is one and from the
 * deployment's variables otherwise.
 *
 * An account that declares a provider replaces the issuer and the client, and nothing
 * else: the id stays fixed (one redirect URI for the deployment -- see account-provider),
 * the scope and audience stay the deployment's, and the endpoints stay discovered rather
 * than configured, because an account has no way to override them and should not.
 */
function getOAuthConfig(
  accountMethod: AccountLoginMethod | null
): OAuthConfig | null {
  const issuer = accountMethod?.issuer ?? process.env.AUTH_ISSUER;
  const clientId = accountMethod?.clientId ?? process.env.AUTH_CLIENT_ID;
  // Never the deployment's secret against an account's provider: that would send this
  // deployment's client secret to a server the account chose.
  const clientSecret = accountMethod
    ? undefined
    : process.env.AUTH_CLIENT_SECRET;
  const audience = process.env.AUTH_AUDIENCE;
  const scope = process.env.AUTH_SCOPE;
  if (!issuer || !clientId || !audience || !scope) {
    return null;
  }

  const id = PROVIDER_ID;
  const name = process.env.AUTH_PROVIDER_NAME ?? 'unknown';
  const expectedissuer = accountMethod
    ? issuer
    : process.env.AUTH_EXPECTED_ISSUER || issuer;

  return {
    id,
    name,
    type: 'oidc',

    issuer,
    expectedissuer,
    clientId,
    clientSecret,
    audience,
    scope,

    // Endpoint overrides are a deployment's business. An account declares an issuer and
    // lets discovery do the rest, which is what "any compliant provider" means.
    authorizeUrl: accountMethod ? undefined : process.env.AUTH_AUTHORIZE_URL,
    userInfoUrl: accountMethod ? undefined : process.env.AUTH_USERINFO_URL,
    logoutUrl: accountMethod ? undefined : process.env.AUTH_LOGOUT_URL,
    tokenUrl: accountMethod ? undefined : process.env.AUTH_TOKEN_URL,
  };
}

/**
 * The configuration is a function of the request, which is what Auth.js v5 accepts and
 * what makes a provider per account possible without forking anything.
 *
 * `request` is undefined outside a request -- `auth()` in a server component, middleware
 * -- and that has to yield a valid configuration rather than throw: the absence of an
 * account is the deployment's own provider, which is the normal case.
 */
export const {
  handlers: { GET, POST },
  // auth function meant to be used in RSC or middleware.
  auth,
} = NextAuth(async (request: NextRequest | undefined) => {
  const slug = getAccountSlug(request);
  const accountMethod = slug ? await fetchAccountLoginMethod(slug) : null;
  return buildConfig(accountMethod);
});

function buildConfig(accountMethod: AccountLoginMethod | null): NextAuthConfig {
  return {
    providers: getProviders(accountMethod),
    session: { strategy: 'jwt' },
    callbacks: {
      session: async ({ session, token }) => {
        session.accessToken = (token as any).accessToken; // eslint-disable-line @typescript-eslint/no-explicit-any
        session.idToken = (token as any).idToken; // eslint-disable-line @typescript-eslint/no-explicit-any
        return session;
      },
      jwt: async ({ token, account }) => {
        // Persist the OAuth access_token and or the user id to the token right after signin
        if (account) {
          token.idToken = account.id_token;
          token.accessToken = account.access_token;
          token.refreshToken = account.refresh_token;
          token.expiresAt = account.expires_at;
          token.provider = account.provider;
        }
        if (
          !token.expiresAt ||
          // Both times must be in the same format
          isAfter(new Date(), new Date((token as any).expiresAt * 1000)) // eslint-disable-line @typescript-eslint/no-explicit-any
        ) {
          // refresh token
          if (!token.refreshToken) {
            // token can't be refreshed, fail
            throw new Error('session is expired, no refresh token available');
          }

          const oauthConfig = getOAuthConfig(null);
          if (!oauthConfig) {
            throw new Error('unable to find provider to refresh token');
          }
          try {
            const response = await fetch(
              await getTokenUrl(oauthConfig.issuer),
              {
                headers: {
                  'Content-Type': 'application/x-www-form-urlencoded',
                },
                body: new URLSearchParams({
                  client_id: oauthConfig.clientId,
                  client_secret: oauthConfig.clientSecret ?? '',
                  grant_type: 'refresh_token',
                  refresh_token: (token as any).refreshToken, // eslint-disable-line @typescript-eslint/no-explicit-any
                }),
                method: 'POST',
              }
            );
            const tokens: TokenSet = await response.json();
            if (!response.ok) {
              throw tokens;
            }
            token.accessToken = tokens.access_token;
            // the refresh token may not always be returned. If it's not, don't update
            if (tokens.refresh_token) {
              token.refreshToken = tokens.refresh_token;
            }
            if (tokens.expires_at) {
              token.expiresAt = tokens.expires_at;
            } else if (tokens.expires_in) {
              token.expiresAt = Math.floor(
                addSeconds(new Date(), tokens.expires_in).getTime() / 1000
              );
            }
          } catch (err) {
            console.error('failed to refresh token', err);
            throw err;
          }
        }
        return token;
      },
    },
  };
}

interface OidcConfiguration {
  issuer: string;
  authorization_endpoint: string;
  token_endpoint: string;
  userinfo_endpoint: string;
  end_session_endpoint?: string;
  jwks_uri: string;
}

async function getTokenUrl(issuer: string): Promise<string> {
  try {
    const oidcConfig = await getOpenIdConfiguration(issuer);
    if (!oidcConfig.token_endpoint) {
      throw new Error('unable to find token endpoint');
    }
    return oidcConfig.token_endpoint;
  } catch (err) {
    throw err;
  }
}

async function getOpenIdConfiguration(
  issuer: string
): Promise<Partial<OidcConfiguration>> {
  const wellKnownUrl = getWellKnown(issuer);
  const res = await fetch(wellKnownUrl, {
    method: 'GET',
    headers: { 'Content-Type': 'application/json' },
  });
  return (await res.json()) as OidcConfiguration;
}

declare module 'next-auth' {
  export interface Session {
    accessToken: string;
    idToken: string;
  }
}

// This isn't currently working, guessing because next-auth has its own local dependency of @auth/core due to mismatched versions
// declare module '@auth/core/jwt' {
//   export interface JWT {
//     refreshToken?: string;
//     expiresAt?: number;
//     accessToken?: string;
//   }
// }
