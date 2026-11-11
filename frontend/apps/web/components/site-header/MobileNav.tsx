'use client';

import * as React from 'react';

import Sidebar from '@/components/shell/Sidebar';
import { MenuIcon } from '@/components/shell/icons';
import { topbarIconButtonClass } from '@/components/shell/styles';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet';
import { cn } from '@/libs/utils';

interface Props {
  isAuthEnabled: boolean;
}

// Under the lg breakpoint the sidebar folds away: this button opens it as a drawer.
export function MobileNav(props: Props) {
  const [open, setOpen] = React.useState(false);

  return (
    <Sheet open={open} onOpenChange={setOpen}>
      <SheetTrigger asChild>
        <button
          type="button"
          aria-label="Open navigation"
          className={cn(topbarIconButtonClass, 'lg:hidden')}
        >
          <MenuIcon size={18} />
        </button>
      </SheetTrigger>
      <SheetContent
        side="left"
        className="w-[272px] max-w-[85vw] overflow-y-auto border-r border-line-soft bg-sidebar p-0 sm:max-w-[272px]"
      >
        <SheetTitle className="sr-only">Navigation</SheetTitle>
        <SheetDescription className="sr-only">
          The sections of the app.
        </SheetDescription>
        <Sidebar
          isAuthEnabled={props.isAuthEnabled}
          onNavigate={() => setOpen(false)}
        />
      </SheetContent>
    </Sheet>
  );
}
