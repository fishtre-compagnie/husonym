'use client';

import { useTheme } from 'next-themes';
import { useSyncExternalStore } from 'react';

import { cn } from '@/libs/utils';
import { MoonIcon, SunIcon } from '../shell/icons';
import { topbarIconButtonClass } from '../shell/styles';

const subscribeNoop = () => () => {};

// One button that flips between light and dark. Until someone presses it the theme
// follows the system's; once pressed, the choice is theirs.
export function ModeToggle() {
  const { resolvedTheme, setTheme } = useTheme();
  // The theme is only known in the browser: the server renders a neutral label.
  const mounted = useSyncExternalStore(
    subscribeNoop,
    () => true,
    () => false
  );
  const isDark = mounted && resolvedTheme === 'dark';
  const label = !mounted
    ? 'Switch theme'
    : isDark
      ? 'Switch to light theme'
      : 'Switch to dark theme';

  return (
    <button
      type="button"
      aria-label={label}
      title={label}
      onClick={() => setTheme(isDark ? 'light' : 'dark')}
      className={cn(topbarIconButtonClass)}
    >
      {/* The icon follows the html class, so it is right before hydration too. */}
      <SunIcon size={18} className="hidden dark:block" />
      <MoonIcon size={18} className="block dark:hidden" />
    </button>
  );
}
