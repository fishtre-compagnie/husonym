import { create } from 'zustand';

// Prefill data for the "Create this transformer" action in
// RecommendationsReviewSheet.tsx (plans/assistant-ia-config-anonymisation.md
// §4.4). Kept as a plain in-memory zustand store (no persistence) since the
// navigation from the recommendations sheet to the new-transformer page is a
// client-side route change within the same app session.
export interface TransformerProposalPrefill {
  name: string;
  description: string;
  javascriptCode: string;
}

interface TransformerProposalPrefillStore {
  prefill: TransformerProposalPrefill | null;
  setPrefill(prefill: TransformerProposalPrefill): void;
  clearPrefill(): void;
}

export const useTransformerProposalPrefillStore =
  create<TransformerProposalPrefillStore>((set) => ({
    prefill: null,
    setPrefill: (prefill) => set({ prefill }),
    clearPrefill: () => set({ prefill: null }),
  }));
