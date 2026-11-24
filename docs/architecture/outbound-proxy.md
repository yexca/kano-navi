# Outbound Proxies

The operator can send the server's own outbound requests through forward
proxies. Proxies never carry visitor traffic; page loads still read only the
local API. `backend/internal/outboundproxy` owns the configuration, its
storage, the per-area transport and the [admin routes](admin-api/sources.md#outbound-proxies).

## Areas

Each workflow and admin client takes its `http.Client` transport from the
pool for one area:

| Area       | Requests                                                                             |
| ---------- | ------------------------------------------------------------------------------------ |
| `x`        | X profile, FxTwitter and vxtwitter reads, the X API (timed chain and manual fetches) |
| `youtube`  | YouTube pages, feeds, the Data API, WebSub subscribe requests and yt-dlp reads       |
| `bilibili` | The Bilibili API                                                                     |
| `shop`     | The official shop's product and collection JSON and catalog Atom feed                |
| `media`    | Post, video and profile media downloads, profile page reads, niconico cover lookups  |
| `llm`      | Model provider requests: extraction, translation, provider tests, model discovery    |
| `notify`   | [Operator alert](alerts.md) deliveries (ntfy, Telegram, webhooks) and the heartbeat  |

The health check and the read-only flag mode make no source requests and are
not routed.

## Configuration

The configuration is one ordered proxy queue, one route per area and a
direct-fallback switch:

- A proxy has an optional name, which labels it in the console's queue, a
  protocol (`http`, `https`, `socks5` or `socks5h`), a hostname or IP
  address, a port, an optional username and password, and an in-use switch. The queue holds at most 16 proxies.
- A route is off (the area connects directly) or on with a proxy selection.
  An empty selection means every proxy; either way proxies are tried in
  queue order. A selection keeps the ids it names when a proxy is removed, so
  it remains an explicit selection with no usable proxy instead of becoming
  the all-proxy route. A route can be turned on only while the queue has a
  proxy.
- With the direct fallback, a routed request connects directly after every
  proxy failed to answer, or when the route has no usable proxy. Without it,
  such a request fails and the source records its failure as for any other
  unreachable upstream.

The configuration is stored as JSON in `app_settings` under `outbound_proxy`.
Proxy passwords are sealed with `LLM_SECRETS_KEY` in the
[secret store format](../development/security.md#external-request-rules) and
never returned by the admin API. A password that no longer decrypts (a
changed key) marks its proxy unreadable: the proxy is skipped and keeps its
sealed value until the operator enters the password again. Stored JSON that
cannot be parsed fails every routed and unrouted request closed with
`outbound proxy setting is unavailable` until an operator save replaces it.
Fresh databases have no row and no proxies.

## Queue Attempts

The pool resolves the area's route on every request, so a saved change
applies to the next request without a restart. A request is sent through the
first usable proxy (in use, selected, with a readable password) and moves to
the next one only when:

- the previous proxy produced no response; an HTTP status, including an
  upstream error, is an answer and is returned as is;
- the request context is still live;
- the body can be replayed; and
- for a non-idempotent request, such as a model provider `POST`, the request
  was never written, so a proxy that drops the connection mid-request cannot
  make the provider receive it twice.

A proxy that failed to answer stays behind the proxies that have not for one
minute. Every proxy is still attempted when all of them failed recently.
This health is shared by all areas, kept in process memory, and reset when a
proxy's endpoint or credentials change; the admin API reports each proxy's
last success, last failure and cooldown end.

Connections to a proxy are bounded at 10 seconds; each client keeps its own
request timeout, redirect policy and response limits. Transport errors from a
proxied attempt carry a fixed message, so proxy addresses and credentials do
not reach stored errors.

Destinations on this machine or a local network connect directly regardless
of the route: `localhost`, names ending in `.localhost`, `.local`, `.lan`,
`.internal` or `.home.arpa`, and loopback, private, link-local and
unspecified IP literals. Every source host is public, so this only affects
model providers on the local network. An area that is not routed uses the
process default transport, which honors the standard proxy environment
variables.

## yt-dlp

yt-dlp is a separate program, so its upload listings and video inspections
follow the `youtube` route through process attempts instead of a transport.
Each attempt starts yt-dlp with the process environment minus every
`*_PROXY` variable, plus `HTTP_PROXY`, `HTTPS_PROXY` and `ALL_PROXY` (and
their lowercase forms) set to one proxy URL. Credentials therefore stay out of
the argument list; they are visible only to the process owner through the
child's environment.

A read tries at most the first three usable proxies in queue order, because
each attempt may use yt-dlp's whole 45-second timeout, then a direct attempt
with the direct fallback. It moves to the next proxy only when the run did
not reach YouTube: it timed out, or yt-dlp's error line names a
`ProxyError` or `TransportError`. Any other failure, such as an unavailable
video, is the answer. Outcomes update proxy health like requests do. An
unrouted `youtube` area starts yt-dlp with the process environment
unchanged; a routed area with nothing to try records
`youtube_ytdlp_proxy_unavailable` without starting it. The version check and
the self-update keep the process environment.

## Proxy Tests

`POST /api/admin/proxy/{id}/test` reaches one fixed public page of an area
through one saved proxy, whether or not it is in use, with no fallback to
another proxy or a direct connection. Any HTTP answer is a success. The
pages belong to hosts the area already reads:

| Area       | Page                                   |
| ---------- | -------------------------------------- |
| `x`        | `https://api.fxtwitter.com/`           |
| `youtube`  | `https://www.youtube.com/generate_204` |
| `bilibili` | `https://www.bilibili.com/`            |
| `shop`     | `https://shop.milpr.com/robots.txt`    |
| `media`    | `https://pbs.twimg.com/`               |

Model providers have no fixed page; a provider test on `/admin` →
Detection already goes through the `llm` route. A test is an operator action, bounded
at 10 seconds and 64 KiB of response, and its outcome updates the proxy's
health.

## Related Docs

- [Admin API](admin-api/sources.md#outbound-proxies)
- [Sources](sources.md)
- [Secure development](../development/security.md#external-request-rules)
