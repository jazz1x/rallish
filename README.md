# rallish

> *Two agents, one melody.*

A local broker that lets multiple coding-agent CLIs (Claude Code, Kimi Code, …)
**turn-taking** — alternate turns — on a single task.

See [`DESIGN.md`](./DESIGN.md) for the full spec.

## Status

Pre-Phase-0. Design only.

## What "turn-taking" means

An execution pattern where multiple agents alternate turns to collaborate
on a single task, passing state through a shared broker.
