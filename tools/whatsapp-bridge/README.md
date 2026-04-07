# WhatsApp Bridge Contract

Jot's first native WhatsApp milestone talks to a local bridge process over JSON stdin/stdout.

The assistant runs the configured `channels.whatsapp.bridgeCommand` with optional `bridgeArgs`, writes one JSON request to stdin, and expects one JSON response on stdout.

Request:

```json
{
  "channel": "whatsapp",
  "action": "status|list_threads|read_thread|send_message",
  "params": {
    "thread_id": "optional",
    "limit": 20,
    "body": "optional"
  }
}
```

Response:

```json
{
  "ok": true,
  "status": {
    "connected": true,
    "accountLabel": "Ntina",
    "detail": "paired"
  },
  "threads": [
    {
      "threadId": "chat-1",
      "contactId": "12345@s.whatsapp.net",
      "contactLabel": "Palma",
      "snippet": "hey are you coming?",
      "lastMessageAt": "2026-04-06T12:00:00Z",
      "unreadCount": 1
    }
  ],
  "thread": {
    "threadId": "chat-1",
    "contactId": "12345@s.whatsapp.net",
    "contactLabel": "Palma",
    "messages": [
      {
        "id": "msg-1",
        "threadId": "chat-1",
        "senderId": "12345@s.whatsapp.net",
        "senderLabel": "Palma",
        "text": "hey are you coming?",
        "sentBySelf": false,
        "timestamp": "2026-04-06T12:00:00Z"
      }
    ]
  },
  "send": {
    "id": "msg-2",
    "threadId": "chat-1",
    "sentAt": "2026-04-06T12:01:00Z"
  }
}
```

Errors should return:

```json
{
  "ok": false,
  "error": "human-readable error"
}
```

The intended production implementation is a local Baileys-backed adapter.

## What Is Implemented

This directory now contains a real local bridge process:

- `index.mjs`
- `package.json`

It uses:

- `@whiskeysockets/baileys` for the linked WhatsApp session
- `useMultiFileAuthState` for persisted auth
- a local store file for cached chats/messages
- QR export to `tools/whatsapp-bridge/.state/last-qr.png`

When launched by Jot, the bridge state directory is set automatically to a per-user config path via `JOT_WHATSAPP_BRIDGE_DIR`, so pairing data does not depend on the install directory being writable.

## Install

From this directory:

```bash
npm install
```

## Configure Jot

Point Jot at the bridge in your `assistant.json`:

```json
{
  "channels": {
    "whatsapp": {
      "kind": "whatsapp",
      "enabled": true,
      "bridgeCommand": "node",
      "bridgeArgs": [
        "C:\\Users\\mamba\\jot\\tools\\whatsapp-bridge\\index.mjs"
      ],
      "replyPolicy": "draft"
    }
  }
}
```

## Pairing Flow

Run:

```bash
jot assistant auth whatsapp
```

If the bridge is not paired yet, Jot should report a detail like:

- `scan the WhatsApp QR at ...\tools\whatsapp-bridge\.state\last-qr.png`

Open that QR image, scan it from WhatsApp on your phone, then run:

```bash
jot assistant auth whatsapp
```

again. Once the bridge reports `connected`, `jot assistant` can use:

- `whatsapp.status`
- `whatsapp.list_threads`
- `whatsapp.read_thread`
- `whatsapp.draft_reply`

## Notes

- This is a linked-session approach via WhatsApp Web, not the WhatsApp Cloud API.
- Session state is stored locally under `.state/`.
- Jot keeps send behavior confirmation-gated on its side.
