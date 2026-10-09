// How the Usage pages write a number: every count of the two pages goes through here,
// so that they all follow the languages of the browser.

// What a number is written in where there is no browser to ask.
const SERVER_LOCALE = 'en-US';

// The languages the viewer reads, the preferred one first. The server has no viewer:
// it writes in one fixed language, whatever the machine it runs on. The numbers of the
// pages come from a read made in the browser, so none is written on the server first
// and then differently in the browser.
function viewerLocales(): string[] {
  return typeof window === 'undefined'
    ? [SERVER_LOCALE]
    : [...navigator.languages];
}

// A formatter of numbers in the language of the viewer, with the options given.
export function countFormat(
  options?: Intl.NumberFormatOptions
): Intl.NumberFormat {
  return new Intl.NumberFormat(viewerLocales(), options);
}

// A count with its digits grouped. A bigint keeps every digit.
export function formatCount(value: bigint | number): string {
  return countFormat().format(value);
}
