// Entry module for the issue/discussion detail pages — the one
// <script type="module"> tag each template needs. Everything is a
// progressive enhancement layered on the same forms: without JS they
// still post and validate normally (the classic POST → 303 → GET path).
import "./copy-permalink.js";
import "./copy-markdown.js";
import "./enter-submit.js";
import "./form-errors.js";
import "./partial-submit.js";
import "./comment-submit-disabled.js";
import "./markdown-editor.js";
import "./leave-guard.js";
import "./live-updates.js";
