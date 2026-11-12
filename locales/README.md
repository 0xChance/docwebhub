# Translations

Every user-visible string lives here, one JSON file per language:

```
locales/
├── en.json      # source of truth — edit this one first
├── zh-CN.json
├── zh-TW.json
├── ja.json
├── ko.json
├── es.json
├── fr.json
├── de.json
├── pt-BR.json
├── ru.json
└── locales.go   # embeds *.json into the binary
```

The file name is the language tag (`en`, `zh-CN`, `pt-BR`, …). Dropping a new
`xx.json` in this folder is all it takes to add a language — no code change, no
rebuild of anything except the binary.

## Format

A flat map of `key: string`, plus two special keys:

```json
{
  "language.name": "简体中文",
  "language.english": "Chinese (Simplified)",

  "ui.port.label": "端口",
  "ui.auth.on": "已开启：%s"
}
```

Rules:

1. **Keep every key, in the same order.** A missing key falls back to the
   English value at runtime, so a half-translated file never breaks the app —
   but it does look untranslated.
2. **Translate values only.** Never rename a key, and never touch
   `language.english` (it stays in English so the language menu is readable
   whatever language is active).
3. **Keep the placeholders.** `%s` and `%d` are filled in by the app; drop one
   and the app crashes. They may be reordered if the grammar needs it.
4. **Keep the punctuation and spacing style of the key's purpose** — `…` for
   menu items that open a dialog, `●` at the start of the status line.
5. **Do not translate the product name** "docWebHub", the word "Tailscale",
   or file paths.
6. **UI text must stay short.** The window is 560 px wide: button labels fit
   about 18 Latin characters, row labels ("Port", "Bind", "Auth") about 8.
   Tooltips and the two `ui.intro.*` sentences have more room.
7. `web.listing.folder.one` / `.other` and the same pair for `file` are the
   singular and plural forms; `.other` is used for every count except 1.

## Adding or updating a language

1. Copy `en.json` to `<tag>.json`.
2. Translate every value.
3. Check it:

   ```sh
   go test ./internal/i18n/
   ```

   The test fails if a file is not valid JSON, has a different key set from
   `en.json`, or drops a `%s` / `%d` placeholder.

## Asking an AI to translate a file

The files are deliberately flat and comment-free so this works:

> Translate `locales/zh-CN.json` into Japanese and write `locales/ja.json`.
> Keep every key in the same order, translate values only, keep `language.english`
> in English, keep the `%s` / `%d` placeholders, and keep UI strings short because
> the app window is 560 px wide. Then run `go test ./internal/i18n/`.

## Overriding translations without rebuilding

The embedded files are used by default. To test edits in place, point the app at
a directory of JSON files:

```sh
WEBDOCAST_LOCALES=./locales ./build/docwebhub
```

A `locales` folder next to the executable is picked up too.
