# HyperBerry

A native WhatsApp client for **BlackBerry 10**, made for the Q10 and its keyboard.

The phone becomes a WhatsApp *linked device*, like WhatsApp Web: you scan a QR code or type a
pairing code on your main phone, and your chats and groups appear in an app that looks and
behaves like a stock BB10 app.

It is built from two parts that ship in one `.bar`:

- **`hyperberryd`**, the engine: Go, built for BB10 with [go-qnx](https://github.com/XIAZY/go-qnx).
  It speaks WhatsApp's protocol through [hypermeow](https://github.com/polymorfa/hypermeow),
  runs in the background as a headless service, and keeps the chats in SQLite.
- **`hyperberry`**, the UI: Cascades (Qt 4.8), reading the engine's database directly and
  talking to it over a local socket.

Calls are planned for later, through [meowcaller](https://github.com/purpshell/meowcaller).

> HyperBerry is an independent project, not affiliated with or endorsed by WhatsApp or Meta.
> Using an unofficial client may be against WhatsApp's terms of service; use it at your own risk.

## Status

Early work. The engine links, syncs, sends and receives on the PC and reaches WhatsApp from the
Q10; the UI is still a test page. What is done and what comes next is in
[docs/plan/steps.md](docs/plan/steps.md).

## Building

You need Docker and the local images `bb10-cxx20` and `yamsergey/bb10-ndk:0.6`.

```sh
./scripts/test-engine.sh     # the engine's tests, on the PC
./scripts/build-engine.sh    # build/hyperberryd for BB10
./scripts/build-app.sh       # build/hyperberry.bar: engine + UI, unsigned
```

The `.bar` installs on a rooted BB10 phone in Development Mode.

## Documents

- [PLAN.md](PLAN.md): the plan, its decisions and rules
- [docs/protocol.md](docs/protocol.md): the contract between the UI and the engine

## License

MIT, see [LICENSE](LICENSE). The libraries keep their own licenses. hypermeow, for example, is
MPL-2.0 for the files it takes from whatsmeow and MIT for the rest.
