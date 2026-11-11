export function shouldHighlightPathName(
  href: string,
  pathname: string
): boolean {
  if (href === '/' && pathname === '/') {
    return true;
  }
  return href !== '/' && pathname.includes(href);
}
