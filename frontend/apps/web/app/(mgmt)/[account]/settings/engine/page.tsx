'use client';
import SubPageHeader from '@/components/headers/SubPageHeader';
import { Badge } from '@/components/ui/badge';
import {
  Card,
  CardContent,
  CardDescription,
  CardHeader,
  CardTitle,
} from '@/components/ui/card';
import { Separator } from '@/components/ui/separator';
import { Skeleton } from '@/components/ui/skeleton';
import { useGetSystemAppConfig } from '@/libs/hooks/useGetSystemAppConfig';
import { ReactElement } from 'react';

// Engine settings — read-only view of the transformation engine powering this
// deployment. The value comes from the app's system config (ENABLE_ATHANOR_ENGINE)
// and must be kept in sync with the worker, which is the real decider.
export default function Engine(): ReactElement {
  const { data: systemAppConfigData, isLoading } = useGetSystemAppConfig();

  if (isLoading) {
    return (
      <div className="flex flex-col gap-4">
        <Skeleton className="h-8 w-64" />
        <Skeleton className="h-48 w-full" />
      </div>
    );
  }

  const isAthanor = systemAppConfigData?.isAthanorEngineEnabled ?? false;
  const activeEngine = isAthanor ? 'Athanor' : 'Benthos (legacy)';

  return (
    <div className="flex flex-col gap-6">
      <SubPageHeader
        header="Engine"
        description="Transformation engine currently powering this deployment."
      />

      <Card>
        <CardHeader>
          <div className="flex flex-row items-center justify-between gap-4">
            <CardTitle>Transformation engine</CardTitle>
            <Badge variant={isAthanor ? 'success' : 'secondary'}>
              {activeEngine} · Active
            </Badge>
          </div>
          <CardDescription>
            The engine that reads source data, applies transformers and writes to
            destinations during a sync.
          </CardDescription>
        </CardHeader>
        <CardContent className="flex flex-col gap-4">
          <EngineRow
            name="Athanor"
            active={isAthanor}
            description="New vectorized engine. Deterministic cross-database consistency, native referential integrity, key-conflict handling (upsert / do nothing) and subsetting."
          />
          <EngineRow
            name="Benthos (legacy)"
            active={!isAthanor}
            description="Original row-by-row streaming engine (redpanda-connect). Used as a fallback when Athanor is disabled."
          />

          <Separator />

          <div className="flex flex-col gap-2">
            <span className="text-sm font-medium">
              Supported databases (Athanor)
            </span>
            <div className="flex flex-row flex-wrap gap-2">
              <Badge variant="outline">PostgreSQL</Badge>
              <Badge variant="outline">MySQL</Badge>
              <Badge variant="darkOutline">SQL Server — planned</Badge>
              <Badge variant="darkOutline">Oracle — best effort</Badge>
            </div>
          </div>

          <p className="text-xs text-muted-foreground">
            Read-only. This reflects the engine configured for this deployment
            (<code>ENABLE_ATHANOR_ENGINE</code>), not a live report from the
            worker — the value must match the worker configuration.
          </p>
        </CardContent>
      </Card>
    </div>
  );
}

interface EngineRowProps {
  name: string;
  active: boolean;
  description: string;
}

function EngineRow({
  name,
  active,
  description,
}: EngineRowProps): ReactElement {
  return (
    <div className="flex flex-row items-start justify-between gap-4 rounded-lg border p-4">
      <div className="flex flex-col gap-1">
        <span className="text-sm font-semibold">{name}</span>
        <span className="text-sm text-muted-foreground">{description}</span>
      </div>
      <Badge variant={active ? 'success' : 'outline'}>
        {active ? 'Active' : 'Inactive'}
      </Badge>
    </div>
  );
}
