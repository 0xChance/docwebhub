# Licensing

docWebHub is **dual-licensed**. You pick the license that fits how you want to use it.

## 1. Open source use — AGPL-3.0

The source is released under the [GNU Affero General Public License v3.0](LICENSE).
You may use, study, modify and redistribute it, provided that you:

- keep the copyright notices and the license text,
- license your modified version under AGPL-3.0 as well,
- and, if you let other people interact with your version over a network,
  offer them the corresponding source (section 13).

This covers personal use, evaluation, research, teaching, internal tooling where
source sharing is acceptable, and other open-source projects with compatible terms.
**No payment and no permission are needed.**

## 2. Commercial use — separate license

If you want to use docWebHub **without** the obligations of AGPL-3.0 — for example
because you cannot publish your modifications, or because your legal team will not
accept a copyleft license — a commercial license is available.

Typical cases:

- bundling docWebHub (or a fork of it) into a closed-source product;
- offering it, or a service built on it, as a hosted or managed product;
- shipping it inside an appliance where the source cannot be published.

A commercial license removes the copyleft obligations and can include support terms.
To obtain one, open an issue titled **Commercial license** or write to
`admin@docwebhub.app` with a short description of your use case.

## What still requires nothing

- Running docWebHub on your own machine, for yourself or your team.
- Forking it on GitHub and publishing your changes under AGPL-3.0.
- Using it in academic work, or in another AGPL-3.0 project.
- Reviewing the code, filing issues, sending pull requests.

## Contributions

To keep dual licensing possible, contributions are accepted under AGPL-3.0 **plus**
a license grant that allows the maintainer to offer commercial licenses.
Contributors confirm that grant when they open a pull request.

## Third-party components

docWebHub's Go code has no third-party dependencies: it is built with the Go standard
library and the platform SDKs only. Icon and screenshot assets are original work of
the project.

The directory listing embeds two MIT-licensed JavaScript libraries in
`internal/web/preview.min.js`: PrismJS 1.30.0 (syntax highlighting) and marked
18.1.2 (Markdown rendering). The bundle is generated from
`tools/listing-preview.mjs` by `tools/build-listing-preview.sh` and committed, so
a normal build does not need Node.js. Their MIT licence texts are reproduced
below.

### PrismJS 1.30.0

MIT License

Copyright (c) 2012 Lea Verou

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.

### marked 18.1.2

MIT License

Copyright (c) 2018-2026, MarkedJS. (MIT License)
Copyright (c) 2011-2018, Christopher Jeffrey. (MIT License)

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
