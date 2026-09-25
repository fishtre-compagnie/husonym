/**
 * A path of this app to go to, or null: only a path, never another origin, so that a link
 * cannot send whoever follows it elsewhere. The path is checked once normalized: `/.//x`
 * or `/a/..//x` come out as `//x`, which a browser takes for another host.
 */
export function getSafePath(value: string | null | undefined): string | null {
  if (!value || !value.startsWith('/')) {
    return null;
  }
  const base = new URL('http://app.invalid');
  try {
    const url = new URL(value, base);
    const path = `${url.pathname}${url.search}${url.hash}`;
    if (
      url.origin !== base.origin ||
      path.startsWith('//') ||
      path.startsWith('/\\')
    ) {
      return null;
    }
    return path;
  } catch {
    return null;
  }
}
