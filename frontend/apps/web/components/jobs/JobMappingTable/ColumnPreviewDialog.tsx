import { Badge } from '@/components/ui/badge';
import {
  Dialog,
  DialogContent,
  DialogDescription,
  DialogHeader,
  DialogTitle,
} from '@/components/ui/dialog';
import { TransformerConfig } from '@husonym/sdk';
import { ReactElement } from 'react';
import ColumnPreview from './ColumnPreview';

interface Props {
  open: boolean;
  onOpenChange(open: boolean): void;
  connectionId: string;
  schema: string;
  table: string;
  column: string;
  dataType?: string;
  // Nombre de valeurs demandées.
  limit?: number;
  // Transformer à essayer sur ces valeurs. Sans lui, la fenêtre montre la colonne telle quelle ;
  // avec lui, chaque valeur à côté de ce que le transformer en fait.
  transformer?: TransformerConfig;
  transformerName?: string;
}

// L'aperçu d'une colonne dans une fenêtre, pour les pages qui n'ont pas la place de l'afficher.
export default function ColumnPreviewDialog({
  open,
  onOpenChange,
  connectionId,
  schema,
  table,
  column,
  dataType,
  limit,
  transformer,
  transformerName,
}: Props): ReactElement {
  return (
    <Dialog open={open} onOpenChange={onOpenChange}>
      <DialogContent className={transformer ? 'max-w-4xl' : 'max-w-2xl'}>
        <DialogHeader>
          <DialogTitle className="flex items-center gap-2">
            <span className="font-mono text-base">{column}</span>
            {dataType && (
              <Badge
                variant="outline"
                className="font-mono text-xs font-normal"
              >
                {dataType}
              </Badge>
            )}
          </DialogTitle>
          <DialogDescription className="font-mono text-xs">
            {schema}.{table}
            {transformer && (
              <span className="font-sans">
                {' '}
                — aperçu avec {transformerName ?? 'le transformer choisi'}
              </span>
            )}
          </DialogDescription>
        </DialogHeader>
        <ColumnPreview
          enabled={open}
          connectionId={connectionId}
          schema={schema}
          table={table}
          column={column}
          limit={limit}
          transformer={transformer}
        />
      </DialogContent>
    </Dialog>
  );
}
