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
  target: PassthroughTarget;
  onAccept(target: PassthroughTarget, note?: string): Promise<void>;
}

// Accepts that one unmapped column ships untransformed, so it stops being reported.
//
// It asks before it writes, through a dialog rather than a single click, on purpose: agreeing
// that a column leaves the source in clear is the kind of decision somebody will be asked about
// later, and a button that does it on the first click is a button people press to make a warning
// go away. The note is optional and is the thing an audit actually asks for.
export default function AcceptPassthroughButton(props: Props): ReactElement {
  const { target, onAccept } = props;
  const [open, setOpen] = useState(false);
  const [note, setNote] = useState('');
  const [isAccepting, setIsAccepting] = useState(false);

  async function onConfirm(): Promise<void> {
    setIsAccepting(true);
    try {
      await onAccept(target, note.trim() || undefined);
      setOpen(false);
      setNote('');
    } finally {
      // The caller reports the failure; this only has to stop showing a spinner, including when
      // the write failed and the dialog stays open so the note is not lost.
      setIsAccepting(false);
    }
  }

  return (
    <>
      <Button
        type="button"
        variant="outline"
        size="sm"
        className="h-6 text-xs shrink-0"
        onClick={() => setOpen(true)}
      >
        Accept
      </Button>
      <Dialog open={open} onOpenChange={setOpen}>
        <DialogContent>
          <DialogHeader>
            <DialogTitle>Accept this passthrough</DialogTitle>
            <DialogDescription>
              <span className="font-mono text-xs">
                {target.schema}.{target.table}.{target.column}
              </span>{' '}
              will keep being copied to the destination untransformed, and will
              stop being reported for this job. It comes back if the column
              changes, or if it starts to read as personal data.
            </DialogDescription>
          </DialogHeader>
          <div className="flex flex-col gap-2">
            <Label htmlFor="accept-passthrough-note">
              Why is this acceptable? (optional)
            </Label>
            <Textarea
              id="accept-passthrough-note"
              value={note}
              onChange={(e) => setNote(e.target.value)}
              placeholder="e.g. internal reference, holds no personal data"
            />
          </div>
          <DialogFooter>
            <Button
              type="button"
              variant="outline"
              onClick={() => setOpen(false)}
            >
              Cancel
            </Button>
            <Button type="button" onClick={() => onConfirm()}>
              <ButtonText
                leftIcon={isAccepting ? <Spinner className="h-4 w-4" /> : null}
                text="Accept"
              />
            </Button>
          </DialogFooter>
        </DialogContent>
      </Dialog>
    </>
  );
}
