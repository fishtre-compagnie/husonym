'use client';

import * as TogglePrimitive from '@radix-ui/react-toggle';
import { cva, type VariantProps } from 'class-variance-authority';
import * as React from 'react';

import { cn } from '@/libs/utils';

export const toggleVariants = cva(
  'inline-flex items-center justify-center rounded-lg text-sm font-medium transition-colors hover:bg-hover hover:text-foreground focus-visible:outline-hidden focus-visible:ring-1 focus-visible:ring-ring disabled:pointer-events-none disabled:opacity-50 data-[state=on]:bg-surface-2 data-[state=on]:text-hi cursor-pointer',
  {
    variants: {
      variant: {
        default: 'bg-transparent',
        outline:
          'border border-input bg-card hover:bg-hover hover:text-foreground',
      },
      size: {
        default: 'h-9 px-3',
        sm: 'h-8 px-2',
        lg: 'h-10 px-3',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  }
);

export function Toggle({
  className,
  variant,
  size,
  ...props
}: React.ComponentPropsWithRef<typeof TogglePrimitive.Root> &
  VariantProps<typeof toggleVariants>) {
  return (
    <TogglePrimitive.Root
      className={cn(toggleVariants({ variant, size, className }))}
      {...props}
    />
  );
}
