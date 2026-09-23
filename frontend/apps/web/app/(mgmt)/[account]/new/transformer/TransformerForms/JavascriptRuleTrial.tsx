'use client';
import ButtonText from '@/components/ButtonText';
import Spinner from '@/components/Spinner';
import { useAccount } from '@/components/providers/account-provider';
import { Alert, AlertDescription, AlertTitle } from '@/components/ui/alert';
import { Button } from '@/components/ui/button';
import { Input } from '@/components/ui/input';
import { Label } from '@/components/ui/label';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Textarea } from '@/components/ui/textarea';
import { create } from '@bufbuild/protobuf';
import { useMutation } from '@connectrpc/connect-query';
import {
  JavascriptRuleFailure,
  TransformerConfigSchema,
  TransformersService,
} from '@husonym/sdk';
import { ReactElement, useState } from 'react';

interface Props {
  code: string;
  // A generate rule receives no value: only the other columns of the row.
  kind: 'transform' | 'generate';
}

const defaultColumn = 'value';
const defaultRows = `[
  { "id": 1, "value": "Jane Doe" },
  { "id": 2, "value": "Jane Doe" },
  { "id": 3, "value": "John Smith" }
]`;

interface TrialRow {
  before: unknown;
  after: unknown;
}

// Essai d'une règle sur quelques lignes saisies ici, jamais lues dans une source.
// L'API l'exécute comme Athanor dans un job : les colonnes d'une ligne partagent leur
// état, la ligne suivante repart de zéro, et les fonctions pseudo.* dérivent d'une clé
// tirée pour l'essai.
export default function JavascriptRuleTrial(props: Props): ReactElement {
  const { code, kind } = props;
  const { account } = useAccount();
  const [column, setColumn] = useState(defaultColumn);
  const [rowsText, setRowsText] = useState(defaultRows);
  const [inputError, setInputError] = useState<string>();
  const [rows, setRows] = useState<TrialRow[]>();
  const [failure, setFailure] = useState<JavascriptRuleFailure>();
  const { mutateAsync: tryRules, isPending } = useMutation(
    TransformersService.method.tryJavascriptRules
  );

  async function handleTry(): Promise<void> {
    if (!account) {
      return;
    }
    setInputError(undefined);
    setRows(undefined);
    setFailure(undefined);

    let parsed: unknown;
    try {
      parsed = JSON.parse(rowsText);
    } catch (err) {
      setInputError(`The rows are not valid JSON: ${err}`);
      return;
    }
    if (
      !Array.isArray(parsed) ||
      parsed.length === 0 ||
      parsed.some((row) => typeof row !== 'object' || row === null)
    ) {
      setInputError('The rows must be an array of JSON objects.');
      return;
    }
    const sources = parsed as Record<string, unknown>[];

    try {
      const res = await tryRules({
        accountId: account.id,
        rules: [
          {
            column,
            transformer: create(TransformerConfigSchema, {
              config:
                kind === 'transform'
                  ? { case: 'transformJavascriptConfig', value: { code } }
                  : { case: 'generateJavascriptConfig', value: { code } },
            }),
          },
        ],
        rows: sources.map((row) => JSON.stringify(row)),
      });
      if (res.failure) {
        setFailure(res.failure);
        return;
      }
      setRows(
        res.rows.map((raw, i) => ({
          before: sources[i][column],
          after: (JSON.parse(raw) as Record<string, unknown>)[column],
        }))
      );
    } catch (err) {
      setInputError(`The trial could not run: ${err}`);
    }
  }

  return (
    <div className="flex flex-col gap-3 rounded-lg border dark:border-gray-700 p-3 mt-4">
      <div className="space-y-0.5">
        <Label>Try the rule</Label>
        <div className="text-sm text-muted-foreground">
          The rule runs on the rows below as it would in an Athanor job: its
          state does not carry from one row to the next. The pseudo.* functions
          derive here from a key drawn for the trial, so their outputs have the
          shape of a run&apos;s, not its values.
        </div>
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="rule-trial-column">Transformed column</Label>
        <Input
          id="rule-trial-column"
          value={column}
          onChange={(e) => setColumn(e.target.value)}
          className="max-w-xs"
        />
      </div>
      <div className="flex flex-col gap-1">
        <Label htmlFor="rule-trial-rows">
          Trial rows (JSON array, 20 at most)
        </Label>
        <Textarea
          id="rule-trial-rows"
          value={rowsText}
          onChange={(e) => setRowsText(e.target.value)}
          rows={6}
          className="font-mono"
        />
      </div>
      <div>
        <Button
          type="button"
          variant="secondary"
          onClick={handleTry}
          disabled={!code || !column || isPending}
        >
          <ButtonText leftIcon={isPending ? <Spinner /> : null} text="Try" />
        </Button>
      </div>
      {inputError && (
        <Alert variant="destructive">
          <AlertDescription>{inputError}</AlertDescription>
        </Alert>
      )}
      {failure && (
        <Alert variant="destructive">
          <AlertTitle>
            Failed on row {failure.row + 1}
            {failure.column ? `, column "${failure.column}"` : ''}
          </AlertTitle>
          <AlertDescription className="font-mono whitespace-pre-wrap">
            {failure.message}
          </AlertDescription>
        </Alert>
      )}
      {rows && (
        <Table>
          <TableHeader>
            <TableRow>
              <TableHead>Row</TableHead>
              {kind === 'transform' && <TableHead>Before</TableHead>}
              <TableHead>After</TableHead>
            </TableRow>
          </TableHeader>
          <TableBody>
            {rows.map((row, i) => (
              <TableRow key={i}>
                <TableCell>{i + 1}</TableCell>
                {kind === 'transform' && (
                  <TableCell className="font-mono">
                    {JSON.stringify(row.before)}
                  </TableCell>
                )}
                <TableCell className="font-mono">
                  {JSON.stringify(row.after)}
                </TableCell>
              </TableRow>
            ))}
          </TableBody>
        </Table>
      )}
    </div>
  );
}
