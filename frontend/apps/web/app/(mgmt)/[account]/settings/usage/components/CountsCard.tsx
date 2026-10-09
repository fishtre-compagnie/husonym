import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { ReactElement } from 'react';

interface Props {
  title: string;
  description?: string;
  // What is counted, and the unit it is counted in.
  columns: [string, string];
  rows: readonly { key: string; label: string; count: string }[];
  // What the card says when it has no row.
  emptyLine?: string;
}

// A card that counts things by name: the errors by category, the refusals by gate.
export default function CountsCard(props: Props): ReactElement {
  const { title, description, columns, rows, emptyLine } = props;
  return (
    <Card>
      <CardHeader>
        <CardTitle>{title}</CardTitle>
        {description && <CardDescription>{description}</CardDescription>}
      </CardHeader>
      <CardContent>
        {rows.length === 0 ? (
          <p className="text-sm text-muted-foreground">{emptyLine}</p>
        ) : (
          <Table>
            <TableHeader>
              <TableRow>
                <TableHead scope="col">{columns[0]}</TableHead>
                <TableHead scope="col" className="text-right">
                  {columns[1]}
                </TableHead>
              </TableRow>
            </TableHeader>
            <TableBody>
              {rows.map((row) => (
                <TableRow key={row.key}>
                  <TableCell>{row.label}</TableCell>
                  <TableCell className="text-right tabular-nums">
                    {row.count}
                  </TableCell>
                </TableRow>
              ))}
            </TableBody>
          </Table>
        )}
      </CardContent>
    </Card>
  );
}
