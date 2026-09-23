import { useEffect, useState } from 'react';

// The value once it has stopped changing for `delayMs`: a preview that reads a database follows
// what is being edited without a query per keystroke.
export function useDebouncedValue<T>(value: T, delayMs: number): T {
  const [debounced, setDebounced] = useState(value);
  useEffect(() => {
    const timer = setTimeout(() => setDebounced(value), delayMs);
    return () => clearTimeout(timer);
  }, [value, delayMs]);
  return debounced;
}
