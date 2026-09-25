import { signOut } from 'next-auth/react';

/**
 * Signs out of the app and of the provider the session came from.
 *
 * The provider's logout is asked for first, while the session still says which provider
 * issued it; the session is then ended here, and the browser sent to end it there too.
 */
export async function signOutEverywhere(): Promise<void> {
  const res = await fetch('/api/auth/provider-sign-out');
  const { url } = (await res.json()) as { url: string | null };
  if (url) {
    await signOut({ redirect: false });
    window.location.href = url;
  } else {
    await signOut();
  }
}
