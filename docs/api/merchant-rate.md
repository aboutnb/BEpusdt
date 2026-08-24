# Sub2API Merchant API

BEpusdt exposes these signed server-to-server endpoints for the Sub2API USDT
provider:

- `POST /api/v1/merchant/capabilities`
- `POST /api/v1/merchant/rate`
- `GET /api/v1/merchant/readiness`
- `POST /api/v1/merchant/order/create`
- `POST /api/v1/merchant/order/query`

Set the following values in the gateway environment. They are separate from
the legacy `api_auth_token` used by the native cashier API.

```dotenv
BEPUSDT_HMAC_KEY_ID=sub2api
BEPUSDT_HMAC_SECRET=<long-random-shared-secret>
BEPUSDT_API_AUTH_TOKEN=<long-random-legacy-token>
BEPUSDT_PUBLIC_BASE_URL=https://upay.nodx.net
BEPUSDT_NOTIFY_HOSTS=aivoza.com
BEPUSDT_REDIRECT_HOSTS=aivoza.com
BEPUSDT_API_APP_URI=https://upay.nodx.net
BEPUSDT_RPC_ENDPOINT_BSC=https://bsc-rpc.publicnode.com
BEPUSDT_RPC_ENDPOINT_TRON=grpc.trongrid.io:50051
```

The API token and RPC values are applied to the new gateway's own database at
startup. They do not read or migrate settings from another BEpusdt installation.

The Sub2API integration reads the current BEpusdt `USDT/CNY` quote immediately
before each order and passes that rate back to the create endpoint, so the
gateway freezes the exact quote.

```http
POST /api/v1/merchant/rate
Content-Type: application/json
X-BEPUSDT-Key-Id: sub2api
X-BEPUSDT-Timestamp: <unix seconds>
X-BEPUSDT-Nonce: <random value>
X-BEPUSDT-Content-SHA256: <sha256 body hex>
X-BEPUSDT-Signature: <hmac-sha256 hex>
```

Request body:

```json
{"crypto":"USDT","fiat":"CNY"}
```

The canonical value is five lines joined with a newline:

```text
METHOD
PATH
UNIX_TIMESTAMP
NONCE
SHA256_BODY_HEX
```

Sign it with `HMAC-SHA256(BEPUSDT_HMAC_SECRET, canonical)`. The request key ID
must equal `BEPUSDT_HMAC_KEY_ID`. Timestamps older than five minutes and replayed
nonces are rejected.

The rate response is
`{"code":"ok","data":{"crypto":"USDT","fiat":"CNY","rate":"...","updated_at":...}}`.

`capabilities` reports enabled wallets and configured RPC endpoints for TRC20
and BEP20. `order/create` accepts `order_id`, `amount`, `fiat`, `trade_type`,
`notify_url`, `redirect_url`, `timeout_seconds`, and `rate`; repeated calls with
the same immutable values return the original order. `order/query` requires both
`order_id` and `trade_id` and returns the complete frozen payment quote.
