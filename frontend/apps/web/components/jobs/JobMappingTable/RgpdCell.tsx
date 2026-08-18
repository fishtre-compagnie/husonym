import { cn } from '@/libs/utils';
import {
  CheckCircledIcon,
  ExclamationTriangleIcon,
} from '@radix-ui/react-icons';
import { PiiConfidence, PiiDetectionMethod } from '@husonym/sdk';
import { ReactElement } from 'react';

// Libellés lisibles des catégories détectées (backend pkg/piidetect + entités Presidio).
const CATEGORY_LABELS: Record<string, string> = {
  email: 'Email',
  phone_number: 'Téléphone',
  person_first_name: 'Prénom',
  person_last_name: 'Nom',
  person_full_name: 'Nom',
  username: 'Identifiant',
  street_address: 'Adresse',
  city: 'Ville',
  state: 'Région',
  postal_code: 'Code postal',
  country: 'Pays',
  ssn: 'N° sécurité sociale',
  nir: 'N° sécurité sociale',
  iban: 'IBAN',
  siret: 'SIRET',
  credit_card: 'Carte bancaire',
  ip_address: 'Adresse IP',
  gender: 'Genre',
  birth_date: 'Date de naissance',
  date: 'Date',
};

// Moteur de reconnaissance qui a identifié la colonne. Nommé du point de vue de
// l'utilisateur : ce qui compte pour lui est de savoir à quel point le résultat
// est fiable, pas le détail de l'implémentation.
const ENGINE_LABELS: Record<number, string> = {
  [PiiDetectionMethod.COLUMN_NAME]: 'dictionnaire',
  [PiiDetectionMethod.CHECKSUM]: 'clé de contrôle',
  [PiiDetectionMethod.CONTENT]: 'IA',
  [PiiDetectionMethod.FORMAT]: 'analyse de format',
};

interface Props {
  isSensitive: boolean;
  dataCategory?: string;
  // Niveau de confiance : CONFIRMED donne un badge vert, NEEDS_REVIEW un badge
  // orange invitant à lever le doute.
  confidence?: PiiConfidence;
  method?: PiiDetectionMethod;
  // true si un transformer d'anonymisation est réellement appliqué. Faux pour
  // Passthrough : la valeur d'origine est alors recopiée telle quelle.
  isAnonymized?: boolean;
  // true si un transformer adapté existe pour cette colonne. Faux pour une date
  // en texte ou un IBAN, faute de générateur qui préserve le format : le message
  // doit alors dire « aucun transformer compatible » et non « non anonymisée ».
  hasSuggestion?: boolean;
}

// Cellule de la colonne « RGPD ».
//
// Trois états, parce qu'ils appellent trois actions différentes :
//   * vert   — sensible ET anonymisé : rien à faire.
//   * rouge  — sensible mais laissé en Passthrough : la donnée personnelle
//              partirait EN CLAIR vers la destination. Un badge vert dans ce cas
//              laissait croire que la colonne était protégée.
//   * orange — détection non prouvée (modèle statistique, format ambigu, clé
//              valide en partie) : à confirmer ou écarter.
export default function RgpdCell({
  isSensitive,
  dataCategory,
  confidence,
  method,
  isAnonymized,
  hasSuggestion,
}: Props): ReactElement {
  const needsReview = confidence === PiiConfidence.NEEDS_REVIEW;
  // Le doute sur la détection primes : inutile d'alarmer sur une absence de
  // transformer si la colonne n'est peut-être pas personnelle.
  const nonTraite = isSensitive && !needsReview && isAnonymized === false;

  // Une détection à confirmer reste affichée même si le backend n'a pas tranché
  // sur la sensibilité : c'est précisément l'objet de la levée de doute.
  if (!isSensitive && !needsReview) {
    return <span className="text-muted-foreground/40 text-xs">—</span>;
  }

  const category = dataCategory ? CATEGORY_LABELS[dataCategory] : undefined;
  const engine = method !== undefined ? ENGINE_LABELS[method] : undefined;

  // Une phrase, trois variantes. Le détail de la preuve (nombre de valeurs
  // vérifiées, formats candidats...) est volontairement omis : l'utilisateur a
  // besoin de savoir quoi faire, pas comment le calcul a été mené.
  const source = engine ? ` Reconnue par ${engine}` : '';
  const tooltip = needsReview
    ? `Donnée RGPD.${source}, mais ambiguïté.`
    : nonTraite
      ? hasSuggestion
        ? `Donnée RGPD.${source}, mais non anonymisée.`
        : `Donnée RGPD.${source}, mais aucun transformer compatible.`
      : `Donnée RGPD.${source}.`;

  return (
    <span
      title={tooltip}
      aria-label={category ? `${tooltip} (${category})` : tooltip}
      className={cn(
        'inline-flex items-center gap-1 rounded-full border px-2 py-0.5 text-xs font-semibold',
        needsReview
          ? [
              'border-amber-600/40 bg-amber-50 text-amber-700',
              'dark:border-amber-500/40 dark:bg-amber-950/40 dark:text-amber-400',
            ]
          : nonTraite
            ? [
                'border-red-600/40 bg-red-50 text-red-700',
                'dark:border-red-500/40 dark:bg-red-950/40 dark:text-red-400',
              ]
            : [
                'border-green-600/40 bg-green-50 text-green-700',
                'dark:border-green-500/40 dark:bg-green-950/40 dark:text-green-400',
              ]
      )}
    >
      {needsReview || nonTraite ? (
        <ExclamationTriangleIcon className="h-3.5 w-3.5" />
      ) : (
        <CheckCircledIcon className="h-3.5 w-3.5" />
      )}
      {needsReview ? 'À vérifier' : nonTraite ? 'Non traité' : 'RGPD'}
    </span>
  );
}
