'use client';
import FormErrorMessage from '@/components/FormErrorMessage';
import { FormDescription, FormLabel } from '@/components/ui/form';
import { ReactElement, ReactNode } from 'react';

interface Props {
  label: ReactNode;
  description?: ReactNode;
  error?: string;
  // Puts the control under the label, at full width, for long free-text values.
  stacked?: boolean;
  children: ReactNode;
}

// The frame every transformer option shares: label and description, then the control
// and its error.
export default function OptionRow(props: Props): ReactElement {
  const { label, description, error, stacked, children } = props;

  const heading = (
    <div className={stacked ? 'space-y-0.5' : 'space-y-0.5 w-[80%]'}>
      <FormLabel>{label}</FormLabel>
      {description && <FormDescription>{description}</FormDescription>}
    </div>
  );

  if (stacked) {
    return (
      <div className="flex flex-col w-full space-y-4 rounded-lg border dark:border-gray-700 p-3 shadow-xs">
        {heading}
        <div className="flex flex-col items-start">
          {children}
          <FormErrorMessage message={error} />
        </div>
      </div>
    );
  }

  return (
    <div className="flex flex-row items-center justify-between gap-4 rounded-lg border dark:border-gray-700 p-3 shadow-xs">
      {heading}
      <div className="flex flex-col">
        <div className="justify-end flex">{children}</div>
        <FormErrorMessage message={error} />
      </div>
    </div>
  );
}
