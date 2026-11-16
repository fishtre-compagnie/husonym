import { Segmented } from '@/components/ds';
import { cn } from '@/libs/utils';
import {
  ColumnRemovalStrategy,
  NewColumnAdditionStrategy,
} from '@/yup-validations/jobs';
import {
  ExclamationTriangleIcon,
  ExternalLinkIcon,
  InfoCircledIcon,
} from '@radix-ui/react-icons';
import { useTranslations } from 'next-intl';
import Link from 'next/link';
import { ReactElement, ReactNode } from 'react';

interface Props {
  newColumn: NewColumnAdditionStrategy;
  removedColumn: ColumnRemovalStrategy;
  onNewColumnChange(strategy: NewColumnAdditionStrategy): void;
  onRemovedColumnChange(strategy: ColumnRemovalStrategy): void;
}

interface Choice<T extends string> {
  value: T;
  // A choice that may copy personal data in clear is told in the warning tone.
  warn?: boolean;
}

// Each choice has its label and its effect (what a run does with the column, told
// under the choice once it is made) in the messages, under its value.
const NEW_COLUMN: Choice<NewColumnAdditionStrategy>[] = [
  { value: 'halt' },
  { value: 'automap' },
  { value: 'passthrough', warn: true },
  { value: 'continue' },
];

const REMOVED_COLUMN: Choice<ColumnRemovalStrategy>[] = [
  { value: 'halt' },
  { value: 'continue' },
];

/**
 * What a run does when the schema of the source no longer matches the mapping:
 * a column that appeared, a mapped column that disappeared.
 */
export default function SchemaChangeStrategiesForm(props: Props): ReactElement {
  const { newColumn, removedColumn, onNewColumnChange, onRemovedColumnChange } =
    props;
  const t = useTranslations('mapping.form.schemaChange');
  return (
    <div className="flex flex-col gap-5">
      <div className="flex flex-col gap-1">
        <h3 className="m-0 font-display text-[15px] font-semibold">
          {t('title')}
        </h3>
        <p className="m-0 text-[13px] text-muted-foreground">
          {t('description')}
        </p>
      </div>
      <StrategyRow
        label={t('newColumn')}
        learnMore="https://docs.husonym.com/guides/new-column-addition-strategies"
        choices={NEW_COLUMN.map((c) => ({
          ...c,
          label: t(`newColumnChoices.${c.value}.label`),
          effect: t(`newColumnChoices.${c.value}.effect`),
        }))}
        value={newColumn}
        onChange={onNewColumnChange}
      />
      <StrategyRow
        label={t('removedColumn')}
        learnMore="https://docs.husonym.com/guides/column-removal-strategies"
        choices={REMOVED_COLUMN.map((c) => ({
          ...c,
          label: t(`removedColumnChoices.${c.value}.label`),
          effect: t(`removedColumnChoices.${c.value}.effect`),
        }))}
        value={removedColumn}
        onChange={onRemovedColumnChange}
      />
    </div>
  );
}

function StrategyRow<T extends string>(props: {
  label: string;
  learnMore: string;
  choices: (Choice<T> & { label: string; effect: string })[];
  value: T;
  onChange(value: T): void;
}): ReactElement {
  const { label, learnMore, choices, value, onChange } = props;
  const chosen = choices.find((c) => c.value === value);
  const t = useTranslations('mapping.form.schemaChange');
  return (
    <div className="grid gap-x-6 gap-y-2 md:grid-cols-[220px_1fr]">
      <div className="flex flex-col gap-0.5 pt-1.5">
        <span className="text-sm font-medium">{label}</span>
        <Link
          href={learnMore}
          target="_blank"
          className="inline-flex w-fit items-center gap-1 text-[12px] text-muted-foreground hover:underline"
        >
          {t('learnMore')}
          <ExternalLinkIcon className="size-3" />
        </Link>
      </div>
      <div className="flex min-w-0 flex-col gap-2">
        <Segmented
          ariaLabel={label}
          options={choices.map((c) => ({ value: c.value, label: c.label }))}
          value={value}
          onChange={onChange}
          className="w-fit max-w-full flex-wrap"
        />
        {chosen && <Effect warn={chosen.warn}>{chosen.effect}</Effect>}
      </div>
    </div>
  );
}

function Effect(props: { warn?: boolean; children: ReactNode }): ReactElement {
  const Icon = props.warn ? ExclamationTriangleIcon : InfoCircledIcon;
  return (
    <p
      aria-live="polite"
      className={cn(
        'm-0 flex max-w-2xl items-start gap-2 rounded-lg px-3 py-2 text-[13px]',
        props.warn
          ? 'bg-warn-soft text-warn-fg'
          : 'bg-surface-2 text-muted-foreground'
      )}
    >
      <Icon className="mt-0.5 size-3.5 shrink-0" />
      <span>{props.children}</span>
    </p>
  );
}
