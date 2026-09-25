import {
  fetchAccountLoginMethod,
  sanitizeSlug,
} from '@/app/api/auth/[...nextauth]/account-provider';
import { getSafePath } from '@/libs/safe-path';
import { getSingleOrUndefined } from '@/libs/utils';
import { notFound, redirect } from 'next/navigation';
import { ReactElement } from 'react';
import AccountLogin from './AccountLogin';

interface Props {
  params: Promise<{ slug: string }>;
  searchParams: Promise<Record<string, string | string[] | undefined>>;
}

/**
 * Where the per-account link leads: which provider the account signs in with, before
 * anything starts. Anyone can send a link to it, so the page names the provider rather
 * than sending straight to it, asks before replacing a session already open, and signs in
 * with the account it shows -- the click names it (login-flow.ts).
 */
export default async function AccountLoginPage(
  props: Props
): Promise<ReactElement> {
  const slug = sanitizeSlug((await props.params).slug);
  if (!slug) {
    notFound();
  }
  const callbackPath = getSafePath(
    getSingleOrUndefined((await props.searchParams).callbackUrl)
  );
  // Without authentication there is nobody to sign in: straight on.
  if (process.env.AUTH_ENABLED !== 'true') {
    redirect(callbackPath ?? '/');
  }

  const method = await fetchAccountLoginMethod(slug);
  return (
    <AccountLogin
      slug={slug}
      provider={getProviderLabel(method?.issuer)}
      ownProvider={!!method}
      callbackPath={callbackPath}
    />
  );
}

/**
 * How the page names the provider: the issuer of the account's, host and path -- two
 * realms of one server differ only by their path -- or the deployment's name.
 */
function getProviderLabel(issuer: string | undefined): string {
  const deployment =
    process.env.AUTH_PROVIDER_NAME ?? 'the deployment provider';
  if (!issuer) {
    return deployment;
  }
  try {
    const url = new URL(issuer);
    return `${url.host}${url.pathname.replace(/\/+$/, '')}`;
  } catch {
    return deployment;
  }
}
