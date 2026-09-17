import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import { ConsistencyScope } from '@husonym/sdk';
import { ReactElement } from 'react';

interface Props {
  value?: number;
  onChange(value: ConsistencyScope): void;
}

// Reach of deterministic consistency on the Athanor engine. Anything wider than a
// single run keeps outputs linkable over time, which is pseudonymization rather
// than anonymization under the GDPR: the default stays at the run.
export default function ConsistencyScopeSelect({
  value,
  onChange,
}: Props): ReactElement {
  return (
    <Select
      onValueChange={(v) => onChange(Number(v))}
      value={String(value ?? ConsistencyScope.UNSPECIFIED)}
    >
      <SelectTrigger>
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        <SelectItem value={String(ConsistencyScope.UNSPECIFIED)}>
          Run (default)
        </SelectItem>
        <SelectItem value={String(ConsistencyScope.JOB)}>
          Job — same outputs on every run of this job
        </SelectItem>
        <SelectItem value={String(ConsistencyScope.ACCOUNT)}>
          Account — same outputs across all jobs (pseudonymization)
        </SelectItem>
      </SelectContent>
    </Select>
  );
}
