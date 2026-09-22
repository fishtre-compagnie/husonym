import { summarizeOptions } from '@/app/(mgmt)/[account]/new/transformer/TransformerForms/options/summary';
import ButtonText from '@/components/ButtonText';
import ColumnDecisionFields, {
  configKey,
  toConfig,
} from '@/components/jobs/ColumnDecision/ColumnDecisionFields';
import ColumnDecisionPanel, {
  PanelNavigation,
} from '@/components/jobs/ColumnDecision/ColumnDecisionPanel';
import { TransformerHandler } from '@/components/jobs/SchemaTable/transformer-handler';
import Spinner from '@/components/Spinner';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { changeLabel, columnName, isPassthrough } from '@/util/mapping-changes';
import { getTransformerFromField } from '@/util/util';
import {
  convertJobMappingTransformerToForm,
  JobMappingTransformerForm,
} from '@/yup-validations/jobs';
import { JobMappingChange, JobMappingChangeKind } from '@husonym/sdk';
import { ReactElement, useCallback, useState } from 'react';

interface Props {
  change: JobMappingChange;
  onClose(): void;
  handler: TransformerHandler;
  // The source connection the preview reads from. No preview without it.
  sourceConnectionId?: string;
  navigation?: PanelNavigation;
  onReview(change: JobMappingChange, note?: string): Promise<void>;
  onApply(
    change: JobMappingChange,
    transformer: JobMappingTransformerForm,
    note?: string
  ): Promise<void>;
}

const NO_TRANSFORMER: JobMappingTransformerForm = {
  config: { case: '', value: {} },
};

// Everything needed to decide about one change, in one place: the transformer and its options,
// what they make of the column's values, and a note. The run's choice is kept, or replaced.
export default function ChangeDecisionPanel(props: Props): ReactElement {
  const {
    change,
    onClose,
    handler,
    sourceConnectionId,
    navigation,
    onReview,
    onApply,
  } = props;
  const runChoice = change.transformer
    ? convertJobMappingTransformerToForm(change.transformer)
    : NO_TRANSFORMER;
  const [draft, setDraft] = useState<JobMappingTransformerForm>(runChoice);
  const [note, setNote] = useState('');
  const [pending, setPending] = useState<'review' | 'apply' | null>(null);
  const [optionsValid, setOptionsValid] = useState(true);
  const onValidChange = useCallback(
    (valid: boolean) => setOptionsValid(valid),
    []
  );

  const correctable = change.kind !== JobMappingChangeKind.REMOVED;
  const changed = configKey(draft) !== configKey(runChoice);

  async function act(action: 'review' | 'apply'): Promise<void> {
    setPending(action);
    try {
      const trimmed = note.trim() || undefined;
      if (action === 'apply') {
        await onApply(change, draft, trimmed);
      } else {
        await onReview(change, trimmed);
      }
      onClose();
    } catch {
      // The caller has already said what went wrong; the panel stays open with the choice made.
    } finally {
      setPending(null);
    }
  }

  return (
    <ColumnDecisionPanel
      title={columnName(change)}
      location={change.column?.schema}
      navigation={navigation}
      onClose={onClose}
      badges={
        <>
          <Badge variant="outline" className="font-mono font-normal">
            {change.dataType}
          </Badge>
          <Badge variant="outline">{changeLabel(change)}</Badge>
          {change.piiCategory && (
            <Badge
              variant={
                change.kind === JobMappingChangeKind.ADDED &&
                isPassthrough(change)
                  ? 'destructive'
                  : 'secondary'
              }
            >
              Personal data: {change.piiCategory}
            </Badge>
          )}
        </>
      }
      footer={
        <>
          <Button
            type="button"
            variant={changed ? 'outline' : 'default'}
            disabled={pending !== null}
            onClick={() => act('review')}
          >
            <ButtonText
              leftIcon={
                pending === 'review' ? <Spinner className="h-4 w-4" /> : null
              }
              text={correctable ? "Keep the run's choice" : 'Mark reviewed'}
            />
          </Button>
          {correctable && (
            <Button
              type="button"
              disabled={
                !changed ||
                !draft.config.case ||
                !optionsValid ||
                pending !== null
              }
              title={
                changed ? undefined : 'Pick another transformer or option first'
              }
              onClick={() => act('apply')}
            >
              <ButtonText
                leftIcon={
                  pending === 'apply' ? <Spinner className="h-4 w-4" /> : null
                }
                text="Apply"
              />
            </Button>
          )}
        </>
      }
    >
      {correctable ? (
        <ColumnDecisionFields
          value={draft}
          onChange={setDraft}
          handler={handler}
          dataType={change.dataType}
          disabled={pending !== null}
          onValidChange={onValidChange}
          origin={
            <span className="text-xs text-muted-foreground">
              {changed
                ? 'Changed from what the run chose.'
                : 'As the run chose it.'}
            </span>
          }
          preview={
            sourceConnectionId && change.column
              ? {
                  connectionId: sourceConnectionId,
                  schema: change.column.schema,
                  table: change.column.table,
                  column: change.column.column,
                }
              : undefined
          }
        />
      ) : (
        <p className="text-sm">
          The column left the source; the job mapped it with{' '}
          <strong>{getTransformerFromField(handler, runChoice).name}</strong>
          {summarizeOptions(toConfig(runChoice)).length > 0 &&
            ` (${summarizeOptions(toConfig(runChoice)).join(' · ')})`}
          . Its mapping has been removed from the job.
        </p>
      )}

      <section className="flex flex-col gap-2">
        <Label htmlFor="change-decision-note">
          Why is this fine? (optional)
        </Label>
        <Textarea
          id="change-decision-note"
          value={note}
          onChange={(e) => setNote(e.target.value)}
          placeholder="e.g. internal references, hold no personal data"
        />
      </section>
    </ColumnDecisionPanel>
  );
}
