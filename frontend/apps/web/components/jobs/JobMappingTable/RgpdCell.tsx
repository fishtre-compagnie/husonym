import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover';
import {
  JobMappingTransformerForm,
  convertJobMappingTransformerToForm,
} from '@/yup-validations/jobs';
import { create } from '@bufbuild/protobuf';
import {
  JobMappingTransformerSchema,
  SystemTransformer,
  TransformerSource,
  UserDefinedTransformer,
} from '@husonym/sdk';
import { LockClosedIcon } from '@radix-ui/react-icons';
import { ReactElement, useEffect, useMemo, useState } from 'react';
import TransformerSelect from '../SchemaTable/TransformerSelect';

// Libellés lisibles des catégories détectées par pkg/piidetect (backend).
const CATEGORY_LABELS: Record<string, string> = {
  email: 'Email',
  phone_number: 'Téléphone',
  person_first_name: 'Prénom',
  person_last_name: 'Nom',
  person_full_name: 'Nom complet',
  username: 'Identifiant',
  street_address: 'Adresse',
  city: 'Ville',
  state: 'Région',
  postal_code: 'Code postal',
  country: 'Pays',
  ssn: 'N° sécurité sociale',
  credit_card: 'Carte bancaire',
  ip_address: 'Adresse IP',
  gender: 'Genre',
};

interface Props {
  isSensitive: boolean;
  dataCategory?: string;
  suggestedSource: TransformerSource;
  // Récupère les transformers compatibles avec la colonne (déjà filtrés par type).
  getTransformers(): {
    system: SystemTransformer[];
    userDefined: UserDefinedTransformer[];
  };
  // Applique le transformer choisi à la ligne (comme une sélection manuelle).
  onApply(value: JobMappingTransformerForm): void;
}

function toForm(sys: SystemTransformer): JobMappingTransformerForm {
  return convertJobMappingTransformerToForm(
    create(JobMappingTransformerSchema, { config: sys.config })
  );
}

export default function RgpdCell(props: Props): ReactElement | null {
  const { isSensitive, dataCategory, suggestedSource, getTransformers, onApply } =
    props;

  const [open, setOpen] = useState(false);
  const [transformers, setTransformers] = useState<{
    system: SystemTransformer[];
    userDefined: UserDefinedTransformer[];
  }>({ system: [], userDefined: [] });
  const [selected, setSelected] = useState<JobMappingTransformerForm | null>(
    null
  );

  // Charge les transformers à l'ouverture et pré-sélectionne la suggestion.
  useEffect(() => {
    if (!open) {
      return;
    }
    const result = getTransformers();
    setTransformers(result);
    const suggested = result.system.find((t) => t.source === suggestedSource);
    setSelected(suggested ? toForm(suggested) : null);
  }, [open, suggestedSource]);

  const label = useMemo(() => {
    if (dataCategory && CATEGORY_LABELS[dataCategory]) {
      return CATEGORY_LABELS[dataCategory];
    }
    return 'Donnée personnelle';
  }, [dataCategory]);

  // Nom lisible du transformer actuellement sélectionné (par correspondance de config).
  const selectedName = useMemo(() => {
    if (!selected?.config.case) {
      return 'Choisir un transformer';
    }
    const match = transformers.system.find(
      (t) => t.config?.config.case === selected.config.case
    );
    return match?.name ?? 'Transformer';
  }, [selected, transformers]);

  if (!isSensitive) {
    return <span className="text-muted-foreground/40 text-xs">—</span>;
  }

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Badge
          variant="outline"
          className="cursor-pointer gap-1 border-amber-400/60 bg-amber-50 text-amber-700 hover:bg-amber-100 dark:border-amber-500/40 dark:bg-amber-950/40 dark:text-amber-300 dark:hover:bg-amber-950/70"
        >
          <LockClosedIcon className="h-3 w-3" />
          {label}
        </Badge>
      </PopoverTrigger>
      <PopoverContent align="start" className="w-80">
        <div className="flex flex-col gap-3">
          <div className="flex items-center gap-2">
            <LockClosedIcon className="h-4 w-4 text-amber-500" />
            <span className="text-sm font-medium">
              Donnée personnelle : {label}
            </span>
          </div>
          <p className="text-xs text-muted-foreground">
            Transformer suggéré pour anonymiser cette colonne. Ajustez si besoin,
            puis appliquez.
          </p>

          <TransformerSelect
            getTransformers={() => transformers}
            value={
              selected ?? { config: { case: '', value: {} } }
            }
            buttonText={selectedName}
            buttonClassName="w-full"
            onSelect={(value) => setSelected(value)}
            side="bottom"
            disabled={false}
          />

          <div className="flex justify-end gap-2">
            <Button
              type="button"
              variant="ghost"
              size="sm"
              onClick={() => setOpen(false)}
            >
              Annuler
            </Button>
            <Button
              type="button"
              size="sm"
              disabled={!selected?.config.case}
              onClick={() => {
                if (selected) {
                  onApply(selected);
                }
                setOpen(false);
              }}
            >
              Apply
            </Button>
          </div>
        </div>
      </PopoverContent>
    </Popover>
  );
}
