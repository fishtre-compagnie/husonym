import { LookupAddress, lookup } from 'node:dns';
import { isIP } from 'node:net';
import { Agent, fetch as undiciFetch } from 'undici';
import { isPublicAddress } from './public-address';

/**
 * The fetch this server reaches an account's identity provider with.
 *
 * The provider's address is chosen by whoever administers the account, and this server
 * fetches what it publishes: discovery, token, userinfo, logout. Unbounded, the account
 * would choose where the deployment sends requests -- to itself, its network, or the
 * cloud's metadata service. So an account's provider is reached over https, at addresses
 * the internet routes to, checked when the connection is made: for every hop, and every
 * address a name resolves to. The backend holds the same bound (safehttp), and refuses to
 * save a provider out of it. AUTH_ACCOUNT_ISSUER_ALLOW_PRIVATE lifts it, for a deployment
 * whose providers live on its own network, or development.
 */
export async function accountFetch(
  input: string | URL | Request,
  init?: RequestInit
): Promise<Response> {
  if (isPrivateAllowed()) {
    return fetch(input, init);
  }
  let url = new URL(input instanceof Request ? input.url : input.toString());
  const request: RequestInit =
    input instanceof Request ? { ...toInit(input), ...init } : { ...init };
  // A caller that follows no redirect -- oauth4webapi, for the token and userinfo, where
  // a redirect is an error -- gets the response as it is. Otherwise only a request
  // without a body is followed, as a GET, and without its credentials when the redirect
  // leaves the origin.
  const follows =
    request.redirect !== 'manual' &&
    request.redirect !== 'error' &&
    ['GET', 'HEAD'].includes((request.method ?? 'GET').toUpperCase());
  for (let hop = 0; hop <= MAX_REDIRECTS; hop++) {
    checkUrl(url);
    const res = (await undiciFetch(url, {
      ...(request as object),
      redirect: 'manual',
      dispatcher: publicOnly,
    })) as unknown as Response;
    const location = res.headers.get('location');
    if (!follows || res.status < 300 || res.status >= 400 || !location) {
      return res;
    }
    // Each hop is checked again: a redirect is a request of its own.
    await res.body?.cancel();
    const next = new URL(location, url);
    if (next.origin !== url.origin) {
      const headers = new Headers(request.headers);
      headers.delete('authorization');
      headers.delete('cookie');
      request.headers = headers;
    }
    url = next;
  }
  throw new Error(`stopped after ${MAX_REDIRECTS} redirects`);
}

// What a provider needs, plus room for the http-to-https and trailing-slash hops that
// real deployments have.
const MAX_REDIRECTS = 5;

// Read the way the backend reads it (Go's strconv.ParseBool), so that both hold the same
// bound.
function isPrivateAllowed(): boolean {
  return ['1', 't', 'T', 'TRUE', 'true', 'True'].includes(
    process.env.AUTH_ACCOUNT_ISSUER_ALLOW_PRIVATE ?? ''
  );
}

function toInit(req: Request): RequestInit {
  return {
    method: req.method,
    headers: req.headers,
    body: req.body,
    // A streamed body needs it, and undici refuses a stream without it.
    ...({ duplex: 'half' } as object),
  };
}

function checkUrl(url: URL): void {
  if (url.protocol !== 'https:') {
    throw new Error(
      `an account's identity provider is reached over https only: ${url.origin}`
    );
  }
  // A literal address is not looked up: it is checked here.
  const host = url.hostname.replace(/^\[|\]$/g, '');
  if (isIP(host) && !isPublicAddress(host)) {
    throw new Error(`the address is not on the public internet: ${host}`);
  }
}

// Resolves a name the way the connection would, and refuses it unless every address it
// resolves to is public: the check holds for the address actually connected to.
type LookupCallback = (
  err: NodeJS.ErrnoException | null,
  address: string | LookupAddress[],
  family?: number
) => void;

function publicLookup(
  hostname: string,
  options: { all?: boolean; family?: number },
  callback: LookupCallback
): void {
  lookup(hostname, { ...options, all: true }, (err, addresses) => {
    if (err) {
      callback(err, []);
      return;
    }
    const blocked = addresses.find((a) => !isPublicAddress(a.address));
    if (blocked) {
      callback(
        new Error(
          `the address is not on the public internet: ${hostname} resolves to ${blocked.address}`
        ),
        []
      );
      return;
    }
    if (options.all) {
      callback(null, addresses);
    } else {
      callback(null, addresses[0].address, addresses[0].family);
    }
  });
}

const publicOnly = new Agent({ connect: { lookup: publicLookup as never } });
