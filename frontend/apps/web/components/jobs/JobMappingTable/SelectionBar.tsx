import ColumnDecisionFields from '@/components/jobs/ColumnDecision/ColumnDecisionFields';
import ColumnDecisionPanel from '@/components/jobs/ColumnDecision/ColumnDecisionPanel';
import { TransformerResult } from '@/components/jobs/SchemaTable/transformer-handler';
import { Button } from '@/components/ui/button';
import {
  isSystemTransformer,
  isUserDefinedTransformer,
  Transformer,
} from '@/shared/transformers';
import { isInvalidTransformer } from '@/util/util';
import {
  convertJobMappingTransformerToForm,
  JobMappingTransformerForm,
} from '@/yup-validations/jobs';
import { create } from '@bufbuild/protobuf';
import { JobMappingTransformerSchema } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { ReactElement, useCallback, useState } from 'react';

interface Props {
  count: number;
  // Ceux que TOUTES les lignes choisies acceptent.
  getAllowedTransformers(): TransformerResult;
  getTransformerFromFieldValue(value: JobMappingTransformerForm): Transformer;
  onApply(value: JobMappingTransformerForm): void;
  onClear(): void;
}

const NONE = (): JobMappingTransformerForm =>
  convertJobMappingTransformerToForm(create(JobMappingTransformerSchema));

export default function SelectionBar(props: Props): ReactElement | null {
  const {
    count,
    getAllowedTransformers,
    getTransformerFromFieldValue,
    onApply,
    onClear,
  } = props;

  const t = useTranslations('mapping.jobMappingTable.selectionBar');
  const [open, setOpen] = useState(false);
  const [draft, setDraft] = useState<JobMappingTransformerForm>(NONE);
  const [optionsValid, setOptionsValid] = useState(true);
  const onValidChange = useCallback(
    (valid: boolean) => setOptionsValid(valid),
    []
  );

  if (count === 0) {
    return null;
  }

  const allowed = getAllowedTransformers();
  const selected = getTransformerFromFieldValue(draft);
  // La liste offerte est déjà l'intersection, mais le choix courant peut dater
  // d'une sélection précédente.
  const applicable = isTransformerAllowed(allowed, selected);

  return (
    <>
      <div className="sticky bottom-4 z-20 flex justify-center pointer-events-none">
        <div
          role="region"
          aria-label={t('region')}
          className="pointer-events-auto flex flex-row items-center gap-3 rounded-2xl border border-border bg-card py-2 pl-4 pr-2 shadow-pop"
        >
          <span className="text-sm font-medium">
            {t('selected', { count })}
          </span>
          <span className="h-5 w-px bg-border" aria-hidden="true" />
          <Button type="button" size="sm" onClick={() => setOpen(true)}>
            {t('setTransformer')}
          </Button>
          <Button type="button" size="sm" variant="ghost" onClick={onClear}>
            {t('clear')}
          </Button>
        </div>
      </div>

      {open && (
        <ColumnDecisionPanel
          title={t('panelTitle', { count })}
          location={t('region')}
          meta={t('meta')}
          onClose={() => setOpen(false)}
          footer={
            <>
              <Button
                type="button"
                variant="outline"
                onClick={() => setOpen(false)}
              >
                {t('cancel')}
              </Button>
              <Button
                type="button"
                disabled={!draft.config.case || !applicable || !optionsValid}
                title={applicable ? undefined : t('noSharedTransformer')}
                onClick={() => {
                  onApply(draft);
                  setDraft(NONE());
                  setOpen(false);
                }}
              >
                {t('apply', { count })}
              </Button>
            </>
          }
        >
          {/* Pas d'aperçu : il faudrait une colonne, et la sélection en compte plusieurs. */}
          <ColumnDecisionFields
            value={draft}
            onChange={setDraft}
            getTransformers={getAllowedTransformers}
            selected={selected}
            onValidChange={onValidChange}
            placeholder={t('placeholder')}
          />
          {!applicable && (
            <p className="text-sm text-danger-fg">{t('noCommon')}</p>
          )}
        </ColumnDecisionPanel>
      )}
    </>
  );
}

function isTransformerAllowed(
  { system, userDefined }: TransformerResult,
  selected: Transformer
): boolean {
  if (isInvalidTransformer(selected)) {
    return true;
  }
  if (isUserDefinedTransformer(selected)) {
    return userDefined.some((t) => t.id === selected.id);
  }
  if (isSystemTransformer(selected)) {
    return system.some((t) => t.source === selected.source);
  }
  return false;
}
