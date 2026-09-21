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
import { ReactElement, useState } from 'react';

// The column a decision is about.
export interface PassthroughTarget {
  schema: string;
  table: string;
  column: string;
}

interface Props {
  open: boolean;
  onOpenChange(open: boolean): void;
  targets: PassthroughTarget[];
  onAccept(targets: PassthroughTarget[], note?: string): Promise<void>;
}

// Accepts that the selected columns ship untransformed, so they stop being reported.
//
// One dialog and one note for the whole selection: the trace an audit asks for is kept, without
// thirty confirmations in a row — by the fifth, people click without reading, which is exactly
// what asking was meant to prevent. It still asks, because agreeing that data leaves the source
// in clear is a decision somebody will be asked about later.
export default function AcceptPassthroughsDialog(props: Props): ReactElement {
  const { open, onOpenChange, targets, onAccept } = props;
  const [note, setNote] = useState('');
  const [isAccepting, setIsAccepting] = useState(false);

  async function onConfirm(): Promise<void> {
    setIsAccepting(true);
    try {
      await onAccept(targets, note.trim() || undefined);
      setNote('');
      onOpenChange(false);
    } catch {
      // The caller has already said what went wrong. The dialog stays open, so the note is not
      // lost and the person can try again.
    } finally {
      setIsAccepting(false);
    }
  }

  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent>
        <DialogHeader>
          <DialogTitle>
            {targets.length === 1
              ? 'Accept this passthrough'
              : `Accept ${targets.length} passthroughs`}
          </DialogTitle>
          <DialogDescription>
            {targets.length === 1 ? 'This column' : 'These columns'} will keep
            being copied to the destination untransformed, and will stop being
            reported for this job. A column comes back if it changes, or if it
            starts to read as personal data.
          </DialogDescription>
        </DialogHeader>
        <ul className="max-h-40 overflow-auto text-xs font-mono flex flex-col gap-1">
          {targets.map((t) => (
            <li key={`${t.schema}.${t.table}.${t.column}`}>
              {t.schema}.{t.table}.{t.column}
            </li>
          ))}
        </ul>
        <div className="flex flex-col gap-2">
          <Label htmlFor="accept-passthroughs-note">
            Why is this acceptable? (optional)
          </Label>
          <Textarea
            id="accept-passthroughs-note"
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
            disabled={isAccepting}
            onClick={() => onConfirm()}
          >
            <ButtonText
              leftIcon={isAccepting ? <Spinner className="h-4 w-4" /> : null}
              text="Accept"
            />
          </Button>
        </DialogFooter>
      </DialogContent>
    </Dialog>
  );
}
