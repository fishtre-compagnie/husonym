import { shouldHighlightPathName } from '../site-header/util';

export type NavKey =
  | 'dashboard'
  | 'jobs'
  | 'runs'
  | 'connections'
  | 'transformers'
  | 'review'
  | 'settings';

/** The account's path to a section: until an account is known, the root. */
export function accountPath(accountName: string | undefined, path: string) {
  return accountName ? `/${accountName}${path}` : '/';
}

/**
 * Which section the pathname is in, with the highlighting the old top nav had: a
 * section is active when the pathname holds its marker ("/job" covers the jobs and
 * the job wizard). A job's review page belongs to the review, not to the jobs.
 */
export function activeNav(
  pathname: string,
  accountName: string | undefined
): NavKey | undefined {
  if (accountName && pathname === `/${accountName}`) {
    return 'dashboard';
  }
  if (shouldHighlightPathName('/review', pathname)) {
    return 'review';
  }
  const markers: [NavKey, string][] = [
    ['settings', '/settings'],
    ['jobs', '/job'],
    ['runs', '/run'],
    ['transformers', '/transformer'],
    ['connections', 'connection'],
  ];
  return markers.find(([, m]) => shouldHighlightPathName(m, pathname))?.[0];
}
