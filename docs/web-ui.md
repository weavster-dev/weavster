# Web UI

The server includes a small read-only web page that draws the [topology](topology.md): every
flow and how flows route to each other, and for one flow its source, transform, and destinations
with their status and message counts. It changes nothing. Create and change flows with the API,
the command-line client, or `weavster config apply`.

## Open it

Open `/ui/` on the server's address, for example `http://127.0.0.1:8080/ui/` (the server's root,
`/`, leads there too). With [`listen.contextPath`](server-config.md#listen) set to `/weavster`,
open `http://127.0.0.1:8080/weavster/ui/`.

Sign in with a user that has the `flows:view` permission (or `admin`). The page signs in through
`POST /api/v1/auth/login` and keeps the session token only in the browser tab, never in a cookie.
**Sign out** ends the session on the server. Closing the tab only makes the page forget the
token: the session itself stays valid until it expires (12 hours), so sign out on a shared
computer. A user who must still change their password is told to do that first
(with the command-line client or `POST /api/v1/auth/password`); the page cannot change passwords.

## What it shows

- **All flows** (`#/`): one box per flow with its status and counts (`in`, `sent`, `err`,
  `queued`), and arrows for routes between flows; dashed arrows are `dependsOn`. Select a flow
  to open it.
- **One flow** (`#/flows/<id>`): the source, the transform, and each destination, with the
  arrows messages take. A destination that sends to another flow has an arrow to that flow;
  select it to open that flow.
- Colours follow the status: green `started`, grey `stopped`/`undeployed`/`halted`, amber
  `paused`, blue `deployed`, red `errored`. Arrows are green while messages cross them, red when
  most of those fail, and grey when idle. Hover over a box or arrow for its id and counts.

The page reloads the graph every 5 seconds while the tab is visible. See [Topology](topology.md)
for what the statuses and counts mean.

## Security

- The page's own files (`/ui/…`) are public; everything it shows comes from the API with the
  signed-in user's token and permissions. Without `flows:view` the page says so.
- It is served with a strict Content Security Policy (only its own script, style, and API; no
  inline code, no framing).
- Its only requests that are not reads are sign-in and sign-out.

## Common pitfalls

- **A blank page or "cannot be reached"**: the page calls the API at `../api/v1/` next to
  `/ui/`, so a reverse proxy must forward both `/ui/` and `/api/` (under the same context path).
- **Signed out after a restart**: sessions are held in memory, so a server restart ends them;
  sign in again.
- **No layout saved**: boxes are placed automatically in columns along the arrows, every time.
