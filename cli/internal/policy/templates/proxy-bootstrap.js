// Conga Line egress proxy bootstrap — patches Node.js to route through HTTPS_PROXY
// Loaded via NODE_OPTIONS="--require ..." before the application starts.
//
// Why this is needed: Node.js's built-in fetch() does not honor HTTPS_PROXY env vars,
// and axios's built-in proxy support uses regular HTTP requests instead of CONNECT tunneling.
//
// What it does:
//  1. Sets undici's EnvHttpProxyAgent as the global fetch dispatcher
//  2. Replaces https.globalAgent with a CONNECT tunnel agent (pure built-in modules)
//  3. Saves the proxy URL in __CONGA_PROXY_URL so child processes can re-discover it
//
// Injected via NODE_OPTIONS="--require /opt/proxy-bootstrap.js" in the container.
// Assumes undici is installed at /app/node_modules/undici (OpenClaw image layout).
'use strict';
const proxyUrl = process.env.HTTPS_PROXY || process.env.HTTP_PROXY || process.env.__CONGA_PROXY_URL;
if (proxyUrl) {
  const http = require('http');
  const https = require('https');
  const tls = require('tls');
  const { URL } = require('url');

  if (!process.env.HTTPS_PROXY) process.env.HTTPS_PROXY = proxyUrl;
  const { EnvHttpProxyAgent, setGlobalDispatcher } = require('/app/node_modules/undici');
  setGlobalDispatcher(new EnvHttpProxyAgent());

  const parsed = new URL(proxyUrl);
  class ConnectProxyAgent extends https.Agent {
    constructor() {
      super({ keepAlive: true });
    }
    createConnection(opts, cb) {
      const req = http.request({
        host: parsed.hostname,
        port: parsed.port,
        method: 'CONNECT',
        path: (opts.host || opts.hostname) + ':' + (opts.port || 443),
      });
      req.on('connect', (_res, socket) => {
        if (_res.statusCode !== 200) {
          cb(new Error('Proxy CONNECT failed: ' + _res.statusCode));
          return;
        }
        const tlsSock = tls.connect({
          socket: socket,
          servername: opts.host || opts.hostname,
        }, () => cb(null, tlsSock));
        tlsSock.on('error', cb);
      });
      req.on('error', cb);
      req.end();
    }
  }
  https.globalAgent = new ConnectProxyAgent();

  process.env.__CONGA_PROXY_URL = proxyUrl;
  process.env.HTTPS_PROXY = proxyUrl;
  process.env.HTTP_PROXY = proxyUrl;
}
