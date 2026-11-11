import { Slot } from '@radix-ui/react-slot';
import { cva, type VariantProps } from 'class-variance-authority';
import * as React from 'react';

import { cn } from '@/libs/utils';

export const buttonVariants = cva(
  'inline-flex items-center justify-center gap-2 rounded-[10px] text-sm font-medium transition-[color,background-color,border-color,filter] disabled:pointer-events-none disabled:opacity-50 cursor-pointer',
  {
    variants: {
      variant: {
        default:
          'bg-primary text-primary-foreground font-semibold hover:brightness-110',
        destructive:
          'bg-destructive text-destructive-foreground font-semibold hover:brightness-110',
        warning:
          'bg-warning text-warning-foreground font-semibold hover:brightness-110',
        outline: 'border border-input bg-card text-foreground hover:bg-hover',
        secondary:
          'bg-secondary text-secondary-foreground hover:bg-secondary/80',
        ghost: 'hover:bg-hover hover:text-foreground overflow-hidden',
        link: 'text-link underline-offset-4 hover:underline',
        success: 'bg-ok text-primary-foreground font-semibold',
      },
      size: {
        default: 'h-10 px-4',
        sm: 'h-8 rounded-lg px-3 text-xs',
        lg: 'h-11 px-8',
        icon: 'h-10 w-10',
      },
    },
    defaultVariants: {
      variant: 'default',
      size: 'default',
    },
  }
);

export interface ButtonProps
  extends
    React.ComponentPropsWithRef<'button'>,
    VariantProps<typeof buttonVariants> {
  asChild?: boolean;
}

export function Button({
  className,
  variant,
  size,
  asChild = false,
  ...props
}: ButtonProps) {
  const Comp = asChild ? Slot : 'button';
  return (
    <Comp
      className={cn(buttonVariants({ variant, size, className }))}
      {...props}
    />
  );
}
