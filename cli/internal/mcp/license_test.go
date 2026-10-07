package mcp_server

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/stretchr/testify/require"
)

func licenseAPI(allowed func(context.Context) (bool, error)) fakeAPI {
	return fakeAPI{
		connections: &fakeConnectionService{},
		data:        &fakeDataService{},
		jobs:        &fakeJobService{},
		allowed:     allowed,
	}
}

// A tool call of an instance whose license does not include mcp is refused with a message that
// names the feature, which the model reads as the error of the tool.
func Test_ToolCalls_NeedTheMcpFeature(t *testing.T) {
	t.Parallel()
	session := connectAPI(t, licenseAPI(func(context.Context) (bool, error) { return false, nil }), nil, "")

	require.Equal(t, "this license does not include mcp", callToolError(t, session, "list_connections", nil))
}

// An instance without a license in force includes no feature at all: the refusal says that,
// rather than that mcp is missing from a license.
func Test_ToolCalls_WithoutALicenseInForceSaySo(t *testing.T) {
	t.Parallel()
	session := connectAPI(t, licenseAPI(func(context.Context) (bool, error) {
		return false, ErrNoLicenseInForce
	}), nil, "")

	message := callToolError(t, session, "list_connections", nil)
	require.Equal(t, "no license is in force on this instance", message)
	require.NotContains(t, message, "does not include")
	require.NotContains(t, message, "unable to read the license")
}

// A client must be able to connect and discover the tools to see why they are refused: only the
// tool calls are.
func Test_ToolCalls_OtherMethodsAreNeverRefused(t *testing.T) {
	t.Parallel()
	var asked atomic.Bool
	session := connectAPI(t, licenseAPI(func(context.Context) (bool, error) {
		asked.Store(true)
		return false, nil
	}), nil, "")

	tools, err := session.ListTools(t.Context(), nil)
	require.NoError(t, err)
	require.NotEmpty(t, tools.Tools)
	require.NoError(t, session.Ping(t.Context(), nil))
	require.False(t, asked.Load(), "the license was asked for something that is not a tool call")
}

// An API that cannot say is not a license that refuses: the call is answered with what went
// wrong, not with the refusal.
func Test_ToolCalls_AnErrorReadingTheLicenseIsReturnedAsSuch(t *testing.T) {
	t.Parallel()
	session := connectAPI(t, licenseAPI(func(context.Context) (bool, error) {
		return false, errors.New("the API did not answer within 30s")
	}), nil, "")

	message := callToolError(t, session, "list_connections", nil)
	require.Equal(t, "unable to read the license: the API did not answer within 30s", message)
	require.NotContains(t, message, "does not include")
}

// A license that includes mcp lets the call through.
func Test_ToolCalls_AreServedWhenTheLicenseIncludesMcp(t *testing.T) {
	t.Parallel()
	session := connectAPI(t, licenseAPI(func(context.Context) (bool, error) { return true, nil }), nil, "")

	res, err := session.CallTool(t.Context(), &mcp.CallToolParams{Name: "list_connections"})
	require.NoError(t, err)
	require.False(t, res.IsError, "%v", res.Content)
}
