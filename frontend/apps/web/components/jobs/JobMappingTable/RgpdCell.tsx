import { cn } from '@/libs/utils';
import {
  CheckCircledIcon,
  ExclamationTriangleIcon,
  QuestionMarkCircledIcon,
} from '@radix-ui/react-icons';
import { PiiConfidence, PiiDetectionMethod } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { rgpdState } from './rgpd-state';

// Les catégories détectées (backend pkg/piidetect + entités Presidio), sous la clé de
// leur libellé dans les messages.
const CATEGORY_KEYS = {
  email: 'email',
  phone_number: 'phone_number',
  person_first_name: 'person_first_name',
  person_last_name: 'person_last_name',
  person_full_name: 'person_full_name',
  username: 'username',
  street_address: 'street_address',
  city: 'city',
  state: 'state',
  postal_code: 'postal_code',
  country: 'country',
  ssn: 'ssn',
  nir: 'ssn',
  national_id: 'national_id',
  secret: 'secret',
  password_hash: 'password_hash',
  iban: 'iban',
  bank_account: 'bank_account',
  salary: 'salary',
  age: 'age',
  ethnicity: 'ethnicity',
  marital_status: 'marital_status',
  mac_address: 'mac_address',
  siret: 'siret',
  credit_card: 'credit_card',
  ip_address: 'ip_address',
  gender: 'gender',
  birth_date: 'birth_date',
  date: 'date',
} as const;

function categoryKey(
  category: string
): (typeof CATEGORY_KEYS)[keyof typeof CATEGORY_KEYS] | undefined {
  return Object.hasOwn(CATEGORY_KEYS, category)
    ? CATEGORY_KEYS[category as keyof typeof CATEGORY_KEYS]
    : undefined;
}

// Moteur de reconnaissance qui a identifié la colonne. Nommé du point de vue de
// l'utilisateur : ce qui compte pour lui est de savoir à quel point le résultat
// est fiable, pas le détail de l'implémentation.
const ENGINE_KEYS: Record<
  number,
  'columnName' | 'checksum' | 'content' | 'format'
> = {
  [PiiDetectionMethod.COLUMN_NAME]: 'columnName',
  [PiiDetectionMethod.CHECKSUM]: 'checksum',
  [PiiDetectionMethod.CONTENT]: 'content',
  [PiiDetectionMethod.FORMAT]: 'format',
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
  // true si un transformer adapté existe pour cette colonne. Faux pour une
  // colonne dont aucun transformer n'écrit le type (JSON, tableau, binaire, type
  // défini dans le schéma) : le message doit alors dire « aucun transformer
  // compatible » et non « non anonymisée ».
  hasSuggestion?: boolean;
  // true si le scan de contenu n'a pas pu analyser la colonne : son nom seul a
  // parlé, et l'absence de détection ne dit rien de ce qu'elle contient.
  contentNotAnalyzed?: boolean;
}

// Cellule de la colonne « RGPD ».
//
// Une colonne dont le contenu n'a pas pu être analysé, et dont le nom ne dit
// rien, porte un badge gris : vide, la cellule passerait pour « aucune donnée
// personnelle ».
//
// Trois autres états, parce qu'ils appellent trois actions différentes :
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
  contentNotAnalyzed,
}: Props): ReactElement {
  const t = useTranslations('mapping.jobMappingTable.rgpdCell');
  const state = rgpdState({
    isSensitive,
    confidence,
    isAnonymized,
    contentNotAnalyzed,
  });
  // Une détection à confirmer reste affichée même si le backend n'a pas tranché
  // sur la sensibilité : c'est précisément l'objet de la levée de doute.
  const needsReview = state === 'review';
  const nonTraite = state === 'not_anonymized';

  if (state === 'none') {
    return (
      <span
        className="text-xs text-muted-foreground"
        aria-label={t('noPersonalData')}
      >
        —
      </span>
    );
  }
  if (state === 'not_analyzed') {
    const notAnalyzed = t('notAnalyzedTitle');
    return (
      <span
        title={notAnalyzed}
        aria-label={notAnalyzed}
        className="inline-flex items-center gap-1 rounded-full bg-quiet-soft px-2 py-0.5 text-xs font-medium text-quiet-fg"
      >
        <QuestionMarkCircledIcon className="h-3.5 w-3.5" />
        {t('notAnalyzed')}
      </span>
    );
  }

  const catKey = dataCategory ? categoryKey(dataCategory) : undefined;
  const category = catKey ? t(`categories.${catKey}`) : undefined;
  const engineKey = method !== undefined ? ENGINE_KEYS[method] : undefined;
  const engine = engineKey ? t(`engines.${engineKey}`) : undefined;

  // Une phrase, trois variantes. Le détail de la preuve (nombre de valeurs
  // vérifiées, formats candidats...) est volontairement omis : l'utilisateur a
  // besoin de savoir quoi faire, pas comment le calcul a été mené.
  // Le point est porté par le fragment, pas par la phrase : autrement, un moteur
  // absent laissait un point suivi d'une virgule au milieu de l'infobulle.
  const source = engine ? ` ${t('recognizedBy', { engine })}` : '';
  const tooltip = `${
    needsReview
      ? t('ambiguous')
      : nonTraite
        ? hasSuggestion
          ? t('notAnonymizedTooltip')
          : t('noCompatibleTransformer')
        : t('gdprData')
  }${source}`;

  return (
    <span
      title={tooltip}
      aria-label={category ? `${tooltip} (${category})` : tooltip}
      className="flex min-w-0 flex-col items-start gap-0.5"
    >
      <span
        className={cn(
          'inline-flex items-center gap-1 rounded-full px-2 py-0.5 text-xs font-medium',
          needsReview
            ? 'bg-warn-soft text-warn-fg'
            : nonTraite
              ? 'bg-danger-soft text-danger-fg'
              : 'bg-ok-soft text-ok'
        )}
      >
        {needsReview || nonTraite ? (
          <ExclamationTriangleIcon className="h-3.5 w-3.5" />
        ) : (
          <CheckCircledIcon className="h-3.5 w-3.5" />
        )}
        {needsReview ? t('review') : nonTraite ? t('notAnonymized') : t('gdpr')}
      </span>
      {category && (
        <span className="max-w-full truncate text-xs text-muted-foreground">
          {category}
        </span>
      )}
    </span>
  );
}
