/**
 * Le scan de contenu d'une table est conservé par l'API. À l'ouverture du mapping,
 * l'écran relit ce scan au lieu d'échantillonner de nouveau la table ; c'est l'API
 * qui juge s'il décrit encore la table (voir GetPiiScanResults).
 */

/** Le plus ancien des instants donnés : l'âge de l'analyse la plus vieille affichée. */
export function oldestScan(dates: (Date | undefined)[]): Date | undefined {
  let oldest: Date | undefined;
  for (const d of dates) {
    if (d && (!oldest || d.getTime() < oldest.getTime())) {
      oldest = d;
    }
  }
  return oldest;
}
