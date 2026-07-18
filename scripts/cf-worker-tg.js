/**
 * Telegram API reverse proxy for CFST Panel.
 *
 * Deploy on Cloudflare Workers, then fill the worker URL into panel:
 *   Telegram API 中转地址 = https://tg-api.xxx.workers.dev
 *
 * Panel will call:
 *   {API_BASE}/bot{TOKEN}/sendMessage
 *
 * This worker simply forwards:
 *   /bot...  -> https://api.telegram.org/bot...
 *   /file... -> https://api.telegram.org/file...
 */
export default {
  async fetch(request) {
    const url = new URL(request.url);
    const path = url.pathname + url.search;

    // health check
    if (request.method === "GET" && (url.pathname === "/" || url.pathname === "/health")) {
      return new Response(JSON.stringify({ ok: true, service: "tg-api-proxy" }), {
        headers: { "Content-Type": "application/json" },
      });
    }

    // only proxy Telegram API paths
    if (!path.startsWith("/bot") && !path.startsWith("/file")) {
      return new Response(JSON.stringify({ ok: false, error: "only /bot* and /file* are proxied" }), {
        status: 404,
        headers: { "Content-Type": "application/json" },
      });
    }

    const target = "https://api.telegram.org" + path;
    const headers = new Headers(request.headers);
    headers.delete("host");
    headers.delete("cf-connecting-ip");
    headers.delete("cf-ipcountry");
    headers.delete("cf-ray");
    headers.delete("cf-visitor");
    headers.delete("x-forwarded-proto");
    headers.delete("x-real-ip");

    const init = {
      method: request.method,
      headers,
      redirect: "follow",
    };
    if (request.method !== "GET" && request.method !== "HEAD") {
      init.body = await request.arrayBuffer();
    }

    const resp = await fetch(target, init);
    const outHeaders = new Headers(resp.headers);
    // avoid leaking upstream hop-by-hop headers issues
    outHeaders.delete("content-encoding");
    outHeaders.delete("transfer-encoding");
    return new Response(resp.body, {
      status: resp.status,
      statusText: resp.statusText,
      headers: outHeaders,
    });
  },
};
