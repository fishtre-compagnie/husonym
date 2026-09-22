import TruncatedText from '@/components/TruncatedText';
import { Button } from '@/components/ui/button';
import {
  Command,
  CommandEmpty,
  CommandGroup,
  CommandInput,
  CommandItem,
  CommandList,
} from '@/components/ui/command';
import {
  Popover,
  PopoverContent,
  PopoverTrigger,
} from '@/components/ui/popover';
import { cn } from '@/libs/utils';
import {
  JobMappingTransformerForm,
  convertJobMappingTransformerToForm,
} from '@/yup-validations/jobs';
import { create } from '@bufbuild/protobuf';
import {
  JobMappingTransformerSchema,
  SystemTransformer,
  TransformerConfigSchema,
  UserDefinedTransformer,
  UserDefinedTransformerConfigSchema,
} from '@husonym/sdk';
import { CaretSortIcon, CheckIcon } from '@radix-ui/react-icons';
import { ReactElement, useEffect, useState } from 'react';
import { TransformerResult } from './transformer-handler';

type Side = 'top' | 'right' | 'bottom' | 'left';

interface Props {
  getTransformers(): {
    system: SystemTransformer[];
    userDefined: UserDefinedTransformer[];
  };
  value: JobMappingTransformerForm;
  buttonText: string;
  buttonClassName?: string;
  // Largeur du libellé. Par défaut il tient dans une cellule de tableau ; dans le
  // panneau de décision, où le bouton fait toute la largeur, le nom du transformer
  // doit se lire en entier.
  buttonTextClassName?: string;
  onSelect(value: JobMappingTransformerForm): void;
  side?: Side;
  disabled: boolean;
  notFoundText?: string;
}

export default function TransformerSelect(props: Props): ReactElement {
  const {
    getTransformers,
    value,
    onSelect,
    buttonText,
    side,
    disabled,
    buttonClassName,
    buttonTextClassName = 'lg:w-[200px]',
    notFoundText = 'No transformers found.',
  } = props;
  const [open, setOpen] = useState(false);

  const [{ system, userDefined }, setTransformerResult] =
    useState<TransformerResult>({ system: [], userDefined: [] });

  useEffect(() => {
    if (open) {
      setTransformerResult(getTransformers());
    }
  }, [open]);

  return (
    <Popover open={open} onOpenChange={setOpen}>
      <PopoverTrigger asChild>
        <Button
          type="button"
          variant="outline"
          role="combobox"
          aria-expanded={open}
          disabled={disabled}
          className={cn('justify-between', buttonClassName)}
        >
          <div
            className={cn(
              'whitespace-nowrap truncate text-left',
              buttonTextClassName
            )}
          >
            {buttonText}
          </div>
          <CaretSortIcon className="ml-2 h-4 w-4 shrink-0 opacity-50" />
        </Button>
      </PopoverTrigger>
      <PopoverContent
        className="w-[350px] p-0"
        avoidCollisions={true} // this prevents the popover from overflowing out of the viewport (meaning there is content the user isn't able to access)
        side={side}
      >
        <Command>
          <CommandInput placeholder={'Search...'} />
          <div>
            <CommandList className="max-h-[600px]">
              <CommandEmpty>{notFoundText}</CommandEmpty>
              {userDefined.length > 0 && (
                <CommandGroup heading="Custom">
                  {userDefined.map((t) => {
                    return (
                      <CommandItem
                        key={t.id}
                        onSelect={() => {
                          onSelect(
                            convertJobMappingTransformerToForm(
                              create(JobMappingTransformerSchema, {
                                config: create(TransformerConfigSchema, {
                                  config: {
                                    case: 'userDefinedTransformerConfig',
                                    value: create(
                                      UserDefinedTransformerConfigSchema,
                                      {
                                        id: t.id,
                                      }
                                    ),
                                  },
                                }),
                              })
                            )
                          );
                          setOpen(false);
                        }}
                        value={t.name}
                      >
                        <div className="flex flex-row items-center justify-between w-full">
                          <div className="flex flex-row items-center">
                            <CheckIcon
                              className={cn(
                                'mr-2 h-4 w-4',
                                value?.config?.case ===
                                  'userDefinedTransformerConfig' &&
                                  value.config.value.id === t.id
                                  ? 'opacity-100'
                                  : 'opacity-0'
                              )}
                            />
                            <div className="items-center">
                              <TruncatedText text={t?.name} maxWidth={200} />
                            </div>
                          </div>
                        </div>
                      </CommandItem>
                    );
                  })}
                </CommandGroup>
              )}
              {system.length > 0 && (
                <CommandGroup heading="System">
                  {system.map((t) => {
                    return (
                      <CommandItem
                        key={t.source}
                        onSelect={() => {
                          onSelect(
                            convertJobMappingTransformerToForm(
                              create(JobMappingTransformerSchema, {
                                config: t.config,
                              })
                            )
                          );
                          setOpen(false);
                        }}
                        value={t.name}
                      >
                        <div className="flex flex-row items-center justify-between w-full">
                          <div className=" flex flex-row items-center">
                            <CheckIcon
                              className={cn(
                                'mr-2 h-4 w-4',
                                value?.config.case === t?.config?.config.case
                                  ? 'opacity-100'
                                  : 'opacity-0'
                              )}
                            />
                            <div className="items-center">{t?.name}</div>
                          </div>
                        </div>
                      </CommandItem>
                    );
                  })}
                </CommandGroup>
              )}
            </CommandList>
          </div>
        </Command>
      </PopoverContent>
    </Popover>
  );
}
