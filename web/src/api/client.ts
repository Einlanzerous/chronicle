// Typed API client (CHRN-53). Types come from the server's own contract
// rather than being hand-written a second time -- regenerate after a route
// change with:
//
//   bun run api:gen        # from web/, or scripts/gen-webapi.sh from the repo root
//
// `schema.d.ts` is GENERATED and committed for PR diff visibility; verify.sh's
// "web api types" step and CI's staleness guard fail if it drifts from
// openapi.yaml.
import createClient from 'openapi-fetch'
import type { paths } from './schema.d.ts'

// Chronicle's contract carries no /v1 prefix and this app is always served
// same-origin by the Go binary that answers it (in dev, Vite's proxy makes
// that true too) -- so the client talks to relative paths and needs no base
// URL of its own.
//
// `credentials: 'same-origin'` is the whole of how the browser authenticates:
// the session lives in the `chronicle_session` cookie (HttpOnly, so this code
// never reads or sets it directly), and openapi-fetch does not send cookies
// by default.
export const api = createClient<paths>({
  baseUrl: '/',
  credentials: 'same-origin',
})
