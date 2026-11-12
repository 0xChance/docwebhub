// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

// Writes the font catalog the CAD viewer asks for.
//
// The viewer looks every text style name up in <baseUrl>/fonts/fonts.json before
// it draws anything, and reports whatever it cannot find as a missing font. The
// names drawings actually use belong to fonts this project may not ship, so
// aliases.json maps them onto the two open fonts vendored here instead; this
// script only turns that mapping into the catalog shape the viewer parses.
//
// Usage: node tools/cad-fonts/make-catalog.mjs <output.json>

import { readFileSync, writeFileSync } from 'node:fs';

const out = process.argv[2];
if (!out) {
  console.error('usage: make-catalog.mjs <output.json>');
  process.exit(1);
}

const aliases = JSON.parse(readFileSync(new URL('./aliases.json', import.meta.url), 'utf8'));

const catalog = [];
for (const [file, names] of Object.entries(aliases)) {
  if (file.startsWith('_')) continue;
  if (!Array.isArray(names) || names.length === 0) continue;
  // "mesh" is what the viewer calls an outline font (TTF/OTF/WOFF) as opposed to
  // an SHX stroke font; it decides which parser to use from this field.
  catalog.push({ file, name: names, type: 'mesh' });
}

writeFileSync(out, JSON.stringify(catalog, null, 2) + '\n');
console.log(`wrote ${catalog.length} font entries (${aliases['noto-sans.ttf'].length + aliases['noto-sans-sc.otf'].length} names)`);
