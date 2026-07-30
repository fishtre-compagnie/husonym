import { Button } from '@/components/ui/button';
import { ReactElement, useState } from 'react';
// Œil Lucide plutôt que celui de @radix-ui/react-icons : tracé plus fin et plus
// lisible à cette taille, iris nettement dessiné.
import { LuEye } from 'react-icons/lu';
import ColumnPreviewDialog from './ColumnPreviewDialog';

interface Props {
  connectionId: string;
  schema: string;
  table: string;
  column: string;
  dataType?: string;
}

// Bouton d'aperçu d'une colonne. Composant à part car il porte l'état
// d'ouverture : une fonction `cell` de TanStack ne peut pas héberger de hooks.
// La fenêtre n'est montée qu'à l'ouverture, sinon un tableau de 200 colonnes
// instancierait 200 dialogues.
export default function ColumnPreviewButton({
  connectionId,
  schema,
  table,
  column,
  dataType,
}: Props): ReactElement {
  const [open, setOpen] = useState(false);
  return (
    <>
      <Button
        type="button"
        variant="ghost"
        size="icon"
        className="text-muted-foreground hover:text-foreground hover:bg-muted h-9 w-9"
        title={`Voir les données de « ${column} »`}
        aria-label={`Voir les données de la colonne ${column}`}
        onClick={() => setOpen(true)}
      >
        <LuEye className="h-[22px] w-[22px]" strokeWidth={1.75} />
      </Button>
      {open && (
        <ColumnPreviewDialog
          open={open}
          onOpenChange={setOpen}
          connectionId={connectionId}
          schema={schema}
          table={table}
          column={column}
          dataType={dataType}
        />
      )}
    </>
  );
}
