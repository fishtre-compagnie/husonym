# Husonym TypeScript SDK

This SDK contains the generated types for Husonym API.
This SDK is dogfooded by the main Husonym webapp to ensure its durability.

## Installation

```sh
npm install @husonym/sdk @bufbuild/protobuf
```

## Detailed Docs

This README shows the basics of how to use the SDK.

For more detailed docs, go [here](https://docs.husonym.allopneus.com/api/typescript).

## Usage

For a prime example of how to us this SDK, view the [withHusonymContext](https://github.com/fishtre-compagnie/husonym/blob/main/frontend/apps/web/api-only/husonym-context.ts#L23) method in the Husonym app's BFF layer.

### Note on Transports

Based on your usage, you'll have to install a different version of `connect` to provide the correct Transport based on your environment.

- Node: [@connectrpc/connect-node](https://connectrpc.com/docs/node/using-clients)
- Web: [@connectrpc/connect-web](https://connectrpc.com/docs/web/using-clients)

Install whichever one makes sense for you

```sh
npm install @connectrpc/connect-node
npm install @connectrpc/connect-web
```

Husonym API serves up `Connect`, which can listen using Connect, gRPC, or Web protocols.
Each of the libraries above provides all three of those protocols, but it's recommended to use `createConnectTransport` for the most efficient setup.

```ts
import { getHusonymClient } from '@husonym/sdk';
import { createConnectTransport } from '@connectrpc/connect-node';

const husonymClient = getHusonymClient({
  getTransport(interceptors) {
    return createConnectTransport({
      baseUrl: '<url>',
      httpVersion: '2',
      interceptors: interceptors,
    });
  },
});
```

## Authenticating

To authenticate the TS Husonym Client, a function may be provided to the configuration that will be invoked prior to every request.
This gives flexability in how the access token may be retrieved and supports either a Husonym API Key or a standard user JWT token.

When the `getAccessToken` function is provided, the Husonym Client is configured with an auth interceptor that attaches the `Authorization` header to every outgoingn request with the access token returned from the function.
This is why the `getTransport` method receives a list of interceptors, and why it's important to hook them up to pass them through to the relevant transport being used.

```ts
import { getHusonymClient } from '@husonym/sdk';
import { createConnectTransport } from '@connectrpc/connect-node';

const husonymClient = getHusonymClient({
  getAccessToken: () => process.env.HUSONYM_API_KEY,
  getTransport(interceptors) {
    return createConnectTransport({
      baseUrl: process.env.HUSONYM_API_URL,
      httpVersion: '2',
      interceptors: interceptors,
    });
  },
});
```

### Husonym App

In the Husonym dashboard app, we pull the user access token off of the incoming request (auth is configured using `next-auth`.).
This way we can ensure that all requests are using the user's access token and are passed through to Husonym API.
