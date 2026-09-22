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

// De quoi passer à la colonne suivante sans refermer : une décision se prend
// rarement seule, et rouvrir le panneau à chaque ligne coûtait un aller-retour.
export interface PanelNavigation {
  position: number;
  total: number;
  onPrevious?(): void;
  onNext?(): void;
}

interface Props {
  // Le nom de la colonne, en tête du panneau.
  title: string;
  // Où elle vit : schema.table, ou la collection.
  location?: string;
  badges?: ReactNode;
  // Une ligne sous l'en-tête : d'où vient cette colonne, quel run l'a ajoutée.
  meta?: ReactNode;
  navigation?: PanelNavigation;
  onClose(): void;
  children: ReactNode;
  footer: ReactNode;
}

// Le panneau accosté où se décide une colonne. Même cadre sur la page Source et
// dans l'onglet Review : seuls son contenu et ses actions changent.
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
