import { withHusonymContext } from '@/api-only/husonym-context';
import { create } from '@bufbuild/protobuf';
import {
  SetPersonalAccountRequestSchema,
  SetUserRequestSchema,
} from '@husonym/sdk';
import { NextRequest, NextResponse } from 'next/server';

export async function GET(req: NextRequest): Promise<NextResponse> {
  return withHusonymContext(async (ctx) => {
    const setUserResp = await ctx.client.users.setUser(
      create(SetUserRequestSchema, {})
    );

    await ctx.client.users.setPersonalAccount(
      create(SetPersonalAccountRequestSchema, {})
    );
    return setUserResp;
  })(req);
}
