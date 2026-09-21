import { summarizeOptions } from '@/app/(mgmt)/[account]/new/transformer/TransformerForms/options/summary';
import TransformerForm from '@/app/(mgmt)/[account]/new/transformer/TransformerForms/TransformerForm';
import ButtonText from '@/components/ButtonText';
import ColumnPreview from '@/components/jobs/JobMappingTable/ColumnPreview';
import { dbDataTypeToTransformerDataType } from '@/components/jobs/SchemaTable/schema-constraint-handler';
import TransformerSelect from '@/components/jobs/SchemaTable/TransformerSelect';
import Spinner from '@/components/Spinner';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import { Form } from '@/components/ui/form';
import { Label } from '@/components/ui/label';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet';
import { Textarea } from '@/components/ui/textarea';
import { useDebouncedValue } from '@/libs/hooks/useDebouncedValue';
import { TransformerHandler } from '@/components/jobs/SchemaTable/transformer-handler';
import {
  getFilterdTransformersByType,
  getTransformerFromField,
  getTransformerSelectButtonText,
} from '@/util/util';
import { changeLabel, columnName, isPassthrough } from '@/util/mapping-changes';
import {
  convertJobMappingTransformerFormToJobMappingTransformer,
  convertJobMappingTransformerToForm,
  JobMappingTransformerForm,
} from '@/yup-validations/jobs';
import { create, toJsonString } from '@bufbuild/protobuf';
import {
  JobMappingChange,
  JobMappingChangeKind,
  JobMappingTransformerSchema,
  TransformerConfig,
  TransformerConfigSchema,
} from '@husonym/sdk';
import { ReactElement, useState } from 'react';
import { useForm } from 'react-hook-form';

interface Props {
  change: JobMappingChange;
  onClose(): void;
  handler: TransformerHandler;
  // The source connection the preview reads from. No preview without it.
  sourceConnectionId?: string;
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

function toConfig(
  transformer: JobMappingTransformerForm
): TransformerConfig | undefined {
  if (!transformer.config.case) {
    return undefined;
  }
  return convertJobMappingTransformerFormToJobMappingTransformer(transformer)
    .config;
}

function configKey(transformer: JobMappingTransformerForm): string {
  const config = toConfig(transformer);
  return config ? toJsonString(TransformerConfigSchema, config) : '';
}

// Everything needed to decide about one change, in one place: the transformer and its options,
// what they make of the column's values, and a note. The run's choice is kept, or replaced.
export default function ChangeDecisionPanel(props: Props): ReactElement {
  const { change, onClose, handler, sourceConnectionId, onReview, onApply } =
    props;
  const runChoice = change.transformer
    ? convertJobMappingTransformerToForm(change.transformer)
    : NO_TRANSFORMER;
  const [draft, setDraft] = useState<JobMappingTransformerForm>(runChoice);
  const [note, setNote] = useState('');
  const [pending, setPending] = useState<'review' | 'apply' | null>(null);
  // The options' labels read the form context, as on the other pages that edit a transformer.
  const form = useForm();

  const correctable = change.kind !== JobMappingChangeKind.REMOVED;
  const changed = configKey(draft) !== configKey(runChoice);
  // The preview follows the options once they stop changing, not at every keystroke.
  const previewed = useDebouncedValue(draft, 400);
  const previewConfig = toConfig(previewed);

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

  const selected = getTransformerFromField(handler, draft);
  const summary = summarizeOptions(toConfig(draft));

  return (
    <Sheet open onOpenChange={(open) => !open && onClose()}>
      <SheetContent className="w-full sm:max-w-3xl overflow-y-auto flex flex-col gap-6">
        <SheetHeader>
          <SheetTitle className="font-mono text-base">
            {columnName(change)}
          </SheetTitle>
          <SheetDescription asChild>
            <div className="flex flex-wrap items-center gap-2">
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
            </div>
          </SheetDescription>
        </SheetHeader>

        {correctable ? (
          <>
            <section className="flex flex-col gap-3">
              <Label>Transformer</Label>
              <TransformerSelect
                getTransformers={() =>
                  getFilterdTransformersByType(
                    handler,
                    dbDataTypeToTransformerDataType(change.dataType)
                  )
                }
                value={draft}
                buttonText={getTransformerSelectButtonText(
                  selected,
                  'Choose a transformer'
                )}
                onSelect={setDraft}
                disabled={pending !== null}
              />
              <p className="text-xs text-muted-foreground">
                {changed
                  ? 'Changed from what the run chose.'
                  : 'As the run chose it.'}
                {summary.length > 0 && ` ${summary.join(' · ')}`}
              </p>
              <Form {...form}>
                <TransformerForm
                  value={toConfig(draft) ?? create(TransformerConfigSchema, {})}
                  setValue={(config) =>
                    setDraft(
                      convertJobMappingTransformerToForm(
                        create(JobMappingTransformerSchema, { config })
                      )
                    )
                  }
                  disabled={pending !== null}
                />
              </Form>
            </section>

            <section className="flex flex-col gap-3">
              <Label>Preview</Label>
              {sourceConnectionId && change.column ? (
                <ColumnPreview
                  enabled
                  connectionId={sourceConnectionId}
                  schema={change.column.schema}
                  table={change.column.table}
                  column={change.column.column}
                  transformer={
                    previewConfig?.config.case === 'passthroughConfig'
                      ? undefined
                      : previewConfig
                  }
                  maxHeightClassName="max-h-72"
                />
              ) : (
                <p className="text-xs text-muted-foreground">
                  No preview: the job has no source connection to read from.
                </p>
              )}
            </section>
          </>
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

        <SheetFooter className="flex flex-row justify-end gap-2">
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
              disabled={!changed || !draft.config.case || pending !== null}
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
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
