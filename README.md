<p align="center">
  <img src="app/assets/images/icon-144.png" width="120" alt="HyperBerry icon">
</p>

<h1 align="center">HyperBerry</h1>

<p align="center">
  <b>A native WhatsApp client for BlackBerry 10</b><br>
  Made for the Q10 and its keyboard. Works on the Classic, Passport and the rest of the BB10 family.
</p>

<p align="center">
  <img alt="Version" src="https://img.shields.io/badge/version-1.0.0-25D366">
  <img alt="Platform" src="https://img.shields.io/badge/platform-BlackBerry%2010.3-222222">
  <img alt="UI" src="https://img.shields.io/badge/UI-Cascades%20%2F%20Qt%204.8-41CD52">
  <img alt="Engine" src="https://img.shields.io/badge/engine-Go-00ADD8">
  <img alt="License" src="https://img.shields.io/badge/license-GPLv3-blue">
</p>

---

HyperBerry makes your BlackBerry 10 phone a WhatsApp **linked device**, the same way WhatsApp Web
works. You scan a QR code or type a pairing code on your main phone, and your chats, groups, calls
and stories show up in an app that looks and behaves like a stock BB10 app: Hub notifications,
an Active Frame, the action bar, keyboard shortcuts.

> [!NOTE]
> HyperBerry is an independent project. It is not affiliated with or endorsed by WhatsApp or Meta.
> Using an unofficial client may break WhatsApp's terms of service. Use it at your own risk.

## Features

**Chats**
- Linking by QR code or pairing code, with a country picker
- Chat list with pinned, muted, archived and marked-unread chats, unread badges and search
- 1:1 and group chats: replies, receipts, typing, online and last seen
- Reactions, mentions, pinned messages, polls, link previews and view-once messages
- Edit your sent messages, delete for you or for everyone
- `*bold*` `_italic_` `~strike~` `` `mono` `` text styles
- Every emoji in colour (Google's Noto Color Emoji) with its own picker
- Search inside a chat
- Translation: long-press a message to translate it, or pick a language for a chat and read its incoming
  messages translated in place. Google, DeepL or your own LibreTranslate server do the work

**Media**
- Photos, albums, videos, stickers, documents, locations and contacts
- Voice notes: waveform playback and recording from the composer
- Share files into HyperBerry from other apps

**Calls**
- 1:1 voice and video calls
- Group voice calls and group video calls, up to four people on screen
- Reconnects on a shaky network, rings with the phone's ringtone

**Stories**
- A Stories tab: watch, reply, like and react to your contacts' status posts
- Post photo, video and text stories, and choose who sees them

**Groups and channels**
- Group info, members, admins, invite links and new groups
- Channels

**BlackBerry 10 integration**
- A Hub account, with one item per chat
- Messages keep arriving after you close the app, and the engine starts at boot
- Active Frame with the unread chats
- Light and dark themes, text size, "Enter sends", notification preview
- Sized for square and tall screens: Q10, Q5, Classic, Passport, Z10, Z30
- English, Italian, German, Spanish, French, Polish, Russian, Ukrainian, Hindi, Indonesian, Vietnamese

## How it works

```
             one .bar, one sandbox
 ┌─────────────────────────┐            ┌──────────────────────────┐
 │ hyperberry   (UI)       │  loopback  │ hyperberryd   (engine)   │
 │ Cascades · Qt 4.8 · C++ │◄──────────►│ Go · headless service    │◄──► WhatsApp
 │                         │  TCP+token │                          │
 └───────────┬─────────────┘            └────────────┬─────────────┘
             │ reads                         writes  │
             └──────────────► data/chats.db ◄────────┘
```

HyperBerry ships as one `.bar` with two processes:

- **`hyperberryd`, the engine**, written in Go and cross-compiled for BB10 (QNX on ARMv7) with
  [go-qnx](https://github.com/XIAZY/go-qnx). It speaks WhatsApp's protocol through
  [hypermeow](https://github.com/polymorfa/hypermeow), a fork of whatsmeow, and handles calls through
  [meowcaller](https://github.com/purpshell/meowcaller). It runs as a BB10 headless service, so the
  connection outlives the UI. It stores messages in SQLite and posts Hub notifications.
- **`hyperberry`, the UI**, a native Cascades app (Qt 4.8, C++ and QML). It reads `chats.db`
  directly, so the chat list and history show at once, even while the engine is still starting.
  It sends commands to the engine over a token-protected loopback socket as NDJSON.

The message cards and the chat list rows are drawn in C++ for speed on the Q10's dual-core Krait.
Video calls decode H.264 on the phone's hardware decoders through OpenMAX IL, and the camera is
encoded the same way. Call audio runs in the UI process because the headless engine has a capped heap.

The engine also has a **simulated account**, which lets the whole app be built and tested on a
phone without linking a real number.

## Building

You need Docker and the local images `bb10-cxx20` and `yamsergey/bb10-ndk:0.6`.

```sh
./scripts/test-engine.sh     # the engine's tests, on the PC
./scripts/build-engine.sh    # build/hyperberryd for BB10
./scripts/build-app.sh       # build/hyperberry.bar: engine + UI, unsigned
```

`build-app.sh` also needs [authman-rlimit](https://github.com/zsavajji/authman-rlimit) cloned beside this
repo as `../rlimit-patch`: the app bundles its `headless-mem` addon, which raises BB10's 30 MB memory cap for
background services to 128 MB, and can install it from Settings > Data and storage.

`build-app.sh` fetches the pieces that aren't committed (Google's emoji font, qrcodegen, libwebp,
the OpenMAX headers) and compiles the translations. The resulting `.bar` is unsigned: install it on
a BB10 phone in Development Mode.

## License

Copyright © 2026 Andrea Faggin.

HyperBerry is free software: you can redistribute it and/or modify it under the terms of the
[GNU General Public License v3.0](LICENSE). The engine links
[libsignal-protocol-go](https://github.com/polymorfa/libsignal-protocol-go), which is GPL-3.0, so the
app as a whole is GPL-3.0 too.

The libraries keep their own licenses: hypermeow is MPL-2.0 for the files it takes from whatsmeow
and MIT for the rest, meowcaller is MIT, and Noto Color Emoji is under the SIL Open Font License.
