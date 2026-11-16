'use client';
import { useFormatDateTime } from '@/i18n/date-fns';
import { ReactElement } from 'react';

interface Props {
  value?: string | Date | number;
  is24Hour?: boolean;
}

// A date and time in the UI language, for the cells of a table whose columns are
// built outside a component.
export default function DateTime(props: Props): ReactElement {
  const { value, is24Hour } = props;
  const formatDateTime = useFormatDateTime();
  return <>{formatDateTime(value, is24Hour)}</>;
}
