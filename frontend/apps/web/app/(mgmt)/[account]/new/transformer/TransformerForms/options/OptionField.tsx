'use client';
import { Input } from '@/components/ui/input';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { Switch } from '@/components/ui/switch';
import { DescField, DescMessage, MessageShape } from '@bufbuild/protobuf';
import { ReactElement, ReactNode, useState } from 'react';
import { TransformerConfigProps } from '../util';
import {
  formatNumber,
  formatStringList,
  getEnumChoices,
  getOptionField,
  getOptionKind,
  OptionFieldName,
  parseStringList,
  readOptionValue,
  toFieldInteger,
  withOptionValue,
} from './fields';
import OptionRow from './OptionRow';

// What describes one option, apart from the message it edits.
export interface OptionSpec<S extends DescMessage> {
  field: OptionFieldName<S>;
  label: ReactNode;
  description?: ReactNode;
  stacked?: boolean;
  // Enum fields only: the label of each value (defaults to its humanized name).
  formatEnumValue?(value: number): string;
}

export interface OptionFieldProps<S extends DescMessage>
  extends OptionSpec<S>, TransformerConfigProps<MessageShape<S>> {
  schema: S;
}

// One transformer option. The control is chosen from the protobuf descriptor of the
// field, so a boolean gets a switch, an enum a select, an int64 a number input that
// writes a bigint.
export default function OptionField<S extends DescMessage>(
  props: OptionFieldProps<S>
): ReactElement {
  const {
    schema,
    value,
    setValue,
    isDisabled,
    errors,
    field: name,
    label,
    description,
    stacked,
    formatEnumValue,
  } = props;

  const field = getOptionField(schema, name);
  const current = readOptionValue(value, name);
  const set = (fieldValue: unknown): void =>
    setValue(withOptionValue(schema, value, name, fieldValue));

  const error = (
    errors as Record<string, { message?: unknown } | undefined> | undefined
  )?.[name]?.message;

  return (
    <OptionRow
      label={label}
      description={description}
      error={typeof error === 'string' ? error : undefined}
      stacked={stacked}
    >
      <OptionControl
        field={field}
        current={current}
        set={set}
        isDisabled={isDisabled}
        stacked={stacked}
        formatEnumValue={formatEnumValue}
      />
    </OptionRow>
  );
}

interface ControlProps {
  field: DescField;
  current: unknown;
  set(fieldValue: unknown): void;
  isDisabled?: boolean;
  stacked?: boolean;
  formatEnumValue?(value: number): string;
}

function OptionControl(props: ControlProps): ReactElement {
  const { field, current, set, isDisabled, stacked, formatEnumValue } = props;
  const width = stacked ? 'w-full' : 'w-[300px]';
  const kind = getOptionKind(field);

  switch (kind) {
    case 'bool':
      return (
        <Switch
          checked={current === true}
          onCheckedChange={set}
          disabled={isDisabled}
        />
      );
    case 'integer':
    case 'float':
      return (
        <NumberOption
          className={width}
          field={field}
          isInteger={kind === 'integer'}
          current={current}
          set={set}
          isDisabled={isDisabled}
        />
      );
    case 'string':
      return (
        <Input
          className={width}
          type="text"
          value={typeof current === 'string' ? current : ''}
          onChange={(e) => set(e.target.value)}
          disabled={isDisabled}
        />
      );
    case 'enum':
      return (
        <Select
          disabled={isDisabled}
          onValueChange={(selected) => set(parseInt(selected, 10))}
          value={typeof current === 'number' ? current.toString() : undefined}
        >
          <SelectTrigger className={width}>
            <SelectValue />
          </SelectTrigger>
          <SelectContent>
            {getEnumChoices(field, formatEnumValue).map((choice) => (
              <SelectItem
                key={choice.number}
                className="cursor-pointer"
                value={choice.number.toString()}
              >
                {choice.label}
              </SelectItem>
            ))}
          </SelectContent>
        </Select>
      );
    case 'stringList':
      return (
        <StringListInput
          className={width}
          list={Array.isArray(current) ? (current as string[]) : []}
          set={set}
          isDisabled={isDisabled}
        />
      );
  }
}

interface NumberOptionProps {
  className: string;
  field: DescField;
  isInteger: boolean;
  current: unknown;
  set(fieldValue: unknown): void;
  isDisabled?: boolean;
}

// A number, kept as typed while it is being typed. Emptying the box is a legitimate step
// between two values, and writing nothing to the config there left the box and the config
// saying different things: the config kept its old value, and since it had not changed,
// nothing redrew the box either — it stayed blank, Apply stayed disabled (it compares the
// draft to the stored config), and the old value came back at the next render from elsewhere.
// The box goes back to what the config holds as soon as it loses focus.
function NumberOption(props: NumberOptionProps): ReactElement {
  const { className, field, isInteger, current, set, isDisabled } = props;
  const value = formatNumber(current);

  // null : la case montre la config. Une chaîne : ce qui est tapé, pas encore un nombre.
  const [typed, setTyped] = useState<string | null>(null);
  const [syncedWith, setSyncedWith] = useState(value);
  if (syncedWith !== value) {
    // La config a changé d'ailleurs (un autre transformer, une remise à zéro).
    setSyncedWith(value);
    setTyped(null);
  }

  return (
    <Input
      className={className}
      type="number"
      value={typed ?? value}
      onChange={(e) => {
        setTyped(e.target.value);
        const n = e.target.valueAsNumber;
        if (isNaN(n)) {
          return;
        }
        const next = isInteger ? toFieldInteger(field, n) : n;
        setSyncedWith(formatNumber(next));
        set(next);
      }}
      onBlur={() => setTyped(null)}
      disabled={isDisabled}
    />
  );
}

interface StringListInputProps {
  className: string;
  list: string[];
  set(list: string[]): void;
  isDisabled?: boolean;
}

// A comma-separated list. The text is kept as typed: rebuilding it from the parsed
// list would swallow a trailing comma, and with it the next item.
function StringListInput(props: StringListInputProps): ReactElement {
  const { className, list, set, isDisabled } = props;
  const joined = list.join(',');

  const [text, setText] = useState(() => formatStringList(list));
  const [syncedWith, setSyncedWith] = useState(joined);
  if (syncedWith !== joined) {
    // The list changed from outside (a reset, another config loaded).
    setSyncedWith(joined);
    if (parseStringList(text).join(',') !== joined) {
      setText(formatStringList(list));
    }
  }

  return (
    <Input
      className={className}
      type="text"
      value={text}
      onChange={(e) => {
        const next = parseStringList(e.target.value);
        setText(e.target.value);
        setSyncedWith(next.join(','));
        set(next);
      }}
      disabled={isDisabled}
    />
  );
}
