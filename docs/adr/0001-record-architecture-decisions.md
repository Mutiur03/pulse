# 1. Record architecture decisions

Date: 2026-10-16. Status: accepted.

## Context

Pulse's large design document (`docs/architecture.md`) describes the target
system. Several decisions were made or changed while building it, and the
reasons are scattered across pull requests.

## Decision

Keep one short Markdown record per significant decision in `docs/adr`, using
the context / decision / consequences format. Records are immutable once
accepted; a change of mind is a new record that supersedes the old one.

## Consequences

New contributors can see why things are the way they are without reading the
whole history. Records that describe the target design say so, and note
where the code differs today.
