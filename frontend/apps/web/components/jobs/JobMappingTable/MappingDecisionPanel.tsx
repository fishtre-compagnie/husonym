import ColumnDecisionFields, {
  configKey,
} from '@/components/jobs/ColumnDecision/ColumnDecisionFields';
import ColumnDecisionPanel, {
  PanelNavigation,
} from '@/components/jobs/ColumnDecision/ColumnDecisionPanel';
import { TransformerResult } from '@/components/jobs/SchemaTable/transformer-handler';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Transformer } from '@/shared/transformers';
import { JobMappingTransformerForm } from '@/yup-validations/jobs';
import { ReactElement, useCallback, useState } from 'react';

// La colonne dont on décide, telle que la table la connaît.
export interface ColumnDecisionTarget {
  schema: string;
  table: string;
  column: string;
  dataType: string;
  transformer: JobMappingTransformerForm;
  // Détection RGPD : la colonne porte-t-elle une donnée personnelle, et laquelle.
  isSensitive: boolean;
  dataCategory?: string;
  // Les contraintes qui pèsent sur la colonne, en quelques mots.
  constraints?: string;
}

interface Props {
  target: ColumnDecisionTarget;
  getTransformers(): TransformerResult;
  getTransformerFromFieldValue(value: JobMappingTransformerForm): Transformer;
  // L'aperçu lit la colonne sur cette connexion. Absent pour un job generate.
  sourceConnectionId?: string;
  navigation?: PanelNavigation;
  onClose(): void;
  onApply(transformer: JobMappingTransformerForm): void;
}

// La décision d'une colonne de la page Source : le transformer, ses options et ce
// qu'ils font des valeurs, dans le panneau que l'onglet Review utilise aussi.
// « Apply » écrit dans le formulaire de la page ; le job, lui, est enregistré par
// son bouton Update.
export default function MappingDecisionPanel(props: Props): ReactElement {
  const {
    target,
    getTransformers,
    getTransformerFromFieldValue,
    sourceConnectionId,
    navigation,
    onClose,
    onApply,
  } = props;

  const [draft, setDraft] = useState<JobMappingTransformerForm>(
    target.transformer
  );
  const [optionsValid, setOptionsValid] = useState(true);
  const onValidChange = useCallback(
    (valid: boolean) => setOptionsValid(valid),
    []
  );

  const changed = configKey(draft) !== configKey(target.transformer);

  return (
    <ColumnDecisionPanel
      title={target.column}
      location={`${target.schema}.${target.table}`}
      navigation={navigation}
      onClose={onClose}
      badges={
        <>
          <Badge variant="outline" className="font-mono font-normal">
            {target.dataType}
          </Badge>
          {target.constraints && (
            <Badge variant="outline">{target.constraints}</Badge>
          )}
          {target.isSensitive && (
            <Badge variant="secondary">
              Personal data
              {target.dataCategory ? `: ${target.dataCategory}` : ''}
            </Badge>
          )}
        </>
      }
      meta="Applied to the mapping right away; the job is saved when you press Update."
      footer={
        <>
          <Button type="button" variant="outline" onClick={onClose}>
            Cancel
          </Button>
          <Button
            type="button"
            disabled={!changed || !draft.config.case || !optionsValid}
            title={
              changed ? undefined : 'Pick another transformer or option first'
            }
            onClick={() => {
              onApply(draft);
              onClose();
            }}
          >
            Apply
          </Button>
        </>
      }
    >
      <ColumnDecisionFields
        value={draft}
        onChange={setDraft}
        getTransformers={getTransformers}
        selected={getTransformerFromFieldValue(draft)}
        onValidChange={onValidChange}
        preview={
          sourceConnectionId
            ? {
                connectionId: sourceConnectionId,
                schema: target.schema,
                table: target.table,
                column: target.column,
              }
            : undefined
        }
      />
    </ColumnDecisionPanel>
  );
}
