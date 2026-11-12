// Copyright (C) 2026 docWebHub contributors
// SPDX-License-Identifier: AGPL-3.0-or-later
//
// Dual-licensed: AGPL-3.0 (see LICENSE) or a commercial license (see LICENSING.md).
// Distributed WITHOUT ANY WARRANTY; see LICENSE for the full terms.

// Builds internal/web/preview.min.js: the syntax highlighter and Markdown
// renderer used by the directory listing's preview pane.
//
// The output is committed so a plain `go build` never needs Node.js. Run
// tools/build-listing-preview.sh after changing this file or the pinned
// library versions.

import Prism from "prismjs";
import "prismjs/components/prism-markup";
import "prismjs/components/prism-css";
import "prismjs/components/prism-clike";
import "prismjs/components/prism-javascript";
import "prismjs/components/prism-typescript";
import "prismjs/components/prism-jsx";
import "prismjs/components/prism-tsx";
import "prismjs/components/prism-json";
import "prismjs/components/prism-bash";
import "prismjs/components/prism-python";
import "prismjs/components/prism-go";
import "prismjs/components/prism-rust";
import "prismjs/components/prism-java";
import "prismjs/components/prism-scala";
import "prismjs/components/prism-c";
import "prismjs/components/prism-cpp";
import "prismjs/components/prism-objectivec";
import "prismjs/components/prism-csharp";
import "prismjs/components/prism-markup-templating";
import "prismjs/components/prism-php";
import "prismjs/components/prism-ruby";
import "prismjs/components/prism-swift";
import "prismjs/components/prism-kotlin";
import "prismjs/components/prism-dart";
import "prismjs/components/prism-yaml";
import "prismjs/components/prism-markdown";
import "prismjs/components/prism-sql";
import "prismjs/components/prism-docker";
import "prismjs/components/prism-ini";
import "prismjs/components/prism-toml";
import "prismjs/components/prism-diff";
import "prismjs/components/prism-lua";
import "prismjs/components/prism-perl";
import "prismjs/components/prism-r";
import "prismjs/components/prism-powershell";
import "prismjs/components/prism-batch";
import "prismjs/components/prism-makefile";
import { marked } from "marked";

const ESCAPES = { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" };

function escapeHTML(text) {
	return String(text).replace(/[&<>"']/g, (ch) => ESCAPES[ch]);
}

// Fence languages and file extensions both resolve through this table. Prism
// already registers many of these aliases, but spelling them out keeps the
// mapping independent of the upstream alias list.
const ALIASES = {
	js: "javascript",
	mjs: "javascript",
	cjs: "javascript",
	ts: "typescript",
	html: "markup",
	htm: "markup",
	xml: "markup",
	svg: "markup",
	vue: "markup",
	svelte: "markup",
	astro: "markup",
	sh: "bash",
	shell: "bash",
	zsh: "bash",
	py: "python",
	rs: "rust",
	cs: "csharp",
	"c++": "cpp",
	cc: "cpp",
	cxx: "cpp",
	objc: "objectivec",
	kt: "kotlin",
	kts: "kotlin",
	yml: "yaml",
	md: "markdown",
	dockerfile: "docker",
	ps1: "powershell",
	psm1: "powershell",
	bat: "batch",
	cmd: "batch",
	make: "makefile",
};

const EXTENSIONS = {
	".js": "javascript", ".mjs": "javascript", ".cjs": "javascript", ".jsx": "jsx",
	".ts": "typescript", ".tsx": "tsx", ".mts": "typescript", ".cts": "typescript",
	".json": "json", ".jsonc": "json", ".webmanifest": "json",
	".css": "css", ".scss": "css", ".sass": "css", ".less": "css",
	".html": "markup", ".htm": "markup", ".xml": "markup", ".svg": "markup",
	".vue": "markup", ".svelte": "markup", ".astro": "markup",
	".sh": "bash", ".bash": "bash", ".zsh": "bash", ".fish": "bash",
	".py": "python", ".go": "go", ".rs": "rust", ".java": "java",
	".c": "c", ".h": "c", ".cc": "cpp", ".cpp": "cpp", ".cxx": "cpp", ".hpp": "cpp", ".hh": "cpp",
	".cs": "csharp", ".php": "php", ".rb": "ruby", ".swift": "swift",
	".kt": "kotlin", ".kts": "kotlin", ".scala": "scala", ".dart": "dart",
	".m": "objectivec", ".mm": "objectivec",
	".sql": "sql", ".yml": "yaml", ".yaml": "yaml",
	".ini": "ini", ".conf": "ini", ".cfg": "ini", ".properties": "ini", ".toml": "toml",
	".diff": "diff", ".patch": "diff", ".lua": "lua",
	".pl": "perl", ".pm": "perl", ".r": "r",
	".ps1": "powershell", ".psm1": "powershell", ".bat": "batch", ".cmd": "batch",
	".md": "markdown", ".markdown": "markdown",
};

// Markdown links and images are untrusted input. Allow only navigation that a
// browser can safely follow; anything with another scheme (javascript:, data:,
// vbscript:, file:) is removed instead of rendered.
const SAFE_PROTOCOLS = new Set(["http:", "https:", "mailto:", "tel:"]);

function safeURL(value, allowContact) {
	const raw = String(value || "").trim();
	if (!raw) return "";
	try {
		const parsed = new URL(raw, window.location.href);
		if (!SAFE_PROTOCOLS.has(parsed.protocol)) return "";
		if (!allowContact && (parsed.protocol === "mailto:" || parsed.protocol === "tel:")) return "";
		return raw;
	} catch (err) {
		return "";
	}
}

function languageFor(name) {
	const lower = String(name).toLowerCase();
	const base = lower.slice(lower.lastIndexOf("/") + 1);
	if (base === "dockerfile") return "docker";
	if (base === "makefile" || base === "gnumakefile") return "makefile";
	const dot = lower.lastIndexOf(".");
	if (dot < 0) return "";
	return EXTENSIONS[lower.slice(dot)] || "";
}

function highlight(code, language) {
	const grammar = Prism.languages[language] || Prism.languages[ALIASES[language] || ""];
	if (!grammar) return null;
	return Prism.highlight(code, grammar, language);
}

// Raw HTML in Markdown is rendered as text: a previewed file must never be able
// to inject markup into the application page. marked still handles the normal
// syntax, and its default link/image cleanup rejects javascript: URLs.
marked.use({
	gfm: true,
	breaks: false,
	renderer: {
		html({ text }) {
			return escapeHTML(text);
		},
	},
});

function markdown(text) {
	let html;
	try {
		html = marked.parse(text);
	} catch (err) {
		return null;
	}

	const template = document.createElement("template");
	template.innerHTML = html;

	template.content.querySelectorAll("pre > code").forEach((code) => {
		const match = /(?:^|\s)language-([\w+#.-]+)/.exec(code.className);
		if (!match) return;
		const language = ALIASES[match[1].toLowerCase()] || match[1].toLowerCase();
		const rendered = highlight(code.textContent, language);
		if (rendered !== null) code.innerHTML = rendered;
	});

	template.content.querySelectorAll("a[href]").forEach((link) => {
		const href = safeURL(link.getAttribute("href"), true);
		if (!href) {
			link.removeAttribute("href");
			return;
		}
		link.setAttribute("href", href);
		if (href.startsWith("#")) return;
		link.target = "_blank";
		link.rel = "noopener noreferrer";
	});
	template.content.querySelectorAll("img[src]").forEach((img) => {
		const src = safeURL(img.getAttribute("src"), false);
		if (!src) {
			img.removeAttribute("src");
			return;
		}
		img.setAttribute("src", src);
		img.loading = "lazy";
		img.decoding = "async";
	});

	return template.content;
}

globalThis.LWPreview = { languageFor, highlight, markdown };
