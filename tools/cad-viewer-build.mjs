// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

// Bundles @mlightcad/cad-simple-viewer into ES modules for the CAD preview.
//
// The published ESM build leaves six peer dependencies and four three.js
// example modules as bare imports, so it cannot be loaded by a browser
// directly. esbuild resolves the whole graph here and the result is packed into
// assets.zip; the shipped binary then serves self-contained files.
//
// Two entries are built rather than one. The viewer entry is what the page
// loads; the pdf entry is a lazily imported module that pulls in the PDF
// backend, which is large and used by a minority of opens. Splitting them keeps
// the first paint cheap while still letting both share a single copy of the
// viewer at runtime — hence splitting on, and no per-entry copy of the shared
// chunk.
//
// Usage: node tools/cad-viewer-build.mjs <work-dir> <out-dir>

import * as esbuild from 'esbuild';
import path from 'node:path';
import process from 'node:process';

const workDir = path.resolve(process.argv[2]);
const outDir = path.resolve(process.argv[3]);
const threeRoot = path.join(workDir, 'node_modules', 'three');

// three@0.172 restricts its exports map, so the deep "three/examples/jsm/*"
// specifiers the renderer uses have no resolvable entry. Map them by hand.
const resolveThreeExamples = {
  name: 'three-examples',
  setup(build) {
    build.onResolve({ filter: /^three\/examples\// }, (args) => {
      const rel = args.path.slice('three/examples/'.length);
      return { path: path.join(threeRoot, 'examples', rel.endsWith('.js') ? rel : rel + '.js') };
    });
  },
};

// Same story for the PDF plugin: its exports map only exposes the root and
// /register, both pre-bundled. The per-file lib/ build is what lets esbuild
// keep pdf.js and the import path out of the lazy chunk.
const resolvePdfPluginLib = {
  name: 'pdf-plugin-lib',
  setup(build) {
    const prefix = '@mlightcad/cad-pdf-plugin/lib/';
    build.onResolve({ filter: /^@mlightcad\/cad-pdf-plugin\/lib\// }, (args) => ({
      path: path.join(workDir, 'node_modules', '@mlightcad', 'cad-pdf-plugin', 'lib',
        args.path.slice(prefix.length)),
    }));
  },
};

await esbuild.build({
  absWorkingDir: workDir,
  entryPoints: {
    viewer: path.join(workDir, 'entry.js'),
    pdf: path.join(workDir, 'pdf-entry.js'),
  },
  outdir: outDir,
  bundle: true,
  format: 'esm',
  splitting: true,
  chunkNames: 'chunk-[hash]',
  target: 'es2022',
  minify: true,
  legalComments: 'none',
  plugins: [resolveThreeExamples, resolvePdfPluginLib],
  logLevel: 'warning',
});
