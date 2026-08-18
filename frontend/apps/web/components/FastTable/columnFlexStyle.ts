import { CSSProperties } from 'react';

/**
 * Style de largeur d'une colonne, partagé par l'en-tête et les cellules — les
 * deux DOIVENT recevoir exactement le même, sinon ils se décalent.
 *
 * Les colonnes se partagent la largeur du conteneur proportionnellement à leur
 * `size` : `flex-grow` valant la `size`, une colonne déclarée à 240 s'étire deux
 * fois plus qu'une colonne à 120, et le tableau remplit toute la largeur sans
 * laisser de vide à droite sur les grands écrans. La `size` sert aussi de
 * largeur minimale, ce qui évite l'écrasement sur petit écran.
 *
 * Les colonnes listées dans `noGrow` gardent une largeur fixe : une case à cocher
 * ou un bouton-icône n'a rien à gagner à s'élargir.
 */
export function columnFlexStyle(
  size: number,
  columnId: string,
  noGrow?: string[]
): CSSProperties {
  if (noGrow?.includes(columnId)) {
    return { width: size, minWidth: size, maxWidth: size, flex: '0 0 auto' };
  }
  return { flex: `${size} 1 ${size}px`, minWidth: size };
}
