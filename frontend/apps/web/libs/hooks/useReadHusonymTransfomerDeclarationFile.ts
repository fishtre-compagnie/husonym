import { useQuery, UseQueryResult } from '@tanstack/react-query';
import { fetcher } from '../fetcher';

export function useReadHusonymTransformerDeclarationFile(): UseQueryResult<string> {
  return useQuery({
    queryKey: [`/api/files/husonym-transformer-declarations`],
    queryFn: (ctx) => fetcher(ctx.queryKey.join('/')),
  });
}
