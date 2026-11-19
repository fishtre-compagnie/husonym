/**
 * Le scan de contenu d'une table est conservé par l'API, avec les colonnes que la
 * table avait alors. À l'ouverture du mapping, l'écran relit ce scan au lieu
 * d'échantillonner de nouveau la table, et ne la réanalyse que si elle a changé
 * depuis : une colonne ajoutée, retirée, ou dont le type n'est plus le même.
 */

interface NamedColumn {
  column: string;
  dataType: string;
}

/**
 * Vrai si le scan conservé décrit encore la table telle que le schéma la montre
 * aujourd'hui : mêmes colonnes, chacune du même type. Une table dont on ignore les
 * colonnes n'est pas réputée inchangée.
 */
export function scanStillHolds(
  scanned: NamedColumn[],
  current: NamedColumn[] | undefined
): boolean {
  if (!current || current.length === 0 || scanned.length !== current.length) {
    return false;
  }
  const types = new Map(scanned.map((c) => [c.column, c.dataType]));
  if (types.size !== scanned.length) {
    return false;
  }
  return current.every((c) => types.get(c.column) === c.dataType);
}

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
