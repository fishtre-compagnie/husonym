import { summarizeOptions } from '@/app/(mgmt)/[account]/new/transformer/TransformerForms/options/summary';
import { Button } from '@/components/ui/button';
import { cn } from '@/libs/utils';
import {
  convertJobMappingTransformerFormToJobMappingTransformer,
  JobMappingTransformerForm,
} from '@/yup-validations/jobs';
import { ReactElement } from 'react';
import { LuPanelRight } from 'react-icons/lu';

interface Props {
  column: string;
  transformer: JobMappingTransformerForm;
  name: string;
  isSensitive: boolean;
  onOpen(): void;
}

// Ce qui sortira de la colonne, lisible sans rien ouvrir.
export default function MappingCell(props: Props): ReactElement {
  const { column, transformer, name, isSensitive, onOpen } = props;

  const configCase = transformer.config.case;
  const anonymizes = !!configCase && configCase !== 'passthroughConfig';
  const options = configCase
    ? summarizeOptions(
        convertJobMappingTransformerFormToJobMappingTransformer(transformer)
          .config
      )
    : [];

  return (
    <button
      type="button"
      onClick={onOpen}
      aria-label={`Decide what leaves ${column}`}
      className="flex flex-row items-center gap-2 text-left w-full group"
    >
      {/* Ambre : une donnée personnelle laissée telle quelle. */}
      <span
        className={cn(
          'h-2 w-2 rounded-full shrink-0',
          anonymizes
            ? 'bg-emerald-500'
            : isSensitive
              ? 'bg-amber-500'
              : 'bg-gray-300 dark:bg-gray-600'
        )}
      />
      <span className="flex flex-col min-w-0">
        <span
          className={cn(
            'text-sm truncate group-hover:underline underline-offset-2',
            !anonymizes && isSensitive && 'text-amber-700 dark:text-amber-500'
          )}
        >
          {name || 'Choose a transformer'}
        </span>
        {options.length > 0 && (
          <span className="text-xs text-muted-foreground truncate">
            {options.join(' · ')}
          </span>
        )}
      </span>
    </button>
  );
}

interface OpenProps {
  column: string;
  onOpen(): void;
}

export function OpenDecisionButton(props: OpenProps): ReactElement {
  const { column, onOpen } = props;
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      className="h-8 w-8 text-muted-foreground hover:text-foreground"
      aria-label={`Open the decision panel for ${column}`}
      onClick={onOpen}
    >
      <LuPanelRight className="h-4 w-4" />
    </Button>
  );
}
