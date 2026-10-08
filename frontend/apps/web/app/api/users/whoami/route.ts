import { withHusonymContext } from '@/api-only/husonym-context';
import { create } from '@bufbuild/protobuf';
import { EnterInstanceRequestSchema, SetUserRequestSchema } from '@husonym/sdk';
import { NextRequest, NextResponse } from 'next/server';

export async function GET(req: NextRequest): Promise<NextResponse> {
  return withHusonymContext(async (ctx) => {
    const setUserResp = await ctx.client.users.setUser(
      create(SetUserRequestSchema, {})
    );

    // The API decides where the user lands: the organization of the instance, or their
    // personal account where no organization applies.
    await ctx.client.users.enterInstance(
      create(EnterInstanceRequestSchema, {})
    );
    return setUserResp;
  })(req);
}
