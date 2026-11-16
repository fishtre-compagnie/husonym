import { summarizeOptions } from '@/app/(mgmt)/[account]/new/transformer/TransformerForms/options/summary';
import { Button } from '@/components/ui/button';
import { cn } from '@/libs/utils';
import {
  convertJobMappingTransformerFormToJobMappingTransformer,
  JobMappingTransformerForm,
} from '@/yup-validations/jobs';
import { useTranslations } from 'next-intl';
import { ReactElement } from 'react';
import { LuPanelRight } from 'react-icons/lu';
import { ChevronDownIcon } from '@radix-ui/react-icons';
import { RgpdState } from './rgpd-state';

interface Props {
  column: string;
  transformer: JobMappingTransformerForm;
  name: string;
  isSensitive: boolean;
  // What the detection says of the column, with its transformer.
  state?: RgpdState;
  onOpen(): void;
}

type PillLook = 'clear' | 'doubt' | 'protected' | 'kept' | 'empty';

// The look of the pill tells what leaves the column:
//   * clear     — personal data copied as it is: it leaves in clear.
//   * doubt     — copied as it is, while the detection is still to confirm.
//   * protected — a transformer rewrites the value.
//   * kept      — nothing personal, copied as it is.
//   * empty     — no transformer chosen yet.
function pillLook(
  anonymizes: boolean,
  hasTransformer: boolean,
  isSensitive: boolean,
  state?: RgpdState
): PillLook {
  if (anonymizes) {
    return 'protected';
  }
  if (state === 'review' || state === 'not_analyzed') {
    return 'doubt';
  }
  if (state === 'not_anonymized' || isSensitive) {
    return 'clear';
  }
  return hasTransformer ? 'kept' : 'empty';
}

const PILL_CLASS: Record<PillLook, string> = {
  clear: 'border-dashed border-danger bg-danger-soft text-danger-fg',
  doubt: 'border-dashed border-warn bg-warn-soft text-warn-fg',
  protected: 'border-line-soft bg-brand-soft text-hi',
  kept: 'border-transparent bg-transparent text-muted-foreground',
  empty: 'border-dashed border-border bg-transparent text-muted-foreground',
};

const PILL_NOTE: Partial<Record<PillLook, 'inClear' | 'toConfirm'>> = {
  clear: 'inClear',
  doubt: 'toConfirm',
};

// Ce qui sortira de la colonne, lisible sans rien ouvrir.
export default function MappingCell(props: Props): ReactElement {
  const { column, transformer, name, isSensitive, state, onOpen } = props;
  const t = useTranslations('mapping.jobMappingTable.mappingCell');

  const configCase = transformer.config.case;
  const anonymizes = !!configCase && configCase !== 'passthroughConfig';
  const options = configCase
    ? summarizeOptions(
        convertJobMappingTransformerFormToJobMappingTransformer(transformer)
          .config
      )
    : [];
  const look = pillLook(anonymizes, !!configCase, isSensitive, state);
  const noteKey = PILL_NOTE[look];
  const note = noteKey ? t(noteKey) : undefined;

  return (
    <button
      type="button"
      onClick={(e) => {
        // The row opens the same panel: once is enough.
        e.stopPropagation();
        onOpen();
      }}
      aria-label={`${t('decideAria', { column, name: name || t('noTransformer') })}${note ? `, ${note}` : ''}`}
      className={cn(
        'flex w-full min-w-0 cursor-pointer items-center gap-2 rounded-lg border px-2.5 py-1 text-left text-[13px] font-medium transition-colors hover:border-primary',
        PILL_CLASS[look]
      )}
    >
      <span className="flex min-w-0 flex-1 flex-col">
        <span className="truncate">
          {name || t('chooseTransformer')}
          {note && <span className="font-normal opacity-80"> · {note}</span>}
        </span>
        {options.length > 0 && (
          <span className="truncate text-xs font-normal text-muted-foreground">
            {options.join(' · ')}
          </span>
        )}
      </span>
      <ChevronDownIcon
        aria-hidden="true"
        className="size-3 shrink-0 opacity-70"
      />
    </button>
  );
}

interface OpenProps {
  column: string;
  onOpen(): void;
}

export function OpenDecisionButton(props: OpenProps): ReactElement {
  const { column, onOpen } = props;
  const t = useTranslations('mapping.jobMappingTable.mappingCell');
  return (
    <Button
      type="button"
      variant="ghost"
      size="icon"
      className="h-8 w-8 text-muted-foreground hover:text-foreground"
      aria-label={t('openDecision', { column })}
      onClick={onOpen}
    >
      <LuPanelRight className="h-4 w-4" />
    </Button>
  );
}
