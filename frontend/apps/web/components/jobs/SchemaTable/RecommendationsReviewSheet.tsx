'use client';

import { useAccount } from '@/components/providers/account-provider';
import { Badge } from '@/components/ui/badge';
import { Button } from '@/components/ui/button';
import {
  Select,
  SelectContent,
  SelectItem,
  SelectTrigger,
  SelectValue,
} from '@/components/ui/select';
import {
  Sheet,
  SheetContent,
  SheetDescription,
  SheetHeader,
  SheetTitle,
} from '@/components/ui/sheet';
import {
  Table,
  TableBody,
  TableCell,
  TableHead,
  TableHeader,
  TableRow,
} from '@/components/ui/table';
import { Transformer } from '@/shared/transformers';
import {
  convertTransformerConfigToForm,
  JobMappingFormValues,
  JobMappingTransformerForm,
} from '@/yup-validations/jobs';
import {
  DetectionEvidence,
  DetectionEvidence_DetectorKind,
  NewTransformerProposal,
  TransformerRecommendation,
  TransformerSource,
} from '@husonym/sdk';
import { useTransformerProposalPrefillStore } from '@/app/(mgmt)/[account]/new/transformer/proposal-prefill-store';
import { useRouter } from 'next/navigation';
import { Fragment, ReactElement, useMemo, useState } from 'react';

interface Props {
  open: boolean;
  onOpenChange(open: boolean): void;
  recommendations: TransformerRecommendation[];
  mappings: JobMappingFormValues[];
  getTransformerFromFieldValue(value: JobMappingTransformerForm): Transformer;
  onApply(
    updates: { index: number; transformer: JobMappingTransformerForm }[]
  ): void;
}

interface ReviewRow {
  index: number;
  schema: string;
  table: string;
  column: string;
  category: string;
  confidence: number;
  transformerName: string;
  transformerForm: JobMappingTransformerForm;
  evidence: DetectionEvidence[];
  hasProposal: boolean;
  proposal?: NewTransformerProposal;
}

const THRESHOLD_OPTIONS = ['70', '80', '90'];

export default function RecommendationsReviewSheet(
  props: Props
): ReactElement {
  const {
    open,
    onOpenChange,
    recommendations,
    mappings,
    getTransformerFromFieldValue,
    onApply,
  } = props;

  const [selectedIndices, setSelectedIndices] = useState<Set<number>>(
    new Set()
  );
  const [threshold, setThreshold] = useState<string>('80');
  const { account } = useAccount();
  const router = useRouter();
  const setProposalPrefill = useTransformerProposalPrefillStore(
    (state) => state.setPrefill
  );

  const rows = useMemo((): ReviewRow[] => {
    const indexByKey = new Map<string, number>();
    mappings.forEach((m, idx) => {
      indexByKey.set(`${m.schema}.${m.table}.${m.column}`, idx);
    });

    return recommendations
      .map((r): ReviewRow | null => {
        const index = indexByKey.get(`${r.schema}.${r.table}.${r.column}`);
        if (index === undefined) {
          // Column isn't present in the current mappings (e.g. table not selected). Skip it.
          return null;
        }
        const transformerForm: JobMappingTransformerForm = {
          config: convertTransformerConfigToForm(r.recommendedConfig),
        };
        const transformer = getTransformerFromFieldValue(transformerForm);
        const hasProposal = !!r.proposal;
        return {
          index,
          schema: r.schema,
          table: r.table,
          column: r.column,
          category: r.category,
          confidence: r.confidence,
          transformerName: hasProposal
            ? ''
            : transformer.name || 'No matching transformer',
          transformerForm,
          evidence: r.evidence,
          hasProposal,
          proposal: r.proposal,
        };
      })
      .filter((r): r is ReviewRow => r !== null);
  }, [recommendations, mappings, getTransformerFromFieldValue]);

  function toggleRow(index: number, checked: boolean): void {
    setSelectedIndices((prev) => {
      const next = new Set(prev);
      if (checked) {
        next.add(index);
      } else {
        next.delete(index);
      }
      return next;
    });
  }

  function selectAllAboveThreshold(): void {
    const minConfidence = Number(threshold) / 100;
    setSelectedIndices((prev) => {
      const next = new Set(prev);
      rows.forEach((row) => {
        if (row.confidence >= minConfidence && !row.hasProposal) {
          next.add(row.index);
        }
      });
      return next;
    });
  }

  function handleCreateTransformer(proposal: NewTransformerProposal): void {
    if (!account) {
      return;
    }
    setProposalPrefill({
      name: proposal.name,
      description: proposal.description,
      javascriptCode: proposal.javascriptCode,
    });
    router.push(
      `/${account.name}/new/transformer?transformer=${TransformerSource.TRANSFORM_JAVASCRIPT}&aiProposal=1`
    );
  }

  function handleApply(): void {
    const updates = rows
      .filter((row) => selectedIndices.has(row.index) && !row.hasProposal)
      .map((row) => ({ index: row.index, transformer: row.transformerForm }));
    onApply(updates);
    onOpenChange(false);
  }

  return (
    <Sheet open={open} onOpenChange={onOpenChange}>
      <SheetContent
        side="right"
        className="w-full sm:max-w-4xl overflow-y-auto"
      >
        <SheetHeader>
          <div className="flex flex-row items-center gap-2">
            <SheetTitle>AI-Assisted Suggestions</SheetTitle>
            <Badge variant="secondary">AI generated</Badge>
          </div>
          <SheetDescription>
            Review each suggestion — this is assistance, not a compliance
            guarantee. Nothing is applied automatically; select the rows you
            want and click Apply.
          </SheetDescription>
        </SheetHeader>

        <div className="flex flex-col gap-4 py-4">
          <div className="flex flex-row items-center gap-2">
            <span className="text-sm text-muted-foreground">
              Select all suggestions above
            </span>
            <Select value={threshold} onValueChange={setThreshold}>
              <SelectTrigger className="w-[90px]">
                <SelectValue />
              </SelectTrigger>
              <SelectContent>
                {THRESHOLD_OPTIONS.map((opt) => (
                  <SelectItem key={opt} value={opt}>
                    {opt}%
                  </SelectItem>
                ))}
              </SelectContent>
            </Select>
            <span className="text-sm text-muted-foreground">confidence</span>
            <Button
              type="button"
              variant="outline"
              onClick={selectAllAboveThreshold}
            >
              Select
            </Button>
          </div>

          <div className="rounded-md border">
            <Table>
              <TableHeader>
                <TableRow>
                  <TableHead className="w-[40px]" />
                  <TableHead>Column</TableHead>
                  <TableHead>Category</TableHead>
                  <TableHead>Suggested Transformer</TableHead>
                  <TableHead>Confidence</TableHead>
                  <TableHead>Justification</TableHead>
                </TableRow>
              </TableHeader>
              <TableBody>
                {rows.length === 0 && (
                  <TableRow>
                    <TableCell colSpan={6} className="text-center py-6">
                      No recommendations apply to the currently selected
                      tables.
                    </TableCell>
                  </TableRow>
                )}
                {rows.map((row) => (
                  <Fragment key={`${row.schema}.${row.table}.${row.column}`}>
                    <TableRow>
                      <TableCell>
                        <input
                          type="checkbox"
                          className="w-4 h-4 cursor-pointer"
                          checked={selectedIndices.has(row.index)}
                          disabled={row.hasProposal}
                          onChange={(e) =>
                            toggleRow(row.index, e.target.checked)
                          }
                        />
                      </TableCell>
                      <TableCell className="font-medium">
                        {row.schema}.{row.table}.{row.column}
                      </TableCell>
                      <TableCell>
                        <Badge variant="outline">{row.category}</Badge>
                      </TableCell>
                      <TableCell>
                        {row.hasProposal ? (
                          <Badge variant="secondary">
                            New transformer proposal: {row.proposal?.name}
                          </Badge>
                        ) : (
                          row.transformerName
                        )}
                      </TableCell>
                      <TableCell>
                        {Math.round(row.confidence * 100)}%
                      </TableCell>
                      <TableCell>
                        <EvidenceList evidence={row.evidence} />
                      </TableCell>
                    </TableRow>
                    {row.hasProposal && row.proposal && (
                      <TableRow>
                        <TableCell colSpan={6} className="bg-muted/30">
                          <ProposalDetails
                            proposal={row.proposal}
                            onCreate={() =>
                              handleCreateTransformer(row.proposal!)
                            }
                          />
                        </TableCell>
                      </TableRow>
                    )}
                  </Fragment>
                ))}
              </TableBody>
            </Table>
          </div>
          {rows.some((row) => row.hasProposal) && (
            <p className="text-xs text-muted-foreground">
              Rows proposing a new custom transformer aren&apos;t applied from
              here yet — creating and assigning generated transformers is
              handled in a later step.
            </p>
          )}
        </div>

        <div className="flex flex-row justify-end gap-2">
          <Button
            type="button"
            variant="outline"
            onClick={() => onOpenChange(false)}
          >
            Cancel
          </Button>
          <Button
            type="button"
            disabled={selectedIndices.size === 0}
            onClick={handleApply}
          >
            Apply {selectedIndices.size > 0 ? `(${selectedIndices.size})` : ''}
          </Button>
        </div>
      </SheetContent>
    </Sheet>
  );
}

function ProposalDetails(props: {
  proposal: NewTransformerProposal;
  onCreate(): void;
}): ReactElement {
  const { proposal, onCreate } = props;
  return (
    <div className="flex flex-col gap-2 py-2">
      <div className="flex flex-row items-center gap-2">
        <Badge variant="secondary">AI generated</Badge>
        <span className="text-sm font-medium">{proposal.name}</span>
      </div>
      {proposal.description && (
        <p className="text-sm text-muted-foreground">
          {proposal.description}
        </p>
      )}
      {proposal.rationale && (
        <p className="text-xs text-muted-foreground italic">
          {proposal.rationale}
        </p>
      )}
      <pre className="max-h-40 overflow-auto rounded-md border bg-background p-2 font-mono text-xs whitespace-pre">
        {proposal.javascriptCode}
      </pre>
      <div>
        <Button type="button" variant="outline" onClick={onCreate}>
          Create this transformer
        </Button>
      </div>
    </div>
  );
}

function EvidenceList(props: { evidence: DetectionEvidence[] }): ReactElement {
  const { evidence } = props;
  return (
    <div className="flex flex-col gap-1">
      {evidence.map((e, idx) => (
        <div key={idx} className="flex flex-row items-center gap-1 text-xs">
          <Badge
            variant="outline"
            className="text-[10px] px-1 py-0 bg-blue-100 text-gray-800 dark:bg-blue-200 dark:text-gray-900"
          >
            {getDetectorKindLabel(e.kind)}
          </Badge>
          <span className="text-muted-foreground">{e.detail}</span>
        </div>
      ))}
    </div>
  );
}

function getDetectorKindLabel(kind: DetectionEvidence_DetectorKind): string {
  switch (kind) {
    case DetectionEvidence_DetectorKind.REGEX:
      return 'REGEX';
    case DetectionEvidence_DetectorKind.DICTIONARY:
      return 'DICTIONARY';
    case DetectionEvidence_DetectorKind.LLM:
      return 'LLM';
    default:
      return 'UNKNOWN';
  }
}
