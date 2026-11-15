---
name: no-comments
description: "Apply the comment-sicko keep-list to a scope of code: delete narration, banners and workaround sermons; keep external-constraint whys and API contracts; move implementation details out of doc strings."
---

# No comments

Adapted from cursor/plugins pstack for this repository: **no subagents — the
invoking agent plays comment-sicko itself**. Read `agents/comment-sicko.md`
first; its keep-list and judgment rules are the spec. The original Cursor
orchestration (Task spawn, report/reject protocol, poteto-mode) is dropped;
what remains is the triage and the follow-through.

## Scope

Use the caller's files or diff. Otherwise use the current diff against the
base branch, or a named tree when the caller asks for a full-repo pass.
`third_party/` is never in scope (vendored, not our code).

## Steps

1. Read `agents/comment-sicko.md` and inventory every comment in scope
   (`//`, `/* */`, `{{/* */}}`, CSS `/* */`).
2. Triage each against the keep-list: legal headers, external-dependency /
   platform / protocol constraints, real lint suppressions, public-API
   contract docs, issue/RFC/design-doc links. Everything else is meat —
   narration, banners, commented-out code, and sermons defending our own
   code's surprises. Doubt after checking the surrounding code is meat.
3. Fix trivial accepted flags directly: delete a dead path, drop a
   parameter, use the real API. Anything bigger gets a `MUST KILL` flag on
   the exact symbol (rename / extract / type / reshape) and is reported
   open — do not widen the fence. Our-code surprises never get their
   comments back; they get the flag.
4. Doc comments on exported symbols state the contract only. Implementation
   details move into ordinary comments inside the body where they still
   earn their keep, or die. Unexported doc comments may carry the why.
   Constraint comments (`do not remove`, `IMPORTANT`, thin justifications)
   survive only with proof the constraint is real and external — trace the
   symbol or run the `why` skill (`.agents/skills/why/`) on it. Where a
   type, runtime check, test, or lint could encode the constraint cheaply,
   offer the encoding and wait for approval before touching it.
5. Never polish meat into a shorter alibi, never restore a comment to make
   a diff look smaller, never write application code to justify a delete.
6. Report: deletion count, restored comments, `MUST KILL` flags with one
   line each, encodings offered/applied, and constraints left open.
