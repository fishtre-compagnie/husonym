import { Pill, StatusDot } from '@/components/ds';
import { Card, CardContent } from '@/components/ui/card';
import { AccountHook } from '@husonym/sdk';
import { ReactElement } from 'react';
import DisableHookButton from './DisableHookButton';
import EditHookButton from './EditHookButton';
import RemoveHookButton from './RemoveHookButton';

interface Props {
  hook: AccountHook;
  onDeleted(): void;
  onEdited(): void;
}

export default function HookCard(props: Props): ReactElement {
  const { hook, onDeleted, onEdited } = props;
  // A Slack hook that is still stored is shown, turned off and deleted, never edited.
  const isSlack = hook.config?.config.case === 'slack';

  return (
    <div id={`accounthook-${hook.id}`}>
      <Card>
        <CardContent className="p-4">
          <div className="flex justify-between">
            <div className="flex grow flex-col gap-2">
              {/* Header Section */}
              <div className="flex items-center justify-between">
                <h3 className="font-display text-base font-semibold">
                  {hook.name}
                </h3>
              </div>

              <p className="text-sm text-muted-foreground">
                {hook.description}
              </p>

              <div className="flex items-center gap-4 pt-2 flex-wrap">
                <Pill tone={hook.enabled ? 'ok' : 'quiet'}>
                  <StatusDot tone={hook.enabled ? 'ok' : 'quiet'} />
                  {hook.enabled ? 'Enabled' : 'Disabled'}
                </Pill>
                {hook.config?.config.case && (
                  <Pill className="font-mono">
                    {isSlack ? 'Slack' : hook.config?.config.case}
                  </Pill>
                )}
              </div>
              {isSlack && (
                <p className="text-sm text-muted-foreground">
                  Slack hooks are no longer supported and are not run. Replace
                  this hook with a webhook.
                </p>
              )}
            </div>

            <div className="flex items-center gap-2">
              {isSlack ? (
                hook.enabled && (
                  <DisableHookButton hook={hook} onDisabled={onEdited} />
                )
              ) : (
                <EditHookButton hook={hook} onEdited={onEdited} />
              )}
              <RemoveHookButton hook={hook} onDeleted={onDeleted} />
            </div>
          </div>
        </CardContent>
      </Card>
    </div>
  );
}
