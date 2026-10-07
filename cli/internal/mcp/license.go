package mcp_server

import (
	"context"
	"errors"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ErrNoLicenseInForce is what Options.Allowed answers for an instance that has no license in
// force, none at all or one past its grace period. It is an answer of the API, not a failure to
// read it: such an instance includes no feature, which is not the same as a license that lacks
// this one.
var ErrNoLicenseInForce = errors.New("no license is in force on this instance")

// requiresFeature refuses the tool calls of an instance whose license does not include the
// mcp feature. Every other method goes through, the initialization and the listing of the
// tools among them: a client must be able to connect and see the tools to read the refusal.
//
// The server has no license of its own: allowed asks the API. A refusal reaches the model as the
// error of the tool, as that of any tool that fails. An API that cannot say is not a license
// that refuses: the call is answered with what went wrong in reading it.
func requiresFeature(allowed func(ctx context.Context) (bool, error)) mcp.Middleware {
	return func(next mcp.MethodHandler) mcp.MethodHandler {
		return func(ctx context.Context, method string, req mcp.Request) (mcp.Result, error) {
			if _, ok := req.(*mcp.CallToolRequest); !ok {
				return next(ctx, method, req)
			}
			ok, err := allowed(ctx)
			if errors.Is(err, ErrNoLicenseInForce) {
				return toolError(ErrNoLicenseInForce), nil
			}
			if err != nil {
				return toolError(fmt.Errorf("unable to read the license: %w", err)), nil
			}
			if !ok {
				// The name is that of the feature in internal/license, which the entry point holds to: this
				// tree imports nothing of the sort.
				return toolError(errors.New("this license does not include mcp")), nil
			}
			return next(ctx, method, req)
		}
	}
}

// toolError is the answer of a tool that failed, as the SDK gives it when a tool returns an error.
func toolError(err error) *mcp.CallToolResult {
	result := &mcp.CallToolResult{}
	result.SetError(err)
	return result
}
