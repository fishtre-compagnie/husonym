import { withHusonymContext } from '@/api-only/husonym-context';
import { NextRequest, NextResponse } from 'next/server';
import { getSystemAppConfig } from './config';

export async function GET(req: NextRequest): Promise<NextResponse> {
  return withHusonymContext(async () => getSystemAppConfig())(req);
}
