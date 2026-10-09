export function shortNumberFormatter(
  formatter: Intl.NumberFormat,
  value: number
): string {
  if (Math.abs(value) >= 1_000_000_000_000) {
    return formatter.format(value / 1_000_000_000_000) + 'T';
  } else if (Math.abs(value) >= 1_000_000_000) {
    return formatter.format(value / 1_000_000_000) + 'B';
  } else if (Math.abs(value) >= 1_000_000) {
    return formatter.format(value / 1_000_000) + 'M';
  } else if (Math.abs(value) >= 1_000) {
    return formatter.format(value / 1_000) + 'K';
  } else {
    return formatter.format(value);
  }
}
