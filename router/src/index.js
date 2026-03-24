import { SocketModeClient } from '@slack/socket-mode';
import { readFileSync, watch } from 'fs';
import { createHmac } from 'crypto';

// Load routing config
const CONFIG_PATH = process.env.ROUTER_CONFIG || '/opt/conga/config/routing.json';
let config;

function loadConfig() {
  const raw = JSON.parse(readFileSync(CONFIG_PATH, 'utf-8'));
  console.log(`[router] Loaded config: ${Object.keys(raw.channels).length} channels, ${Object.keys(raw.members).length} members`);
  return raw;
}

try {
  config = loadConfig();
} catch (err) {
  console.error(`[router] Failed to load config from ${CONFIG_PATH}:`, err.message);
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
      console.error(`[router] Config reload failed, keeping previous config:`, err.message);
    }
  }, 500);
});

const appToken = process.env.SLACK_APP_TOKEN;
const signingSecret = process.env.SLACK_SIGNING_SECRET;
if (!appToken) { console.error('[router] SLACK_APP_TOKEN required'); process.exit(1); }
if (!signingSecret) { console.error('[router] SLACK_SIGNING_SECRET required'); process.exit(1); }

// Socket Mode client
const client = new SocketModeClient({ appToken });

// Extract channel ID from various Slack event payload shapes
function extractChannel(payload) {
  return payload?.event?.channel
    || payload?.event?.item?.channel
    || payload?.channel?.id
    || payload?.channel
    || null;
}

// Extract user ID from various Slack event payload shapes
function extractUser(payload) {
  return payload?.event?.user
    || payload?.user?.id
    || payload?.user
    || null;
}

// Find the target container URL for an event
function resolveTarget(payload) {
  const channel = extractChannel(payload);

  if (channel) {
    if (channel.startsWith('D')) {
      const userId = extractUser(payload);
      if (userId && config.members[userId]) {
        return { target: config.members[userId], reason: `dm:${userId}` };
      }
    }
    if (config.channels[channel]) {
      return { target: config.channels[channel], reason: `channel:${channel}` };
    }
  }

  // Fallback: user-based routing (app_home, etc.)
  const userId = extractUser(payload);
  if (userId && config.members[userId]) {
    return { target: config.members[userId], reason: `user:${userId}` };
  }

  return null;
}

// Compute Slack request signature for the forwarded request
function computeSlackSignature(timestamp, body) {
  const sigBasestring = `v0:${timestamp}:${body}`;
  const signature = createHmac('sha256', signingSecret)
    .update(sigBasestring)
    .digest('hex');
  return `v0=${signature}`;
}

// Forward an event to the target container via HTTP POST
// Sends in Events API HTTP format with proper Slack signature headers
async function forwardEvent(target, payload) {
  const body = JSON.stringify(payload);
  const timestamp = Math.floor(Date.now() / 1000).toString();
  const signature = computeSlackSignature(timestamp, body);

  try {
    const res = await fetch(target, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        'x-slack-signature': signature,
        'x-slack-request-timestamp': timestamp,
      },
      body,
    });
    if (!res.ok) {
      const text = await res.text().catch(() => '');
      console.error(`[router] Forward failed: ${res.status} → ${target} ${text}`);
    }
  } catch (err) {
    console.error(`[router] Forward error to ${target}:`, err.message);
  }
}

// Handle ALL incoming Slack events via the catch-all listener
// SDK v2 emits specific event types that don't match the envelope type names,
// so we use 'slack_event' which fires for everything
// Deduplicate events — Slack may send retries or duplicate envelope deliveries
const recentEvents = new Map();
const DEDUP_TTL_MS = 30_000;

function isDuplicate(body) {
  const eventId = body?.event_id || body?.event?.client_msg_id;
  if (!eventId) return false;
  if (recentEvents.has(eventId)) return true;
  recentEvents.set(eventId, Date.now());
  // Prune old entries
  if (recentEvents.size > 500) {
    const cutoff = Date.now() - DEDUP_TTL_MS;
    for (const [k, v] of recentEvents) {
      if (v < cutoff) recentEvents.delete(k);
    }
  }
  return false;
}

client.on('slack_event', async ({ body, ack }) => {
  // Ack immediately — Slack requires this within 3 seconds
  if (ack) await ack();

  // Deduplicate retries and duplicate envelope deliveries
  if (isDuplicate(body)) {
    return;
  }

  const eventType = body?.event?.type || body?.type || 'unknown';
  const subtype = body?.event?.subtype;

  // Drop events the containers should never see:
  // - app_mention: Slack fires both 'message' and 'app_mention' for @-mentions
  // - assistant_thread_started: triggers a duplicate response in channel contexts
  // - bot_message / message_changed / message_deleted: bot's own activity echoed back
  if (eventType === 'app_mention' || eventType === 'assistant_thread_started') {
    return;
  }
  if (subtype && ['bot_message', 'message_changed', 'message_deleted'].includes(subtype)) {
    return;
  }
  // Drop messages from bots (covers bot users that don't use the bot_message subtype)
  if (body?.event?.bot_id) {
    return;
  }

  const route = resolveTarget(body);

  if (route) {
    console.log(`[router] ${eventType} → ${route.reason}`);
    forwardEvent(route.target, body).catch(err =>
      console.error(`[router] Async forward error:`, err.message)
    );
  } else {
    const channel = extractChannel(body);
    const user = extractUser(body);
    console.log(`[router] No route: type=${eventType} channel=${channel} user=${user} — dropped`);
  }
});

// Connection lifecycle
client.on('connected', () => {
  console.log('[router] Socket Mode connected to Slack');
});

client.on('disconnected', () => {
  console.log('[router] Socket Mode disconnected — SDK will reconnect');
});

// Start
console.log('[router] Starting OpenClaw Slack event router...');
client.start().catch(err => {
  console.error('[router] Fatal startup error:', err);
  process.exit(1);
});
