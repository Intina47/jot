import fs from 'node:fs';
import fsPromises from 'node:fs/promises';
import path from 'node:path';
import process from 'node:process';
import { fileURLToPath } from 'node:url';
import makeWASocket, {
  Browsers,
  fetchLatestBaileysVersion,
  jidNormalizedUser,
  makeInMemoryStore,
  useMultiFileAuthState,
} from '@whiskeysockets/baileys';
import pino from 'pino';
import QRCode from 'qrcode';

const rootDir = path.dirname(fileURLToPath(import.meta.url));
const stateDir = path.resolve(process.env.JOT_WHATSAPP_BRIDGE_DIR || path.join(rootDir, '.state'));
const authDir = path.join(stateDir, 'auth');
const storePath = path.join(stateDir, 'store.json');
const qrPath = path.join(stateDir, 'last-qr.png');
const connectTimeoutMs = parseIntSafe(process.env.JOT_WHATSAPP_BRIDGE_TIMEOUT_MS, 20000);
const syncGraceMs = parseIntSafe(process.env.JOT_WHATSAPP_BRIDGE_SYNC_GRACE_MS, 2500);

async function main() {
  try {
    await fsPromises.mkdir(stateDir, { recursive: true });
    await fsPromises.mkdir(authDir, { recursive: true });
    const request = await readRequest();
    const response = await handleRequest(request);
    await writeResponse(response);
  } catch (error) {
    await writeResponse({ ok: false, error: errorMessage(error) });
  }
}

async function handleRequest(request) {
  if (!request || request.channel !== 'whatsapp') {
    return { ok: false, error: 'unsupported channel' };
  }
  const action = stringValue(request.action);
  switch (action) {
    case 'status':
      return withSession(async (session) => {
        return {
          ok: true,
          status: sessionStatus(session),
        };
      });
    case 'list_threads':
      return withSession(async (session) => {
        if (!session.connected) {
          return {
            ok: true,
            status: sessionStatus(session),
            threads: [],
          };
        }
        const limit = clampInt(request.params?.limit, 10, 1, 50);
        const threads = listThreadsFromStore(session, limit);
        return {
          ok: true,
          status: sessionStatus(session),
          threads,
        };
      });
    case 'read_thread':
      return withSession(async (session) => {
        if (!session.connected) {
          return {
            ok: false,
            error: session.detail || 'whatsapp is not connected',
          };
        }
        const threadID = stringValue(request.params?.thread_id);
        if (!threadID) {
          return { ok: false, error: 'thread_id is required' };
        }
        const limit = clampInt(request.params?.limit, 20, 1, 100);
        const thread = await readThreadFromStore(session, threadID, limit);
        return {
          ok: true,
          status: sessionStatus(session),
          thread,
        };
      });
    case 'send_message':
      return withSession(async (session) => {
        if (!session.connected) {
          return {
            ok: false,
            error: session.detail || 'whatsapp is not connected',
          };
        }
        const threadID = stringValue(request.params?.thread_id);
        const body = stringValue(request.params?.body);
        if (!threadID) {
          return { ok: false, error: 'thread_id is required' };
        }
        if (!body) {
          return { ok: false, error: 'body is required' };
        }
        const sent = await session.sock.sendMessage(threadID, { text: body });
        await persistStore(session);
        return {
          ok: true,
          status: sessionStatus(session),
          send: {
            id: stringValue(sent?.key?.id),
            threadId: stringValue(sent?.key?.remoteJid || threadID),
            sentAt: new Date().toISOString(),
          },
        };
      });
    default:
      return { ok: false, error: `unsupported action: ${action || 'unknown'}` };
  }
}

async function withSession(fn) {
  const logger = pino({ level: process.env.JOT_WHATSAPP_BRIDGE_LOG_LEVEL || 'silent' });
  const store = makeInMemoryStore({ logger });
  if (fs.existsSync(storePath)) {
    try {
      store.readFromFile(storePath);
    } catch {
      // ignore corrupted cache and rebuild from sync
    }
  }

  const { state, saveCreds } = await useMultiFileAuthState(authDir);
  const socketOptions = {
    auth: state,
    logger,
    printQRInTerminal: false,
    markOnlineOnConnect: false,
    syncFullHistory: false,
    browser: Browsers.macOS('Jot Assistant'),
  };
  try {
    const { version } = await fetchLatestBaileysVersion();
    if (Array.isArray(version) && version.length > 0) {
      socketOptions.version = version;
    }
  } catch {
    // Fall back to Baileys' bundled default when version lookup is unavailable.
  }
  const sock = makeWASocket(socketOptions);
  store.bind(sock.ev);
  sock.ev.on('creds.update', saveCreds);

  const session = {
    sock,
    store,
    connected: false,
    accountLabel: '',
    detail: '',
    qrText: '',
  };

  const outcome = await waitForConnection(sock, session);
  if (outcome === 'open') {
    await sleep(syncGraceMs);
    session.connected = true;
    session.accountLabel = userLabel(sock.user);
    session.detail = session.accountLabel ? `paired as ${session.accountLabel}` : 'paired';
    await removeQR();
  } else if (!session.detail) {
    session.detail = 'connection did not open in time';
  }

  try {
    const result = await fn(session);
    await persistStore(session);
    return result;
  } finally {
    try {
      await persistStore(session);
    } catch {
      // ignore persist failures on shutdown
    }
    try {
      if (session.sock?.end) {
        session.sock.end(undefined);
      }
    } catch {
      // ignore socket shutdown failures
    }
  }
}

async function waitForConnection(sock, session) {
  return new Promise((resolve) => {
    let settled = false;
    const finish = (value) => {
      if (settled) {
        return;
      }
      settled = true;
      clearTimeout(timer);
      resolve(value);
    };
    const timer = setTimeout(() => finish('timeout'), connectTimeoutMs);

    sock.ev.on('connection.update', async (update) => {
      if (update.qr) {
        session.qrText = update.qr;
        await writeQR(update.qr);
        session.detail = `scan the WhatsApp QR at ${qrPath}`;
      }
      if (update.connection === 'open') {
        finish('open');
        return;
      }
      if (update.connection === 'close') {
        const reason = disconnectReason(update.lastDisconnect);
        if (reason) {
          session.detail = reason;
        }
        finish('close');
      }
    });
  });
}

function sessionStatus(session) {
  return {
    connected: Boolean(session?.connected),
    accountLabel: stringValue(session?.accountLabel),
    detail: stringValue(session?.detail),
  };
}

function listThreadsFromStore(session, limit) {
  const chats = storeChats(session.store);
  const threads = chats
    .filter(isDirectChat)
    .sort((left, right) => chatTimestamp(right) - chatTimestamp(left))
    .slice(0, limit)
    .map((chat) => normalizeThread(session.store, chat));
  return threads;
}

async function readThreadFromStore(session, threadID, limit) {
  const messages = await loadMessages(session.store, threadID, limit);
  const chat = storeChats(session.store).find((item) => stringValue(item?.id) === threadID);
  const base = normalizeThread(session.store, chat || { id: threadID });
  base.messages = messages
    .sort((left, right) => messageTimestamp(left) - messageTimestamp(right))
    .map((item) => normalizeMessage(session.store, threadID, item, userJid(session.sock)));
  return base;
}

async function loadMessages(store, jid, limit) {
  if (store && typeof store.loadMessages === 'function') {
    const items = await store.loadMessages(jid, limit, undefined);
    if (Array.isArray(items)) {
      return items;
    }
  }
  const bucket = store?.messages?.[jid];
  if (bucket && typeof bucket.array === 'function') {
    return bucket.array.slice(-limit);
  }
  if (Array.isArray(bucket)) {
    return bucket.slice(-limit);
  }
  return [];
}

function normalizeThread(store, chat) {
  const jid = stringValue(chat?.id);
  const contact = store?.contacts?.[jid] || {};
  return {
    threadId: jid,
    contactId: jid,
    contactLabel: contactLabel(chat, contact, jid),
    snippet: threadSnippet(chat),
    lastMessageAt: timestampISO(chatTimestamp(chat)),
    unreadCount: clampInt(chat?.unreadCount, 0, 0, 1000000),
  };
}

function normalizeMessage(store, threadID, msg, selfJid) {
  const senderID = messageSenderID(msg, selfJid);
  const contact = store?.contacts?.[senderID] || {};
  return {
    id: stringValue(msg?.key?.id),
    threadId: stringValue(threadID),
    senderId: senderID,
    senderLabel: contactLabel(null, contact, senderID),
    text: extractMessageText(msg?.message),
    sentBySelf: Boolean(msg?.key?.fromMe),
    timestamp: timestampISO(messageTimestamp(msg)),
  };
}

function storeChats(store) {
  if (!store?.chats) {
    return [];
  }
  if (typeof store.chats.all === 'function') {
    return store.chats.all();
  }
  if (Array.isArray(store.chats)) {
    return store.chats;
  }
  return Object.values(store.chats);
}

function isDirectChat(chat) {
  const id = stringValue(chat?.id);
  return id.endsWith('@s.whatsapp.net') || id.endsWith('@lid');
}

function threadSnippet(chat) {
  return stringValue(chat?.name);
}

function contactLabel(chat, contact, fallback) {
  const candidates = [
    stringValue(chat?.name),
    stringValue(contact?.name),
    stringValue(contact?.verifiedName),
    stringValue(contact?.notify),
    stringValue(contact?.pushname),
    stringValue(fallback),
  ];
  for (const candidate of candidates) {
    if (candidate) {
      return candidate;
    }
  }
  return '';
}

function messageSenderID(message, selfJid) {
  if (message?.key?.fromMe) {
    return selfJid;
  }
  return stringValue(message?.key?.participant || message?.key?.remoteJid);
}

function chatTimestamp(chat) {
  return epochSeconds(chat?.conversationTimestamp || chat?.lastMessageRecvTimestamp || chat?.muteEndTime || 0);
}

function messageTimestamp(message) {
  return epochSeconds(message?.messageTimestamp || 0);
}

function epochSeconds(value) {
  if (typeof value === 'number') {
    return value;
  }
  if (typeof value === 'string') {
    const parsed = Number.parseInt(value, 10);
    return Number.isFinite(parsed) ? parsed : 0;
  }
  if (typeof value === 'bigint') {
    return Number(value);
  }
  if (value && typeof value === 'object') {
    if (typeof value.toNumber === 'function') {
      return value.toNumber();
    }
    if (typeof value.low === 'number') {
      return value.low;
    }
  }
  return 0;
}

function timestampISO(seconds) {
  if (!seconds || seconds <= 0) {
    return '';
  }
  return new Date(seconds * 1000).toISOString();
}

function userLabel(user) {
  const candidates = [
    stringValue(user?.name),
    stringValue(user?.verifiedName),
    stringValue(user?.notify),
    stringValue(user?.id),
  ];
  for (const candidate of candidates) {
    if (candidate) {
      return candidate;
    }
  }
  return '';
}

function userJid(sock) {
  if (!sock?.user?.id) {
    return '';
  }
  try {
    return jidNormalizedUser(sock.user.id);
  } catch {
    return stringValue(sock.user.id);
  }
}

function extractMessageText(message) {
  if (!message || typeof message !== 'object') {
    return '';
  }
  const direct = [
    stringValue(message.conversation),
    stringValue(message.extendedTextMessage?.text),
    stringValue(message.imageMessage?.caption),
    stringValue(message.videoMessage?.caption),
    stringValue(message.documentMessage?.caption),
    stringValue(message.buttonsResponseMessage?.selectedDisplayText),
    stringValue(message.listResponseMessage?.title),
    stringValue(message.listResponseMessage?.singleSelectReply?.selectedRowId),
    stringValue(message.templateButtonReplyMessage?.selectedDisplayText),
    stringValue(message.pollUpdateMessage?.pollCreationMessageKey?.id),
    stringValue(message.contactMessage?.displayName),
  ];
  for (const item of direct) {
    if (item) {
      return item;
    }
  }
  const nested = [
    message.ephemeralMessage?.message,
    message.viewOnceMessage?.message,
    message.viewOnceMessageV2?.message,
    message.viewOnceMessageV2Extension?.message,
    message.documentWithCaptionMessage?.message,
  ];
  for (const item of nested) {
    const text = extractMessageText(item);
    if (text) {
      return text;
    }
  }
  return '';
}

async function persistStore(session) {
  if (!session?.store || typeof session.store.writeToFile !== 'function') {
    return;
  }
  session.store.writeToFile(storePath);
}

async function writeQR(qrText) {
  if (!qrText) {
    return;
  }
  await QRCode.toFile(qrPath, qrText, { type: 'png', margin: 1, width: 320 });
}

async function removeQR() {
  try {
    await fsPromises.unlink(qrPath);
  } catch {
    // ignore missing QR
  }
}

function disconnectReason(lastDisconnect) {
  const statusCode = lastDisconnect?.error?.output?.statusCode;
  if (statusCode === 401) {
    return 'logged out; remove bridge state and pair again';
  }
  if (statusCode) {
    return `connection closed (${statusCode})`;
  }
  return 'connection closed';
}

async function readRequest() {
  const chunks = [];
  for await (const chunk of process.stdin) {
    chunks.push(Buffer.from(chunk));
  }
  const raw = Buffer.concat(chunks).toString('utf8').trim();
  if (!raw) {
    throw new Error('request payload is empty');
  }
  return JSON.parse(raw);
}

async function writeResponse(response) {
  process.stdout.write(`${JSON.stringify(response)}\n`);
}

function parseIntSafe(value, fallback) {
  const parsed = Number.parseInt(String(value ?? ''), 10);
  return Number.isFinite(parsed) ? parsed : fallback;
}

function clampInt(value, fallback, minValue, maxValue) {
  const parsed = parseIntSafe(value, fallback);
  return Math.max(minValue, Math.min(maxValue, parsed));
}

function stringValue(value) {
  return typeof value === 'string' ? value.trim() : '';
}

function errorMessage(error) {
  if (error instanceof Error) {
    return error.message;
  }
  return String(error ?? 'unknown error');
}

function sleep(ms) {
  return new Promise((resolve) => setTimeout(resolve, ms));
}

await main();
