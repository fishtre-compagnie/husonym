'use client';

import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import {
  isUsagePeriod,
  periodLabel,
  USAGE_PERIODS,
  UsagePeriod,
} from '@/libs/usage/period';
import { ReactElement } from 'react';

interface Props {
  period: UsagePeriod;
  setPeriod(period: UsagePeriod): void;
}

// The period a Usage page reads.
export default function UsagePeriodSelect(props: Props): ReactElement {
  const { period, setPeriod } = props;
  return (
    <Select
      value={period}
      onValueChange={(value) => {
        if (isUsagePeriod(value)) {
          setPeriod(value);
        }
      }}
    >
      <SelectTrigger aria-label="Period" className="w-44">
        <SelectValue />
      </SelectTrigger>
      <SelectContent>
        {USAGE_PERIODS.map((value) => (
          <SelectItem key={value} value={value} className="cursor-pointer">
            {periodLabel(value)}
          </SelectItem>
        ))}
      </SelectContent>
    </Select>
  );
}
