import { cva, type VariantProps } from 'class-variance-authority';
import * as React from 'react';

import { cn } from '@/libs/utils';

const alertVariants = cva(
  'relative w-full rounded-xl border border-border px-4 py-3 text-sm [&>svg+div]:translate-y-[-3px] [&>svg]:absolute [&>svg]:left-4 [&>svg]:top-4 [&>svg]:text-foreground [&>svg~*]:pl-7',
  {
    variants: {
      variant: {
        default: 'bg-card text-foreground',
        success: 'border-ok/40 bg-ok-soft text-foreground [&>svg]:text-ok',
        destructive:
          'border-danger-line bg-danger-soft text-danger-fg [&>svg]:text-danger-fg',
        warning:
          'border-warn-line bg-warn-soft text-foreground [&>svg]:text-warn-fg',
        info: 'border-line-soft bg-brand-soft text-foreground [&>svg]:text-link',
      },
    },
    defaultVariants: {
      variant: 'default',
    },
  }
);

export function Alert({
  className,
  variant,
  ...props
}: React.ComponentPropsWithRef<'div'> & VariantProps<typeof alertVariants>) {
  return (
    <div
      role="alert"
      className={cn(alertVariants({ variant }), className)}
      {...props}
    />
  );
}

export function AlertTitle({
  className,
  ...props
}: React.HTMLAttributes<HTMLHeadingElement>) {
  return (
    <h5
      className={cn('mb-1 font-medium leading-none tracking-tight', className)}
      {...props}
    />
  );
}

export function AlertDescription({
  className,
  ...props
}: React.HTMLAttributes<HTMLParagraphElement>) {
  return (
    <div
      className={cn('text-sm [&_p]:leading-relaxed', className)}
      {...props}
    />
  );
}
