import { Button } from '@/components/ui/button';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetFooter,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet';
import { ChevronDownIcon, ChevronUpIcon } from '@radix-ui/react-icons';
import { ReactElement, ReactNode } from 'react';

export interface PanelNavigation {
  position: number;
  total: number;
  onPrevious?(): void;
  onNext?(): void;
}

interface Props {
  title: string;
  location?: string;
  badges?: ReactNode;
  meta?: ReactNode;
  navigation?: PanelNavigation;
  onClose(): void;
  children: ReactNode;
  footer: ReactNode;
}

// Le cadre commun à la page Source et à l'onglet Review.
export default function ColumnDecisionPanel(props: Props): ReactElement {
  const {
    title,
    location,
    badges,
    meta,
    navigation,
    onClose,
    children,
    footer,
  } = props;

  return (
    <Sheet open onOpenChange={(open) => !open && onClose()}>
      <SheetContent className="w-full sm:max-w-3xl overflow-y-auto flex flex-col gap-6">
        <SheetHeader>
          <div className="flex flex-row items-center gap-2">
            {location && (
              <span className="text-xs text-muted-foreground">{location}</span>
            )}
            <div className="grow" />
            {navigation && (
              <div className="flex flex-row items-center gap-2">
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  className="h-7 w-7"
                  aria-label="Previous column"
                  disabled={!navigation.onPrevious}
                  onClick={navigation.onPrevious}
                >
                  <ChevronUpIcon />
                </Button>
                <Button
                  type="button"
                  variant="outline"
                  size="icon"
                  className="h-7 w-7"
                  aria-label="Next column"
                  disabled={!navigation.onNext}
                  onClick={navigation.onNext}
                >
                  <ChevronDownIcon />
                </Button>
                <span className="text-xs text-muted-foreground">
                  {navigation.position} of {navigation.total}
                </span>
              </div>
            )}
          </div>
          <SheetTitle className="font-mono text-base">{title}</SheetTitle>
          {badges && (
            <SheetDescription asChild>
              <div className="flex flex-wrap items-center gap-2">{badges}</div>
            </SheetDescription>
          )}
          {meta && <p className="text-xs text-muted-foreground">{meta}</p>}
        </SheetHeader>

        {children}

        <SheetFooter className="flex flex-row justify-end gap-2">
          {footer}
        </SheetFooter>
      </SheetContent>
    </Sheet>
  );
}
