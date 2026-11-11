import { getSystemAppConfig } from '@/app/api/config/config';
import Breadcrumb from '@/components/shell/Breadcrumb';
import CommandPalette from '@/components/shell/CommandPalette';
import NewJobButton from '@/components/shell/NewJobButton';
import { HelpIcon } from '@/components/shell/icons';
import { topbarIconButtonClass } from '@/components/shell/styles';
import { Button } from '@/components/ui/button';
import {
  Sheet,
  SheetClose,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
  SheetTrigger,
} from '@/components/ui/sheet';
import { ReactElement } from 'react';
import SupportDrawer from '../SupportDrawer';
import { AccountStatusHandler } from './AccountStatusHandler';
import HusonymVersion from './HusonymVersion';
import { MobileNav } from './MobileNav';
import { ModeToggle } from './ModeToggle';
import PendingReviewsBell from './PendingReviewsBell';

/**
 * The bar on top of every page: where the page is, the search, the account's status,
 * and the buttons always at hand. The sections themselves live in the sidebar.
 */
export default function SiteHeader(): ReactElement {
  const systemAppConfig = getSystemAppConfig();

  return (
    <header className="vt-header sticky top-0 z-40 flex flex-wrap items-center gap-2 border-b border-line-soft bg-header px-4 py-3 backdrop-blur-md sm:gap-3 sm:px-8 sm:py-3.5">
      <MobileNav isAuthEnabled={systemAppConfig.isAuthEnabled} />
      <Breadcrumb />
      <div className="ml-auto flex items-center gap-2 sm:gap-3">
        <CommandPalette />
        <AccountStatusHandler systemAppConfig={systemAppConfig} />
        <ModeToggle />
        <PendingReviewsBell />
        <SupportSheet />
        <NewJobButton />
      </div>
    </header>
  );
}

function SupportSheet(): ReactElement {
  return (
    <Sheet>
      <SheetTrigger asChild>
        <button
          type="button"
          aria-label="Support"
          className={topbarIconButtonClass}
        >
          <HelpIcon size={18} />
        </button>
      </SheetTrigger>
      <SheetContent className="w-[400px] sm:w-[540px]">
        <SheetHeader>
          <div className="flex flex-col md:flex-row gap-2 items-center justify-between">
            <SheetTitle>Support</SheetTitle>
            <div>
              <HusonymVersion className="font-mono text-xs text-muted-foreground" />
            </div>
          </div>
          <SheetDescription>Need help? We got you covered.</SheetDescription>
        </SheetHeader>
        <SupportDrawer />
        <SheetFooter className="pt-20">
          <SheetClose asChild>
            <Button>Close</Button>
          </SheetClose>
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
