import ButtonText from '@/components/ButtonText';
import Spinner from '@/components/Spinner';
import { Button } from '@/components/ui/button';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogFooter,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { Label } from '@/components/ui/label';
import { Textarea } from '@/components/ui/textarea';
import { changeLabel, columnName } from '@/util/mapping-changes';
import { JobMappingChange } from '@husonym/sdk';
import { useTranslations } from 'next-intl';
import { ReactElement, useState } from 'react';

interface Props {
  open: boolean;
  onOpenChange(open: boolean): void;
  changes: JobMappingChange[];
  onConfirm(changes: JobMappingChange[], note?: string): Promise<void>;
}

// Marks a selection of changes reviewed as the runs left them, with one note for all of them.
//
// One dialog and one note for the whole selection: the trace an audit asks for is kept, without
// thirty confirmations in a row — by the fifth, people click without reading, which is exactly
// what asking was meant to prevent.
export default function ReviewChangesDialog(props: Props): ReactElement {
  const { open, onOpenChange, changes, onConfirm } = props;
  const t = useTranslations('jobs.review.dialog');
  const tKinds = useTranslations('jobs.review.changeKinds');
  const [note, setNote] = useState('');
  const [isConfirming, setIsConfirming] = useState(false);

  // The note is the trace an audit reads: it belongs to the changes it was typed for. The
  // component stays mounted for the page's lifetime, so a note left behind by a Cancel, an
  // Escape or a click outside would be attached, silently, to the next selection confirmed.
  function close(open: boolean): void {
    if (!open) {
      setNote('');
    }
    onOpenChange(open);
  }

  async function confirm(): Promise<void> {
    setIsConfirming(true);
    try {
      await onConfirm(changes, note.trim() || undefined);
      close(false);
    } catch {
      // The caller has already said what went wrong. The dialog stays open, so the note is not
      // lost and the person can try again.
    } finally {
      setIsConfirming(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={close}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>{t('title', { count: changes.length })}</DialogTitle>
          <DialogDescription>
            {t('description', { count: changes.length })}
          </DialogDescription>
        </DialogHeader>
        <ul className="max-h-40 overflow-auto text-xs font-mono flex flex-col gap-1">
          {changes.map((c) => (
            <li key={c.id}>
              {columnName(c)} — {changeLabel(c, tKinds)}
            </li>
          ))}
        </ul>
        <div className="flex flex-col gap-2">
          <Label htmlFor="review-changes-note">{t('note')}</Label>
          <Textarea
            id="review-changes-note"
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder={t('notePlaceholder')}
          />
        </div>
        <DialogFooter>
          <Button type="button" variant="outline" onClick={() => close(false)}>
            {t('cancel')}
          </Button>
          <Button
            type="button"
            disabled={isConfirming}
            onClick={() => confirm()}
          >
            <ButtonText
              leftIcon={isConfirming ? <Spinner className="h-4 w-4" /> : null}
              text={t('confirm')}
            />
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
