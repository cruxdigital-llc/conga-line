import { readFileSync, watch } from 'fs';
import { createHmac } from 'crypto';
import { createServer } from 'http';

// Load routing config
const CONFIG_PATH = process.env.ROUTER_CONFIG || '/opt/conga/config/telegram-routing.json';
let config;

function loadConfig() {
  const raw = JSON.parse(readFileSync(CONFIG_PATH, 'utf-8'));
  console.log(`[telegram-router] Loaded config: ${Object.keys(raw.channels || {}).length} channels, ${Object.keys(raw.members || {}).length} members`);
  return raw;
}

try {
  config = loadConfig();
} catch (err) {
  console.error(`[telegram-router] Failed to load config from ${CONFIG_PATH}:`, err.message);
  process.exit(1);
}

// Watch for config changes and hot-reload
let reloadTimer;
watch(CONFIG_PATH, () => {
  clearTimeout(reloadTimer);
  reloadTimer = setTimeout(() => {
    try {
      config = loadConfig();
    } catch (err) {
      console.error(`[telegram-router] Config reload failed, keeping previous config:`, err.message);
    }
  }, 500);
});

const botToken = process.env.TELEGRAM_BOT_TOKEN;
const webhookSecret = process.env.TELEGRAM_WEBHOOK_SECRET || '';
const signingSecret = process.env.SLACK_SIGNING_SECRET || webhookSecret;

if (!botToken) { console.error('[telegram-router] TELEGRAM_BOT_TOKEN required'); process.exit(1); }

const TELEGRAM_API = `https://api.telegram.org/bot${botToken}`;

// Deduplication by update_id
const recentUpdates = new Map();
const DEDUP_TTL_MS = 30_000;

function isDuplicate(updateId) {
  if (!updateId) return false;
  if (recentUpdates.has(updateId)) return true;
  recentUpdates.set(updateId, Date.now());
  if (recentUpdates.size > 500) {
    const cutoff = Date.now() - DEDUP_TTL_MS;
    for (const [k, v] of recentUpdates) {
      if (v < cutoff) recentUpdates.delete(k);
    }
  }
  return false;
}

function extractUserId(update) {
  return update?.message?.from?.id?.toString()
    || update?.callback_query?.from?.id?.toString()
    || update?.inline_query?.from?.id?.toString()
    || null;
}

function extractChatId(update) {
  return update?.message?.chat?.id?.toString()
    || update?.callback_query?.message?.chat?.id?.toString()
    || null;
}

function resolveTarget(update) {
  const chatId = extractChatId(update);
  const userId = extractUserId(update);

  if (chatId && chatId.startsWith('-') && config.channels?.[chatId]) {
    return { target: config.channels[chatId], reason: `group:${chatId}` };
  }

  if (userId && config.members?.[userId]) {
    return { target: config.members[userId], reason: `user:${userId}` };
  }

  return null;
}

// Send a reply back to Telegram
async function sendTelegramReply(chatId, text) {
  try {
    await fetch(`${TELEGRAM_API}/sendMessage`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ chat_id: chatId, text }),
    });
  } catch (err) {
    console.error(`[telegram-router] Failed to send reply:`, err.message);
  }
}

// Forward a Telegram update to the agent and relay the response back
async function forwardUpdate(target, update) {
  const messageText = update?.message?.text;
  if (!messageText) return; // skip non-text messages for now

  const chatId = extractChatId(update);
  const url = new URL(target);

  // Route through the API server's OpenAI-compatible endpoint (port 8642).
  // The target URL may point to the webhook port (8644), so we override
  // to the API server port for chat completions.
  const apiPort = process.env.AGENT_API_PORT || '8642';
  const apiUrl = `${url.protocol}//${url.hostname}:${apiPort}/v1/chat/completions`;

  const body = JSON.stringify({
    model: 'hermes-agent',
    messages: [{ role: 'user', content: messageText }],
  });

  const headers = { 'Content-Type': 'application/json' };
  // Pass the signing secret as the API server bearer token.
  // The Hermes API server uses API_SERVER_KEY for auth, which is set to
  // the gateway token during provisioning. The router has the signing
  // secret which may differ — use AGENT_API_KEY env var if set.
  const apiKey = process.env.AGENT_API_KEY || '';
  if (apiKey) {
    headers['Authorization'] = `Bearer ${apiKey}`;
  }

  try {
    const res = await fetch(apiUrl, {
      method: 'POST',
      headers,
      body,
    });

    if (!res.ok) {
      const text = await res.text().catch(() => '');
      console.error(`[telegram-router] Agent returned ${res.status}: ${text}`);
      return;
    }

    const data = await res.json();
    const reply = data?.choices?.[0]?.message?.content;
    if (reply && chatId) {
      await sendTelegramReply(chatId, reply);
    }
  } catch (err) {
    console.error(`[telegram-router] Forward error to ${target}:`, err.message);
  }
}

// Process a single update (shared by polling and webhook modes)
function processUpdate(update) {
  if (isDuplicate(update.update_id)) return;

  const userId = extractUserId(update);
  const chatId = extractChatId(update);
  const route = resolveTarget(update);

  if (route) {
    console.log(`[telegram-router] update_id=${update.update_id} → ${route.reason}`);
    forwardUpdate(route.target, update).catch(err =>
      console.error(`[telegram-router] Async forward error:`, err.message)
    );
  } else {
    console.log(`[telegram-router] No route: user=${userId} chat=${chatId} — dropped`);
  }
}

// --- Long Polling Mode (default for local/dev) ---

async function startPolling() {
  // Delete any existing webhook so getUpdates works
  try {
    await fetch(`${TELEGRAM_API}/deleteWebhook`);
  } catch (err) {
    console.error(`[telegram-router] Failed to delete webhook:`, err.message);
  }

  console.log('[telegram-router] Starting long-polling mode...');
  let offset = 0;

  while (true) {
    try {
      const params = new URLSearchParams({
        offset: offset.toString(),
        timeout: '30',
        allowed_updates: JSON.stringify(['message', 'callback_query', 'inline_query']),
      });

      const res = await fetch(`${TELEGRAM_API}/getUpdates?${params}`, {
        signal: AbortSignal.timeout(35_000),
      });
      const data = await res.json();

      if (data.ok && data.result?.length > 0) {
        for (const update of data.result) {
          processUpdate(update);
          offset = update.update_id + 1;
        }
      }
    } catch (err) {
      if (err.name !== 'TimeoutError') {
        console.error(`[telegram-router] Polling error:`, err.message);
        await new Promise(r => setTimeout(r, 5000));
      }
    }
  }
}

// --- Webhook Mode (for production with public URL) ---

async function startWebhook() {
  const webhookUrl = process.env.TELEGRAM_WEBHOOK_URL;
  const webhookPort = parseInt(process.env.TELEGRAM_ROUTER_PORT || '8443', 10);

  // Register webhook with Telegram
  const params = new URLSearchParams({
    url: webhookUrl,
    allowed_updates: JSON.stringify(['message', 'callback_query', 'inline_query']),
  });
  if (webhookSecret) {
    params.set('secret_token', webhookSecret);
  }

  try {
    const res = await fetch(`${TELEGRAM_API}/setWebhook?${params}`);
    const data = await res.json();
    if (data.ok) {
      console.log(`[telegram-router] Webhook registered: ${webhookUrl}`);
    } else {
      console.error(`[telegram-router] Failed to register webhook:`, data.description);
    }
  } catch (err) {
    console.error(`[telegram-router] Webhook registration error:`, err.message);
  }

  // HTTP server for incoming webhook POSTs
  const server = createServer(async (req, res) => {
    if (req.method === 'GET' && req.url === '/health') {
      res.writeHead(200, { 'Content-Type': 'application/json' });
      res.end(JSON.stringify({ status: 'ok', platform: 'telegram-router' }));
      return;
    }

    if (req.method !== 'POST') {
      res.writeHead(405);
      res.end('Method Not Allowed');
      return;
    }

    if (webhookSecret) {
      const headerSecret = req.headers['x-telegram-bot-api-secret-token'];
      if (headerSecret !== webhookSecret) {
        res.writeHead(403);
        res.end('Forbidden');
        return;
      }
    }

    const chunks = [];
    for await (const chunk of req) chunks.push(chunk);
    const body = Buffer.concat(chunks).toString();

    let update;
    try {
      update = JSON.parse(body);
    } catch {
      res.writeHead(400);
      res.end('Bad Request');
      return;
    }

    res.writeHead(200);
    res.end('ok');

    processUpdate(update);
  });

  server.listen(webhookPort, () => {
    console.log(`[telegram-router] Webhook server listening on port ${webhookPort}`);
  });
}

// --- Start ---

console.log('[telegram-router] Starting Conga Line Telegram event router...');

if (process.env.TELEGRAM_WEBHOOK_URL) {
  startWebhook();
} else {
  startPolling();
}
