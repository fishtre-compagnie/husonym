import {
  ACCOUNT_COOKIE,
  fetchAccountLoginMethod,
  getSafeCallbackPath,
  sanitizeSlug,
} from '@/app/api/auth/[...nextauth]/account-provider';
import { getSingleOrUndefined } from '@/libs/utils';
import { cookies } from 'next/headers';
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
 * than sending straight to it, and asks before replacing a session already open.
 */
export default async function AccountLoginPage(
  props: Props
): Promise<ReactElement> {
  const slug = sanitizeSlug((await props.params).slug);
  if (!slug) {
    notFound();
  }
  const searchParams = await props.searchParams;
  const callbackPath = getSafeCallbackPath(
    getSingleOrUndefined(searchParams.callbackUrl)
  );

  // The sign-in reads the account from the cookie the link sets: reached some other way,
  // the page goes through the link once.
  const jar = await cookies();
  if (
    jar.get(ACCOUNT_COOKIE)?.value !== slug &&
    getSingleOrUndefined(searchParams.set) !== '1'
  ) {
    const link = new URLSearchParams();
    if (callbackPath) {
      link.set('callbackUrl', callbackPath);
    }
    redirect(`/api/auth/account/${slug}?${link.toString()}`);
  }

  const method = await fetchAccountLoginMethod(slug);
  return (
    <AccountLogin
      slug={slug}
      provider={
        method
          ? new URL(method.issuer).host
          : (process.env.AUTH_PROVIDER_NAME ?? 'the deployment provider')
      }
      ownProvider={!!method}
      callbackPath={callbackPath}
    />
  );
}
