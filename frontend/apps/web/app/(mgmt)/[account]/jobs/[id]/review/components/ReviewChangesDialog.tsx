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
  const [note, setNote] = useState('');
  const [isConfirming, setIsConfirming] = useState(false);

  async function confirm(): Promise<void> {
    setIsConfirming(true);
    try {
      await onConfirm(changes, note.trim() || undefined);
      setNote('');
      onOpenChange(false);
    } catch {
      // The caller has already said what went wrong. The dialog stays open, so the note is not
      // lost and the person can try again.
    } finally {
      setIsConfirming(false);
    }
  }

  const many = changes.length !== 1;

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {many
              ? `Mark ${changes.length} changes reviewed`
              : 'Mark this change reviewed'}
          </DialogTitle>
          <DialogDescription>
            The job keeps its mappings as the runs left them, and{' '}
            {many ? 'these changes stop' : 'this change stops'} being reported.
          </DialogDescription>
        </DialogHeader>
        <ul className="max-h-40 overflow-auto text-xs font-mono flex flex-col gap-1">
          {changes.map((c) => (
            <li key={c.id}>
              {columnName(c)} — {changeLabel(c)}
            </li>
          ))}
        </ul>
        <div className="flex flex-col gap-2">
          <Label htmlFor="review-changes-note">
            Why is this fine? (optional)
          </Label>
          <Textarea
            id="review-changes-note"
            value={note}
            onChange={(e) => setNote(e.target.value)}
            placeholder="e.g. internal references, hold no personal data"
          />
        </div>
        <DialogFooter>
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
          >
            Cancel
          </Button>
          <Button
            type="button"
            disabled={isConfirming}
            onClick={() => confirm()}
          >
            <ButtonText
              leftIcon={isConfirming ? <Spinner className="h-4 w-4" /> : null}
              text="Mark reviewed"
            />
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
