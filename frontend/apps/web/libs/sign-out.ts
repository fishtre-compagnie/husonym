import { signOut } from 'next-auth/react';

/**
 * Signs out of the app and of the provider the session came from, then comes back to
 * `returnTo` (a path of this app) when given.
 *
 * The provider's logout is asked for first, while the session still says which provider
 * issued it; the session is then ended here, and the browser sent to end it there too.
 * The provider sends it back to the app's root, with `returnTo` as the logout's state.
 */
export async function signOutEverywhere(returnTo?: string): Promise<void> {
  const query = returnTo ? `?${new URLSearchParams({ returnTo })}` : '';
  const res = await fetch(`/api/auth/provider-sign-out${query}`);
  const { url } = (await res.json()) as { url: string | null };
  if (url) {
    await signOut({ redirect: false });
    window.location.href = url;
  } else {
    await signOut(returnTo ? { callbackUrl: returnTo } : undefined);
  }
}
